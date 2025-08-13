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
GO_CMD=""
if command -v go &> /dev/null; then
    GO_CMD="go"
elif [ -f "/usr/local/go/bin/go" ]; then
    GO_CMD="/usr/local/go/bin/go"
elif [ -f "/usr/bin/go" ]; then
    GO_CMD="/usr/bin/go"
else
    print_error "Go is not installed or not found in PATH."
    print_error "Please install Go or add it to your PATH."
    print_error "Common Go installation paths:"
    print_error "  - /usr/local/go/bin/go"
    print_error "  - /usr/bin/go"
    print_error "  - ~/go/bin/go"
    exit 1
fi

print_status "Go version: $($GO_CMD version)"
print_status "Using Go from: $(which $GO_CMD 2>/dev/null || echo $GO_CMD)"

# Optionally add Go to PATH for this session if it's not there
if ! command -v go &> /dev/null; then
    print_warning "Go is not in PATH. Adding Go to PATH for this session..."
    export PATH="$PATH:/usr/local/go/bin"
    print_status "Added /usr/local/go/bin to PATH"
fi

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    print_error "go.mod not found. Please run this script from the project root directory."
    exit 1
fi

print_status "Project root directory confirmed"

# Clean previous test artifacts
print_status "Cleaning previous test artifacts..."
$GO_CMD clean -testcache 2>/dev/null || true

# Update dependencies
print_status "Updating Go dependencies..."
$GO_CMD mod tidy
$GO_CMD mod download

# Run tests with verbose output and coverage
echo ""
echo "🧪 Running Registry Package Tests..."
echo "-----------------------------------"

# Test registry components
print_status "Testing Package Registry..."
$GO_CMD test -v ./internal/registry/ -run TestPackageRegistry

print_status "Testing Dependency Resolver..."
$GO_CMD test -v ./internal/registry/ -run TestDependencyResolver

print_status "Testing Version Strategy..."
$GO_CMD test -v ./internal/registry/ -run TestVersionStrategy

print_status "Testing Contract Orchestrator..."
$GO_CMD test -v ./internal/registry/ -run TestContractOrchestrator

print_status "Testing Mock Compiler..."
$GO_CMD test -v ./internal/registry/ -run TestMockCompiler

echo ""
echo "🔧 Running Enhanced Contract Verifier Tests..."
echo "--------------------------------------------"

# Test enhanced contract verifier
print_status "Testing Enhanced Contract Verifier..."
$GO_CMD test -v ./internal/repository/ -run TestEnhancedContractVerifier

echo ""
echo "📊 Running All Tests with Coverage..."
echo "-----------------------------------"

# Run all tests with coverage
print_status "Running comprehensive test suite..."
$GO_CMD test -v -coverprofile=coverage.out ./internal/registry/ ./internal/repository/

# Generate coverage report
if [ -f "coverage.out" ]; then
    print_status "Generating coverage report..."
    $GO_CMD tool cover -html=coverage.out -o coverage.html
    print_success "Coverage report generated: coverage.html"
    
    # Show coverage summary
    echo ""
    echo "📈 Coverage Summary:"
    echo "-------------------"
    $GO_CMD tool cover -func=coverage.out
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
