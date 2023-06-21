IMAGE_NAME ?= "cnskunkworks/movie-catalogue:v3"
JAEGER_OPERATOR_VERSION = v1.40.0
.PHONY: docker-push
docker-push:
	docker build -t $(IMAGE_NAME) .
	docker push $(IMAGE_NAME)
deploy:
	cd chart && helm upgrade --install movie-catalogue . --set=image.tag="v3" --set=postgres.password=$$(kubectl get secrets movie-db-cluster-app -o jsonpath="{.data.password}" | base64 --decode) && cd ../

add-ingress:
	minikube addons enable ingress

deploy-cert-manager:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.12.0/cert-manager.yaml

namespace-k8s:
	kubectl create namespace observability

jaeger-operator-k8s:
	# Create the jaeger operator and necessary artifacts in ns observability
	kubectl create -n observability -f https://github.com/jaegertracing/jaeger-operator/releases/download/$(JAEGER_OPERATOR_VERSION)/jaeger-operator.yaml

jaeger-k8s:
	kubectl apply -f k8s-jaeger/jaeger.yaml

otel-collector-k8s:
	kubectl apply -f k8s-jaeger/otel-collector.yaml