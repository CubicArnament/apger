GO ?= go
HELM ?= helm
IMAGE ?= ghcr.io/nuros-linux/apger:dev

.PHONY: build test vet verify image helm-lint

build:
	$(GO) build -o apger ./cmd/apger

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

helm-lint:
	$(HELM) lint charts/apger charts/apgbuild charts/acp

verify: test vet helm-lint

image:
	docker build -t $(IMAGE) .
