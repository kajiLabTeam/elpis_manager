GOCMD=go
GORUN=$(GOCMD) run
GOBUILD=$(GOCMD) build

run: ## Run the server
	@$(GORUN) ./cmd/server.go

build: ## Build for local OS
	GOOS=linux $(GOBUILD) ./cmd/server.go

build/pi: ## Build for Raspberry Pi
	GOOS=linux GOARCH=arm GOARM=7 $(GOBUILD) ./cmd/server.go

build/pi64: ## Build for Raspberry Pi 64-bit
	GOOS=linux GOARCH=arm64 $(GOBUILD) ./cmd/server.go

help: ## Display this help screen
	@grep -E '^[a-zA-Z/_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
