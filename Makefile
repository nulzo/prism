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
COMPOSE := docker compose
COMPOSE_SERVICES := prism searxng

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
.PHONY: docker-build docker-run docker-stop docker-down docker-rm docker-clean

docker-build: ## Build out docker image
	docker build -t $(IMAGE_NAME):latest --build-arg PRISM_VERSION=$(VERSION) -f $(DOCKER_FILE) .

docker-run: ## Run docker container from image
	docker run --rm --name $(IMAGE_NAME) -p 8080:8080 -d --env-file .env $(IMAGE_NAME):latest

docker-stop: ## Stop the running docker container
	docker stop $(IMAGE_NAME) || true

docker-down: docker-stop ## Stop the running docker container (alias for docker-stop)

docker-rm: ## Stop and remove the docker container
	docker stop $(IMAGE_NAME) || true
	docker rm $(IMAGE_NAME) || true

docker-clean: ## Remove the docker image
	docker rmi $(IMAGE_NAME):latest || true

##@ Compose (full stack: prism + searxng)
.PHONY: compose-build compose-up compose-up-fg compose-down compose-restart compose-logs compose-logs-prism compose-logs-searxng compose-ps compose-pull compose-nuke compose-shell compose-search-check

compose-build: ## Build images defined in docker-compose.yml
	$(COMPOSE) build

compose-up: ## Start prism + searxng detached (rebuilds if source changed)
	$(COMPOSE) up -d --build

compose-up-fg: ## Start prism + searxng in the foreground (Ctrl-C to stop)
	$(COMPOSE) up --build

compose-down: ## Stop and remove the compose stack (keeps named volumes)
	$(COMPOSE) down

compose-restart: ## Recreate the prism container (pulls in code changes)
	$(COMPOSE) up -d --build --force-recreate --no-deps prism

compose-logs: ## Tail logs for every service in the stack
	$(COMPOSE) logs -f --tail=100

compose-logs-prism: ## Tail only the prism service logs
	$(COMPOSE) logs -f --tail=200 prism

compose-logs-searxng: ## Tail only the searxng service logs
	$(COMPOSE) logs -f --tail=200 searxng

compose-ps: ## Show status of compose services
	$(COMPOSE) ps

compose-pull: ## Pull latest upstream images (searxng, etc.)
	$(COMPOSE) pull

compose-shell: ## Open an interactive shell inside the prism container
	$(COMPOSE) exec prism sh

compose-search-check: ## Smoke-test searxng JSON from inside the prism container
	$(COMPOSE) exec prism sh -c 'wget -qO- "http://searxng:8080/search?q=hello&format=json" | head -c 400; echo'

compose-nuke: ## Tear down the stack AND remove the named volumes (destroys router.db)
	$(COMPOSE) down -v