# Stargate Backend Makefile

.PHONY: build build-migrate clean test docker-build docker-run help

# Default target
all: build

# Build main application
build:
	@echo "Building main application..."
	go build -o stargate-backend .
	@echo "✅ Main application built: stargate-backend"

# Build the Postgres→SQLite migration utility (single-binary transition helper)
build-migrate:
	@echo "Building Postgres to SQLite migration utility..."
	@mkdir -p bin
	go build -o bin/migrate-pg-to-sqlite ./cmd/migrate-pg-to-sqlite
	@echo "✅ Migration utility built: bin/migrate-pg-to-sqlite"
	@echo "   Run with: ./bin/migrate-pg-to-sqlite --help"

# Build all binaries (migration helper is the relevant one for single-binary transition)
build-all: build build-migrate

# Run tests
test:
	@echo "Running smart contract tests..."
	go test ./core/smart_contract -v

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f stargate-backend
	rm -f bin/migrate-pg-to-sqlite
	@echo "✅ Clean completed"

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
	@echo "  build         - Build main application (backend only)"
	@echo "  build-migrate - Build Postgres→SQLite migration utility (single-binary transition)"
	@echo "  build-all     - Build all binaries (incl. migrator)"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  docker-build  - Build Docker image (legacy backend image)"
	@echo "  docker-run    - Build and run Docker container"
	@echo "  dev-setup     - Set up development environment"
	@echo "  help          - Show this help message"
	@echo ""
	@echo "For the unified single-binary image use the root Makefile:"
	@echo "  make single-binary   # local build with embedded frontend"
	@echo "  make docker          # build unified stargate:latest image"