# Makefile for managing Docker Compose services and running Go services locally

# Go services paths
CMD_PATH := ./cmd/server.go

# Define default targets
.PHONY: build up down restart clean help \
        proxy-local manager-local \
        restart-manager restart-proxy e2e-test \
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

est-local: ## Run the estimation service locally with command-line flags
	@echo "Running Estimation Service Locally..."
	cd ./estimation && uv run main

restart-manager: ## Restart only the manager service
	docker compose restart manager

restart-proxy: ## Restart only the proxy service
	docker compose restart proxy

e2e-test: ## Run end-to-end tests by executing test_send_data.sh
	./manager_test.sh

db-up: ## Start only the database services
	docker compose up -d postgres_manager postgres_proxy

db-down: ## Stop and remove the database services and their volumes
	docker compose rm -s -v -f postgres_manager postgres_proxy

help: ## Display this help message
	@echo "Usage: make [target] [GO_FLAGS='-mode=local -port=8010']"
	@echo
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'
