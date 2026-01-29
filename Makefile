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

##@ Help
.PHONY: help
help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: build run deps deps-audit format proto

build: ## Build the application binary
	go build -ldflags "-X main.Version=$(VERSION)" -o bin/prism cmd/server/main.go

run: ## Run the application
	go run cmd/server/main.go

deps: ## Run dependency tidy & verify
	go mod tidy && go mod verify

deps-audit: ## Check dependency vulnerabilities
	govulncheck ./...

format: ## Run code formatter (gofumpt & gci)
	gofumpt -l -w .
	gci write . --skip-generated -s standard -s default

proto: ## Generate source files from proto definitions
	protoc --go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		docs/proto/v1/*.proto

##@ Testing
.PHONY: test integration-test lint

test: ## Run unit tests with race detection
	go test -v -race -covermode atomic -coverprofile=coverage.txt ./internal/... ./pkg/...

integration-test: ## Run integration tests
	go clean -testcache && go test -v ./test/...

lint: ## Run golangci linter
	golangci-lint run

##@ Database
.PHONY: migrate-create migrate-up

migrate-create: ## Create new migration (Usage: make migrate-create name_of_migration)
	migrate create -ext sql -dir migrations '$(word 2,$(MAKECMDGOALS))'

migrate-up: ## Run migration up
	migrate -path migrations -database '$(PG_URL)?sslmode=disable' up

##@ Docker
.PHONY: docker-build docker-run

docker-build: ## Build out docker image
	docker build -t $(IMAGE_NAME):latest --build-arg PRISM_VERSION=$(VERSION) -f $(DOCKER_FILE) .

docker-run: ## Run docker container from image
	docker run -p 8080:8080 --env-file .env $(IMAGE_NAME):latest