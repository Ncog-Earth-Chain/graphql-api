#!/bin/bash

# Enhanced Contract Verification System - Test Runner
# For Ubuntu/Linux servers

set -e

echo "🚀 Starting Enhanced Contract Verification System Tests"
echo "=================================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed. Please install Go first."
    exit 1
fi

print_status "Go version: $(go version)"

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    print_error "go.mod not found. Please run this script from the project root directory."
    exit 1
fi

print_status "Project root directory confirmed"

# Clean previous test artifacts
print_status "Cleaning previous test artifacts..."
go clean -testcache 2>/dev/null || true

# Update dependencies
print_status "Updating Go dependencies..."
go mod tidy
go mod download

# Run tests with verbose output and coverage
echo ""
echo "🧪 Running Registry Package Tests..."
echo "-----------------------------------"

# Test registry components
print_status "Testing Package Registry..."
go test -v ./internal/registry/ -run TestPackageRegistry

print_status "Testing Dependency Resolver..."
go test -v ./internal/registry/ -run TestDependencyResolver

print_status "Testing Version Strategy..."
go test -v ./internal/registry/ -run TestVersionStrategy

print_status "Testing Contract Orchestrator..."
go test -v ./internal/registry/ -run TestContractOrchestrator

print_status "Testing Mock Compiler..."
go test -v ./internal/registry/ -run TestMockCompiler

echo ""
echo "🔧 Running Enhanced Contract Verifier Tests..."
echo "--------------------------------------------"

# Test enhanced contract verifier
print_status "Testing Enhanced Contract Verifier..."
go test -v ./internal/repository/ -run TestEnhancedContractVerifier

echo ""
echo "📊 Running All Tests with Coverage..."
echo "-----------------------------------"

# Run all tests with coverage
print_status "Running comprehensive test suite..."
go test -v -coverprofile=coverage.out ./internal/registry/ ./internal/repository/

# Generate coverage report
if [ -f "coverage.out" ]; then
    print_status "Generating coverage report..."
    go tool cover -html=coverage.out -o coverage.html
    print_success "Coverage report generated: coverage.html"
    
    # Show coverage summary
    echo ""
    echo "📈 Coverage Summary:"
    echo "-------------------"
    go tool cover -func=coverage.out
fi

echo ""
echo "✅ All tests completed!"
echo ""

# Check for any test failures
if [ $? -eq 0 ]; then
    print_success "All tests passed successfully!"
else
    print_error "Some tests failed. Please check the output above."
    exit 1
fi

echo ""
echo "🚀 Next Steps:"
echo "1. Review test coverage report: coverage.html"
echo "2. Run integration tests (if available)"
echo "3. Deploy to your Ubuntu server"
echo "4. Update GraphQL API endpoints"
echo "5. Test with real contracts"

echo ""
print_status "Test execution completed successfully!"
