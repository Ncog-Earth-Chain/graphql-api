#!/bin/bash

# Download All Solidity Compiler Versions Script
# This script downloads all Solidity compiler versions listed in internal/solidity/releases.go
# Usage: ./download-all-solc-versions.sh [target_directory]

set -e  # Exit on any error

# Default target directory
DEFAULT_TARGET_DIR="./solidity-compilers"
TARGET_DIR="${1:-$DEFAULT_TARGET_DIR}"

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

# Function to detect OS and architecture
detect_platform() {
    case "$(uname -s)" in
        Linux*)     
            OS="linux"
            ARCH="$(uname -m)"
            if [[ "$ARCH" == "x86_64" ]]; then
                ARCH="x86_64"
            elif [[ "$ARCH" == "aarch64" ]] || [[ "$ARCH" == "arm64" ]]; then
                ARCH="aarch64"
            else
                print_error "Unsupported architecture: $ARCH"
                exit 1
            fi
            ;;
        Darwin*)    
            OS="macos"
            ARCH="$(uname -m)"
            if [[ "$ARCH" == "x86_64" ]]; then
                ARCH="x86_64"
            elif [[ "$ARCH" == "arm64" ]]; then
                ARCH="arm64"
            else
                print_error "Unsupported architecture: $ARCH"
                exit 1
            fi
            ;;
        CYGWIN*|MINGW32*|MSYS*|MINGW*)    
            OS="windows"
            ARCH="x86_64"
            ;;
        *)          
            print_error "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac
    
    print_status "Detected platform: $OS-$ARCH"
}

# Function to get filename for platform
get_filename() {
    local version="$1"
    case "$OS" in
        windows)
            echo "solc-windows.exe"
            ;;
        darwin)
            if [[ "$ARCH" == "arm64" ]]; then
                echo "solc-macos-arm64"
            else
                echo "solc-macos"
            fi
            ;;
        linux)
            if [[ "$ARCH" == "aarch64" ]]; then
                echo "solc-static-linux-aarch64"
            else
                echo "solc-static-linux"
            fi
            ;;
        *)
            print_error "Unknown OS: $OS"
            exit 1
            ;;
    esac
}

# Function to download a single version
download_version() {
    local version="$1"
    local filename="$2"
    local target_path="$3"
    
    local download_url="https://github.com/ethereum/solidity/releases/download/${version}/${filename}"
    
    print_status "Downloading $version ($filename)..."
    
    # Create temporary file for download
    local temp_file="${target_path}.tmp"
    
    # Download with progress indicator
    if command -v curl >/dev/null 2>&1; then
        if curl -L -o "$temp_file" --progress-bar "$download_url"; then
            # Move to final location
            mv "$temp_file" "$target_path"
            # Make executable on Unix-like systems
            if [[ "$OS" != "windows" ]]; then
                chmod +x "$target_path"
            fi
            print_success "Downloaded $version successfully"
            return 0
        else
            rm -f "$temp_file"
            return 1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if wget -O "$temp_file" "$download_url"; then
            # Move to final location
            mv "$temp_file" "$target_path"
            # Make executable on Unix-like systems
            if [[ "$OS" != "windows" ]]; then
                chmod +x "$target_path"
            fi
            print_success "Downloaded $version successfully"
            return 0
        else
            rm -f "$temp_file"
            return 1
        fi
    else
        print_error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi
}

# Function to test a downloaded compiler
test_compiler() {
    local compiler_path="$1"
    local version="$2"
    
    print_status "Testing $version..."
    
    if [[ "$OS" == "windows" ]]; then
        # On Windows, we can't easily test without wine or similar
        print_warning "Skipping test for Windows binary (requires wine or similar)"
        return 0
    else
        # Test the compiler
        if "$compiler_path" --version >/dev/null 2>&1; then
            print_success "Test passed for $version"
            return 0
        else
            print_error "Test failed for $version"
            return 1
        fi
    fi
}

# Main script
main() {
    print_status "Starting download of all Solidity compiler versions..."
    print_status "Target directory: $TARGET_DIR"
    
    # Detect platform
    detect_platform
    
    # Create target directory
    if [[ ! -d "$TARGET_DIR" ]]; then
        print_status "Creating target directory: $TARGET_DIR"
        mkdir -p "$TARGET_DIR"
    fi
    
    # Solidity versions from releases.go
    versions=(
        "v0.8.30" "v0.8.29" "v0.8.28" "v0.8.27" "v0.8.26" "v0.8.25" "v0.8.24" "v0.8.23" "v0.8.22" "v0.8.21" "v0.8.20"
        "v0.8.19" "v0.8.18" "v0.8.17" "v0.8.16" "v0.8.15" "v0.8.14" "v0.8.13" "v0.8.12" "v0.8.11" "v0.8.10" "v0.8.9"
        "v0.8.8" "v0.8.7" "v0.8.6" "v0.8.5" "v0.8.4" "v0.8.3" "v0.8.2" "v0.8.1" "v0.8.0"
        "v0.7.6" "v0.7.5" "v0.7.4" "v0.7.3" "v0.7.2" "v0.7.1" "v0.7.0"
        "v0.6.12" "v0.6.11" "v0.6.10" "v0.6.9" "v0.6.8" "v0.6.7" "v0.6.6" "v0.6.5" "v0.6.4" "v0.6.3" "v0.6.2" "v0.6.1" "v0.6.0"
        "v0.5.17" "v0.5.16" "v0.5.15" "v0.5.14" "v0.5.13" "v0.5.12" "v0.5.11" "v0.5.10" "v0.5.9" "v0.5.8" "v0.5.7" "v0.5.6" "v0.5.5" "v0.5.4" "v0.5.3" "v0.5.2" "v0.5.1" "v0.5.0"
        "v0.4.26" "v0.4.25" "v0.4.24" "v0.4.23" "v0.4.22" "v0.4.21" "v0.4.20" "v0.4.19" "v0.4.18" "v0.4.17" "v0.4.16" "v0.4.15" "v0.4.14" "v0.4.13" "v0.4.12" "v0.4.11" "v0.4.10" "v0.4.9" "v0.4.8" "v0.4.7" "v0.4.6" "v0.4.5" "v0.4.4" "v0.4.3" "v0.4.2" "v0.4.1" "v0.4.0"
        "v0.3.6" "v0.3.5" "v0.3.4" "v0.3.3" "v0.3.2" "v0.3.1" "v0.3.0"
        "v0.2.2" "v0.2.1" "v0.2.0"
        "v0.1.7" "v0.1.6" "v0.1.5" "v0.1.4" "v0.1.3" "v0.1.2"
    )

    print_status "Found ${#versions[@]} versions to download"
    
    # Counters for summary
    successful=0
    failed=0
    skipped=0
    
    # Download each version
    for version in "${versions[@]}"; do
        # Get filename for this platform
        filename=$(get_filename "$version")
        
        # Build target path
        local target_path="$TARGET_DIR/solc-${version#v}"
        if [[ "$OS" == "windows" ]]; then
            target_path="${target_path}.exe"
        fi
        
        # Check if already exists
        if [[ -f "$target_path" ]]; then
            print_warning "Skipping $version (already exists)"
            ((skipped++))
            continue
        fi
        
        # Download the version
        if download_version "$version" "$filename" "$target_path"; then
            # Test the downloaded compiler
            if test_compiler "$target_path" "$version"; then
                ((successful++))
            else
                ((failed++))
                # Remove failed download
                rm -f "$target_path"
            fi
        else
            print_error "Failed to download $version"
            ((failed++))
        fi
        
        # Small delay to be nice to GitHub
        sleep 1
    done
    
    # Print summary
    echo
    print_status "Download Summary:"
    print_success "Successfully downloaded: $successful"
    print_warning "Skipped (already exists): $skipped"
    if [[ $failed -gt 0 ]]; then
        print_error "Failed: $failed"
    fi
    
    print_status "All compilers downloaded to: $TARGET_DIR"
    print_status "You can now use these compilers with your GraphQL API by setting the compiler.temp.path configuration to: $TARGET_DIR"
}

# Run main function
main "$@"
