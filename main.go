//go:generate scripts/generate-openapi.sh
package main

import (
	"context"
	"flag"
	"fmt"
	"movie-catalogue/pkg/api"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/deepmap/oapi-codegen/pkg/middleware"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	port             *int
	postgresUser     = "app"
	postgresPassword *string
	postgresHost     *string
	postgresPort     = "5432"
	build            = "develop"
)

const (
	service     = "trace-sales-api"
	environment = "production"
	id          = 1
	url         = "http://localhost:14268/api/traces"
)

func main() {
	port = flag.Int("port", 8081, "Port for test HTTP server")
	postgresPassword = flag.String("postgres-password", "hiclass@12", "Postgres password")
	postgresHost = flag.String("postgres-host", "localhost", "Postgres host")
	flag.Parse()

	// Connect to postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s",
		*postgresHost, postgresUser, *postgresPassword, "movie", postgresPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	db.AutoMigrate(&api.Movie{})

	//
	swagger, err := api.GetSwagger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading swagger spec\n: %s", err)
		os.Exit(1)
	}

	swagger.Servers = nil

	serverImpl := &api.ServerImplementation{
		DB: db,
	}

	e := echo.New()
	e.Use(echomiddleware.Logger())

	e.Use(middleware.OapiRequestValidator(swagger))

	// We now register our petStore above as the handler for the interface
	api.RegisterHandlers(e, serverImpl)

	// ctx, span := otel.GetTracerProvider().Tracer("").Start(ctx, "foundation.web.respond")
	// span.SetAttributes(attribute.Int("statusCode", statusCode))
	// defer span.End()

	// Start Tracing Support

	log.Info("startup", "status", "initializing OT/Jaeger tracing support")

	traceProvider, err := startTracing(
		service,
		url,
		id,
	)
	if err != nil {
		fmt.Errorf("starting tracing: %w", err)
	}
	defer traceProvider.Shutdown(context.Background())

	// Construct the mux for the debug calls.
	debugMux := api.DebugStandardLibraryMux()

	// Start the service listening for debug requests.
	// Not concerned with shutting this down with load shedding.
	go func() {
		if err := http.ListenAndServe("0.0.0.0:4000", debugMux); err != nil {
			log.Error("shutdown", "status", "debug router closed", "host", "0.0.0.0:4000", "ERROR", err)
		}
	}()

	// App Starting

	ctxi := make(chan error, 1)

	log.Info("starting service", "version", build)
	defer log.Info("shutdown complete")

	// And we serve HTTP until the world ends.
	go func() {
		// ctx <- context.Background()
		log.Info("api", "status", "api started", "handlers", context.Background())
		ctxi <- (e.Start(fmt.Sprintf("0.0.0.0:%d", *port)))
	}()

	shutdown := make(chan os.Signal, 0)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-ctxi:
		fmt.Errorf("server error: %w", err)
	case sig := <-shutdown:
		log.Info("shutdown", "status", "shutdown started", "signal", sig)
		defer log.Info("shutdown", "status", "shutdown complete", "signal", sig)

		// Give outstanding requests a deadline for completion.
		_, cancel := context.WithTimeout(context.Background(), e.Server.ReadTimeout)
		defer cancel()
	}

}

// startTracing configure open telemetery to be used with zipkin.
func startTracing(serviceName string, reporterURI string, probability float64) (*trace.TracerProvider, error) {

	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(
		// Always be sure to batch in production.
		trace.WithBatcher(exp),
		// Record information about this application in a Resource.
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(service),
			attribute.String("environment", environment),
			attribute.Int64("ID", id),
		)),
	)

	// I can only get this working properly using the singleton :(
	otel.SetTracerProvider(tp)
	return tp, nil
}
