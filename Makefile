# Makefile for managing Docker Compose services and running Go services locally

# Go services paths
CMD_PATH := ./cmd/server.go

# Define default targets
.PHONY: build up down restart clean help \
        proxy-local manager-local est-model-local est-api-local \
        restart-manager restart-proxy \
        e2e-test est-api-test manager-test proxy-test web-test \
        db-up db-down

# Default flags for running Go services locally
GO_FLAGS ?= -mode=local -port=8010

build: ## Build the Docker images for all services
	docker compose build

up: ## Start all services in the background
	docker compose up -d

down: ## Stop all running services
	docker compose down

restart: down up ## Restart all services

clean: ## Stop all services and remove containers, networks, and volumes
	docker compose down --volumes --remove-orphans

proxy-local: ## Run the proxy service locally with command-line flags
	@echo "Running Proxy Service Locally..."
	cd ./proxy && go run $(CMD_PATH)

manager-local: ## Run the manager service locally with command-line flags
	@echo "Running Manager Service Locally..."
	cd ./manager && go run $(CMD_PATH) $(GO_FLAGS)

est-model-local: ## Run the estimation service locally with command-line flags
	@echo "Running Estimation Service Locally..."
	cd ./estimation && uv run src/estimation/main.py

est-api-local: ## Run the estimation API service locally with command-line flags
	@echo "Running Estimation API Service Locally..."
	cd ./estimation && uv run src/estimation/app.py 

restart-manager: ## Restart only the manager service
	docker compose restart manager

restart-proxy: ## Restart only the proxy service
	docker compose restart proxy

restart-est-api: ## Restart only the estimation API service
	docker compose restart estimation-api

db-up: ## Start only the database services
	docker compose up -d postgres_manager postgres_proxy

db-down: ## Stop and remove the database services and their volumes
	docker compose rm -s -v -f postgres_manager postgres_proxy

est-api-test: ## Run the est-api e2e test
	bash ./e2e/est-api_test.sh

manager-test: ## Run the manager e2e test
	bash ./e2e/manager_test.sh

proxy-test: ## Run the proxy e2e test
	bash ./e2e/proxy_test.sh

web-test: ## Run the web e2e test
	bash ./e2e/web_test.sh

help: ## Display this help message
	@echo "Usage: make [target] [GO_FLAGS='-mode=local -port=8010']"
	@echo
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'
