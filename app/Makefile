GO = go
GOFLAGS = -v

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d %H:%M:%S UTC')
LDFLAGS = -ldflags "-s -w -X 'github.com/mqtt-home/mqtt-unifi-network/version.Version=$(VERSION)' -X 'github.com/mqtt-home/mqtt-unifi-network/version.GitCommit=$(GIT_COMMIT)' -X 'github.com/mqtt-home/mqtt-unifi-network/version.BuildTime=$(BUILD_TIME)'"

BUILD_DIR = build
CONFIG_DIR = $(PWD)/../production/config
WEB_DIR = web

BINARY_NAME = mqtt-unifi-network
DOCKER_IMAGE_NAME = pharndt/mqtt-unifi-network:latest

.PHONY: build
build: build-frontend build-backend

.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

.PHONY: build-frontend
build-frontend:
	@echo "Building the frontend..."
	@cd $(WEB_DIR) && pnpm install && pnpm run build

.PHONY: build-backend
build-backend:
	@echo "Building the backend..."
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .

.PHONY: dev
dev: build-frontend build-backend
	@echo "Starting the application..."
	@$(BUILD_DIR)/$(BINARY_NAME) $(CONFIG_DIR)/config.json

.PHONY: dev-frontend
dev-frontend:
	@echo "Starting frontend dev server..."
	@cd $(WEB_DIR) && pnpm run dev

.PHONY: dev-backend
dev-backend: build-backend
	@echo "Starting backend dev server..."
	@$(BUILD_DIR)/$(BINARY_NAME) $(CONFIG_DIR)/config.json

.PHONY: run
run: build
	@echo "Running the application..."
	@$(BUILD_DIR)/$(BINARY_NAME) $(CONFIG_DIR)/config.json

.PHONY: test
test:
	@$(GO) test ./...

.PHONY: docker
docker:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE_NAME) .

.PHONY: clean
clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@cd $(WEB_DIR) && rm -rf dist node_modules

.PHONY: deps
deps:
	@echo "Downloading Go dependencies..."
	@$(GO) mod download
	@$(GO) mod tidy
