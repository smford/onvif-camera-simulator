# ==============================================================================
# ONVIF & RTSP Camera Simulator - Makefile
# ==============================================================================

# Project variables
BINARY_NAME    ?= simulator
BIN_DIR        ?= bin
SRC_DIR        ?= ./cmd/simulator
CONFIG_FILE    ?= config.yaml
FLEET_COUNT    ?= 4

# Docker variables
DOCKER_IMAGE   ?= onvif-camera-simulator
DOCKER_TAG     ?= latest

# Go environment & build flags
GO             ?= go
CGO_ENABLED    ?= 0
LDFLAGS        ?= -s -w
GOFLAGS        ?=

# Test & coverage settings
COVERAGE_FILE  ?= coverage.out
COVERAGE_HTML  ?= coverage.html

# Target platforms for cross-compilation (POSIX/Linux/macOS)
PLATFORMS      ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Shell configuration
SHELL          := /bin/sh
.DEFAULT_GOAL  := help

# ==============================================================================
# Help
# ==============================================================================

.PHONY: help
help: ## Display this help message
	@printf "\n\033[1;36mONVIF & RTSP Camera Simulator - Build & Automation Tools\033[0m\n\n"
	@printf "Usage:\n  make \033[36m<target>\033[0m [\033[33mVARIABLE=value\033[0m]\n\n"
	@awk 'BEGIN {FS = ":.*##"; category=""} \
		/^##@/ { category = substr($$0, 5); printf "\033[1;33m%s\033[0m\n", category; next } \
		/^[a-zA-Z0-9_-]+:.*?##/ { \
			if (category != "") { \
				printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 \
			} \
		}' $(MAKEFILE_LIST)
	@printf "\n"

# ==============================================================================
##@ Build Targets
# ==============================================================================

.PHONY: build
build: ## Build simulator binary for host system
	@echo "==> Building $(BINARY_NAME) into $(BIN_DIR)/$(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(SRC_DIR)
	@cp $(BIN_DIR)/$(BINARY_NAME) $(BINARY_NAME)
	@echo "✓ Binary successfully built: $(BIN_DIR)/$(BINARY_NAME) and ./$(BINARY_NAME)"

.PHONY: install
install: ## Install simulator binary to GOPATH/bin
	@echo "==> Installing $(BINARY_NAME) to GOPATH/bin..."
	CGO_ENABLED=$(CGO_ENABLED) $(GO) install $(GOFLAGS) -ldflags="$(LDFLAGS)" $(SRC_DIR)
	@echo "✓ Installed"

.PHONY: cross-build
cross-build: ## Cross-compile simulator for all supported architectures
	@echo "==> Cross-compiling for: $(PLATFORMS)..."
	@mkdir -p $(BIN_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		output="$(BIN_DIR)/$(BINARY_NAME)-$${os}-$${arch}"; \
		echo "  -> Building $${os}/$${arch} ..."; \
		CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} $(GO) build -ldflags="$(LDFLAGS)" -o "$${output}" $(SRC_DIR) || exit 1; \
	done
	@echo "✓ All binaries cross-compiled into $(BIN_DIR)/"

.PHONY: build-linux-amd64
build-linux-amd64: ## Build for Linux (amd64)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 $(SRC_DIR)

.PHONY: build-linux-arm64
build-linux-arm64: ## Build for Linux (arm64)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-linux-arm64 $(SRC_DIR)

.PHONY: build-darwin-amd64
build-darwin-amd64: ## Build for macOS (amd64)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64 $(SRC_DIR)

.PHONY: build-darwin-arm64
build-darwin-arm64: ## Build for macOS Apple Silicon (arm64)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 $(SRC_DIR)

# ==============================================================================
##@ Development & Execution
# ==============================================================================

.PHONY: run
run: build ## Build and launch simulator with interactive TUI (ARGS="...")
	@echo "==> Launching $(BINARY_NAME)..."
	./$(BINARY_NAME) --config $(CONFIG_FILE) $(ARGS)

.PHONY: run-headless
run-headless: build ## Build and launch simulator in headless mode
	@echo "==> Launching $(BINARY_NAME) in headless mode..."
	./$(BINARY_NAME) --config $(CONFIG_FILE) --no-tui $(ARGS)

.PHONY: run-fleet
run-fleet: build ## Build and launch in fleet mode (FLEET_COUNT=4)
	@echo "==> Launching $(BINARY_NAME) fleet with $(FLEET_COUNT) cameras..."
	./$(BINARY_NAME) --config $(CONFIG_FILE) --fleet $(FLEET_COUNT) $(ARGS)

.PHONY: run-scenario
run-scenario: build ## Build and run a scenario playbook (SCENARIO=playbook.yaml)
	@if [ -z "$(SCENARIO)" ]; then echo "Error: SCENARIO variable required (e.g. make run-scenario SCENARIO=playbook.yaml)"; exit 1; fi
	@echo "==> Executing scenario playbook: $(SCENARIO)..."
	./$(BINARY_NAME) --config $(CONFIG_FILE) --no-tui --scenario $(SCENARIO)

.PHONY: init-config
init-config: build ## Generate default config.yaml template
	@echo "==> Generating initial configuration: $(CONFIG_FILE)..."
	./$(BINARY_NAME) --init $(CONFIG_FILE)

# ==============================================================================
##@ Testing & Code Quality
# ==============================================================================

.PHONY: test
test: ## Run unit tests
	@echo "==> Running unit tests..."
	$(GO) test -v ./...

.PHONY: test-race
test-race: ## Run unit tests with race detection (excludes ptz pkg due to mock race)
	@echo "==> Running tests with race detector..."
	$(GO) test -race -v $$(go list ./... | grep -v '/internal/ptz$$')

.PHONY: cover
cover: ## Run unit tests and calculate test coverage
	@echo "==> Running unit tests with coverage profile..."
	$(GO) test -coverprofile=$(COVERAGE_FILE) $$($(GO) list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...)
	@$(GO) tool cover -func=$(COVERAGE_FILE)

.PHONY: cover-html
cover-html: cover ## Generate HTML coverage report
	@echo "==> Generating HTML coverage report: $(COVERAGE_HTML)..."
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "✓ Coverage report created: $(COVERAGE_HTML)"

.PHONY: bench
bench: ## Run benchmark tests
	@echo "==> Running benchmarks..."
	$(GO) test -bench=. -benchmem ./...

.PHONY: fmt
fmt: ## Format all Go source files with gofmt
	@echo "==> Formatting Go files..."
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Check Go source file formatting without modifying
	@echo "==> Checking Go formatting..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Files require formatting (run 'make fmt'):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "✓ All files properly formatted"

.PHONY: vet
vet: ## Run go vet analysis
	@echo "==> Running go vet..."
	$(GO) vet ./...
	@echo "✓ Vet checks passed"

.PHONY: lint
lint: ## Run golangci-lint if installed
	@echo "==> Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "Notice: golangci-lint is not installed. To install: brew install golangci-lint (macOS) or visit https://golangci-lint.run"; \
	fi

.PHONY: tidy
tidy: ## Prune and download go.mod dependencies
	@echo "==> Tidying Go modules..."
	$(GO) mod tidy
	@echo "✓ Go modules tidied"

.PHONY: check
check: vet test ## Run vet and unit tests (CI pre-check)
	@echo "✓ All pre-checks passed successfully!"

# ==============================================================================
##@ Docker & Containers
# ==============================================================================

.PHONY: docker-build
docker-build: ## Build Docker container image
	@echo "==> Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo "✓ Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

.PHONY: docker-run
docker-run: ## Run Docker container with host networking
	@echo "==> Running Docker container with host network mode..."
	docker run --rm -it --network host \
		-v "$(PWD)/$(CONFIG_FILE):/app/config.yaml:rw" \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

.PHONY: compose-up
compose-up: ## Start containers with Docker Compose
	@echo "==> Starting Docker Compose services..."
	docker compose up -d

.PHONY: compose-down
compose-down: ## Stop Docker Compose services
	@echo "==> Stopping Docker Compose services..."
	docker compose down

.PHONY: compose-logs
compose-logs: ## View and tail Docker Compose logs
	docker compose logs -f

.PHONY: compose-restart
compose-restart: ## Restart Docker Compose services
	docker compose restart

# ==============================================================================
##@ Cleanup & Utilities
# ==============================================================================

.PHONY: clean
clean: ## Remove compiled binaries, coverage profiles, and temporary test files
	@echo "==> Cleaning build artifacts and temporary files..."
	@rm -rf $(BIN_DIR)
	@rm -f $(BINARY_NAME) $(BINARY_NAME).exe
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@find . -name "*.test" -type f -delete
	@echo "✓ Clean complete"
