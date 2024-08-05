# Makefile for managing Docker Compose services and running Go services locally

# Default environment variables
export COMPOSE_PROJECT_NAME := elpis_project
export COMPOSE_FILE := compose.yaml

# Go services paths
CMD_PATH := ./cmd/server.go

# Define default targets
.PHONY: all build up down logs restart clean help \
        proxy manager postgres_manager postgres_proxy vite-app \
        proxy-local manager-local

# Default flags for running Go services locally
GO_FLAGS ?= -mode=local -port=8010


all: up ## Build and start the services

build: ## Build the Docker images for all services
	docker compose build

up: ## Start all services in the background
	docker compose up -d

down: ## Stop all running services
	docker compose down

logs: ## Tail logs for all services
	docker compose logs -f

restart: down up ## Restart all services

clean: ## Stop all services and remove containers, networks, and volumes
	docker compose down --volumes --remove-orphans

ps: ## List all running services
	docker compose ps

proxy: ## Run only the proxy service
	docker compose up -d proxy

manager: ## Run only the manager service
	docker compose up -d manager

postgres_manager: ## Run only the postgres_manager service
	docker compose up -d postgres_manager

postgres_proxy: ## Run only the postgres_proxy service
	docker compose up -d postgres_proxy

vite-app: ## Run only the vite-app service
	docker compose up -d vite-app

proxy-local: ## Run the proxy service locally with command-line flags
	@echo "Running Proxy Service Locally..."
	cd ./proxy && go run $(CMD_PATH)

manager-local: ## Run the manager service locally with command-line flags
	@echo "Running Manager Service Locally..."
	cd ./manager && go run $(CMD_PATH) $(GO_FLAGS)

help: ## Display this help message
	@echo "Usage: make [target] [GO_FLAGS='-mode=local -port=8010']"
	@echo
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'
