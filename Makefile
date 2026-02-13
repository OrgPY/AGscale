IMG ?= ghcr.io/example/agscale-controller:latest

.PHONY: build
build:
	go build -o bin/manager main.go

.PHONY: run
run:
	go run ./main.go

.PHONY: envtest
envtest:
	./hack/setup-envtest.sh

.PHONY: install-pre-commit
install-pre-commit:
	./hack/install-pre-commit.sh

.PHONY: test
test:
	@eval "$$(./hack/setup-envtest.sh)"; go test ./...

.PHONY: helm-lint
helm-lint:
	helm lint charts/agscale

.PHONY: helm-template
helm-template:
	helm template agscale charts/agscale >/tmp/agscale-helm.yaml

.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .

.PHONY: deploy
deploy:
	kubectl apply -k config/default

.PHONY: undeploy
undeploy:
	kubectl delete -k config/default --ignore-not-found=true
