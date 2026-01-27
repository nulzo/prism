ifneq ($(wildcard .env),)
include .env
export
else
$(warning WARNING: .env file not found! Using .env.example)
include .env.example
export
endif

VERSION := $(shell git describe --tags --always --dirty)
IMAGE_NAME := prism
DOCKER_FILE := build/Dockerfile

.PHONY: help

help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


.PHONY: build run docker-build docker-run

# Go Commands
build: ## Build the application binary
	go build -ldflags "-X main.Version=$(VERSION)" -o bin/prism cmd/server/main.go
.PHONY: build

run: ### Run the application
	go run cmd/server/main.go
.PHONY: run

deps: ### Run dependency tidy & verify
	go mod tidy && go mod verify
.PHONY: deps

deps-audit: ### Check dependency vulnerabilities
	govulncheck ./...
.PHONY: deps-audit

proto: ### Generate source files from proto
	protoc --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		docs/proto/v1/*.proto
.PHONY: proto

# Docker Commands
docker-build: ### Build out docker image
	docker build -t $(IMAGE_NAME):latest --build-arg PRISM_VERSION=$(VERSION) -f $(DOCKER_FILE) .
.PHONY: docker-build

docker-run: ### Run docker container from image
	docker run -p 8080:8080 --env-file .env $(IMAGE_NAME):latest
.PHONY: docker-run

# Test Commands
test: ### Run unit tests
	go test -v -race -covermode atomic -coverprofile=coverage.txt ./internal/... ./pkg/...
.PHONY: test

integration-test: ### Run integration tests
	go clean -testcache && go test -v ./test/...
.PHONY: integration-test
