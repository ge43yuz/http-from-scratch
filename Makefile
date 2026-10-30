IMAGE := scratch-http:dev
CLUSTER := observability
NAMESPACE := apps



.PHONY: image deploy-local



image:
	docker build -t $(IMAGE) .



deploy-local: image
	kind load docker-image $(IMAGE) --name $(CLUSTER)
	kubectl create namespace $(NAMESPACE) \
		--dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f k8s/
	kubectl rollout restart deployment/go-http -n $(NAMESPACE)
	kubectl rollout status deployment/go-http -n $(NAMESPACE)


