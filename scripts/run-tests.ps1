# Enhanced Contract Verification System - Test Runner
# For Windows PowerShell

param(
    [switch]$Verbose,
    [switch]$Coverage,
    [switch]$Help
)

if ($Help) {
    Write-Host "Enhanced Contract Verification System - Test Runner" -ForegroundColor Cyan
    Write-Host "Usage: .\run-tests.ps1 [-Verbose] [-Coverage] [-Help]" -ForegroundColor White
    Write-Host ""
    Write-Host "Options:" -ForegroundColor White
    Write-Host "  -Verbose    Show detailed test output" -ForegroundColor White
    Write-Host "  -Coverage   Generate coverage report" -ForegroundColor White
    Write-Host "  -Help       Show this help message" -ForegroundColor White
    exit 0
}

Write-Host "🚀 Starting Enhanced Contract Verification System Tests" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan

# Function to print colored output
function Write-Status {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Blue
}

function Write-Success {
    param([string]$Message)
    Write-Host "[SUCCESS] $Message" -ForegroundColor Green
}

function Write-Warning {
    param([string]$Message)
    Write-Host "[WARNING] $Message" -ForegroundColor Yellow
}

function Write-Error {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

# Check if Go is installed
try {
    $goVersion = go version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Go command failed"
    }
    Write-Status "Go version: $goVersion"
} catch {
    Write-Error "Go is not installed. Please install Go first."
    exit 1
}

# Check if we're in the right directory
if (-not (Test-Path "go.mod")) {
    Write-Error "go.mod not found. Please run this script from the project root directory."
    exit 1
}

Write-Status "Project root directory confirmed"

# Clean previous test artifacts
Write-Status "Cleaning previous test artifacts..."
go clean -testcache 2>$null

# Update dependencies
Write-Status "Updating Go dependencies..."
go mod tidy
go mod download

# Run tests with verbose output and coverage
Write-Host ""
Write-Host "🧪 Running Registry Package Tests..." -ForegroundColor Yellow
Write-Host "-----------------------------------" -ForegroundColor Yellow

# Test registry components
Write-Status "Testing Package Registry..."
if ($Verbose) {
    go test -v ./internal/registry/ -run TestPackageRegistry
} else {
    go test ./internal/registry/ -run TestPackageRegistry
}

Write-Status "Testing Dependency Resolver..."
if ($Verbose) {
    go test -v ./internal/registry/ -run TestDependencyResolver
} else {
    go test ./internal/registry/ -run TestDependencyResolver
}

Write-Status "Testing Version Strategy..."
if ($Verbose) {
    go test -v ./internal/registry/ -run TestVersionStrategy
} else {
    go test ./internal/registry/ -run TestVersionStrategy
}

Write-Status "Testing Contract Orchestrator..."
if ($Verbose) {
    go test -v ./internal/registry/ -run TestContractOrchestrator
} else {
    go test ./internal/registry/ -run TestContractOrchestrator
}

Write-Status "Testing Mock Compiler..."
if ($Verbose) {
    go test -v ./internal/registry/ -run TestMockCompiler
} else {
    go test ./internal/registry/ -run TestMockCompiler
}

Write-Host ""
Write-Host "🔧 Running Enhanced Contract Verifier Tests..." -ForegroundColor Yellow
Write-Host "--------------------------------------------" -ForegroundColor Yellow

# Test enhanced contract verifier
Write-Status "Testing Enhanced Contract Verifier..."
if ($Verbose) {
    go test -v ./internal/repository/ -run TestEnhancedContractVerifier
} else {
    go test ./internal/repository/ -run TestEnhancedContractVerifier
}

Write-Host ""
Write-Host "📊 Running All Tests with Coverage..." -ForegroundColor Yellow
Write-Host "-----------------------------------" -ForegroundColor Yellow

# Run all tests with coverage
Write-Status "Running comprehensive test suite..."
if ($Coverage) {
    go test -v -coverprofile=coverage.out ./internal/registry/ ./internal/repository/
    
    # Generate coverage report
    if (Test-Path "coverage.out") {
        Write-Status "Generating coverage report..."
        go tool cover -html=coverage.out -o coverage.html
        Write-Success "Coverage report generated: coverage.html"
        
        # Show coverage summary
        Write-Host ""
        Write-Host "📈 Coverage Summary:" -ForegroundColor Yellow
        Write-Host "-------------------" -ForegroundColor Yellow
        go tool cover -func=coverage.out
    }
} else {
    if ($Verbose) {
        go test -v ./internal/registry/ ./internal/repository/
    } else {
        go test ./internal/registry/ ./internal/repository/
    }
}

Write-Host ""
Write-Host "✅ All tests completed!" -ForegroundColor Green
Write-Host ""

# Check for any test failures
if ($LASTEXITCODE -eq 0) {
    Write-Success "All tests passed successfully!"
} else {
    Write-Error "Some tests failed. Please check the output above."
    exit 1
}

Write-Host ""
Write-Host "🚀 Next Steps:" -ForegroundColor Cyan
Write-Host "1. Review test coverage report: coverage.html (if generated)" -ForegroundColor White
Write-Host "2. Run integration tests (if available)" -ForegroundColor White
Write-Host "3. Deploy to your Ubuntu server" -ForegroundColor White
Write-Host "4. Update GraphQL API endpoints" -ForegroundColor White
Write-Host "5. Test with real contracts" -ForegroundColor White

Write-Host ""
Write-Status "Test execution completed successfully!"
