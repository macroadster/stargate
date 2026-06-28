#!/bin/bash

# Optimized Docker Build Script for Stargate
# This script demonstrates the optimized Docker builds with proper layer caching

set -e

echo "🚀 Building optimized Stargate Docker images..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    print_error "Docker is not installed or not in PATH"
    exit 1
fi

# Build unified single-binary image (primary for feature/single-binary)
print_status "Building unified single-binary image (stargate:latest) with layer caching..."
docker build \
    -t stargate:latest \
    -f Dockerfile \
    .

# Show image sizes
print_status "Image sizes after optimization:"
docker images | grep stargate | head -10

print_warning "To test the builds locally:"
echo "  docker run -p 3001:3001 stargate:latest"
echo "  (Legacy separate images can be built with make backend-legacy / make frontend-legacy if needed)"

print_status "✅ Optimized builds completed successfully!"
echo ""
echo "🔧 Key optimizations applied:"
echo "  • Proper .dockerignore files to exclude unnecessary files"
echo "  • Layer caching by copying dependencies first"
echo "  • Multi-stage builds to reduce final image size"
echo "  • Security hardening with non-root users"
echo "  • Health checks for backend container"
echo "  • Deterministic dependency installation"