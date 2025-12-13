# Stargate Backend Makefile

.PHONY: build build-test clean test docker-build docker-run help

# Default target
all: build

# Build main application
build:
	@echo "Building main application..."
	go build -o stargate-backend .
	@echo "✅ Main application built: stargate-backend"

# Build test utilities
build-test:
	@echo "Building test utilities..."
	@mkdir -p cmd/tests
	go build -o cmd/tests/build-verification ./cmd/tests/
	@echo "✅ Build verification utility built: cmd/tests/build-verification"

# Build all binaries
build-all: build build-test

# Run tests
test:
	@echo "Running smart contract tests..."
	go test ./core/smart_contract -v

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f stargate-backend
	rm -f cmd/tests/build-verification
	@echo "✅ Clean completed"

# Run build verification
verify: build-test
	@echo "Running build verification..."
	./cmd/tests/build-verification

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t stargate-backend .
	@echo "✅ Docker image built"

# Docker run
docker-run: docker-build
	@echo "Running Docker container..."
	docker run -p 3001:3001 stargate-backend

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	go mod tidy
	go mod download
	@echo "✅ Development setup complete"

# Help target
help:
	@echo "Stargate Backend Build System"
	@echo ""
	@echo "Available targets:"
	@echo "  build        - Build main application"
	@echo "  build-test   - Build test utilities"
	@echo "  build-all    - Build all binaries"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  verify       - Run build verification"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Build and run Docker container"
	@echo "  dev-setup    - Set up development environment"
	@echo "  help         - Show this help message"