# Solidity Compiler Download Scripts

This directory contains scripts to download all Solidity compiler versions for your contract verification API.

## Available Scripts

### 1. `download-all-solc-versions.sh` (Linux/macOS)
Bash script for Unix-like systems (Linux, macOS, WSL).

### 2. `download-all-solc-versions.ps1` (Windows)
PowerShell script for Windows systems.

### 3. `test-auto-download.ps1` (Windows)
PowerShell script to test the automatic download feature via GraphQL API.

## Usage

### Linux/macOS
```bash
# Make the script executable (first time only)
chmod +x scripts/download-all-solc-versions.sh

# Download to default directory (./solidity-compilers)
./scripts/download-all-solc-versions.sh

# Download to custom directory
./scripts/download-all-solc-versions.sh /path/to/your/compilers
```

### Windows
```powershell
# Download to default directory (.\solidity-compilers)
.\scripts\download-all-solc-versions.ps1

# Download to custom directory
.\scripts\download-all-solc-versions.ps1 -TargetDir "C:\path\to\your\compilers"
```

## What These Scripts Do

1. **Detect Platform**: Automatically detect your operating system and architecture
2. **Download All Versions**: Download all 58 Solidity compiler versions from `internal/solidity/releases.go`:
   - v0.7.5 through v0.7.0
   - v0.6.12 through v0.6.0
   - v0.5.17 through v0.5.0
   - v0.4.26 through v0.4.0
3. **Cross-Platform Support**: Download the correct binary for your platform:
   - **Windows**: `solc-windows.exe`
   - **macOS**: `solc-macos` (Intel) or `solc-macos-arm64` (Apple Silicon)
   - **Linux**: `solc-static-linux` (x86_64) or `solc-static-linux-aarch64` (ARM64)
4. **Smart Naming**: Files are named as `solc-{version}` (e.g., `solc-0.8.19`)
5. **Skip Existing**: Skip downloads for files that already exist
6. **Test Downloads**: Verify downloaded files (size check on Windows, execution test on Unix)
7. **Progress Tracking**: Show download progress and summary statistics

## Configuration

After downloading, configure your GraphQL API to use these compilers:

### Update your config file (e.g., `config.json`):
```json
{
  "compiler": {
    "temp_path": "./solidity-compilers",
    "default_sol_compiler_path": "./solidity-compilers/solc-0.8.19"
  }
}
```

### Or set environment variables:
```bash
export COMPILER_TEMP_PATH="./solidity-compilers"
export COMPILER_DEFAULT_SOL_COMPILER_PATH="./solidity-compilers/solc-0.8.19"
```

## Features

- **Automatic Platform Detection**: Works on Windows, Linux, and macOS
- **Architecture Support**: Supports x86_64 and ARM64 architectures
- **Resume Downloads**: Skip already downloaded files
- **Error Handling**: Comprehensive error handling with detailed messages
- **Progress Feedback**: Colored output with status updates
- **File Testing**: Verify downloaded files are valid
- **Rate Limiting**: 1-second delay between downloads to be respectful to GitHub

## Expected Output

```
[INFO] Starting download of all Solidity compiler versions...
[INFO] Target directory: ./solidity-compilers
[INFO] Detected platform: linux-x86_64
[INFO] Found 58 versions to download
[INFO] Downloading v0.7.5 (solc-static-linux)...
[SUCCESS] Downloaded v0.7.5 successfully
[INFO] Testing v0.7.5...
[SUCCESS] Test passed for v0.7.5
...
[INFO] Download Summary:
[SUCCESS] Successfully downloaded: 58
[WARNING] Skipped (already exists): 0
[ERROR] Failed: 0
[INFO] All compilers downloaded to: ./solidity-compilers
```

## Troubleshooting

### Permission Denied (Linux/macOS)
```bash
chmod +x scripts/download-all-solc-versions.sh
```

### PowerShell Execution Policy (Windows)
```powershell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

### Network Issues
- Check your internet connection
- Some corporate networks may block GitHub downloads
- Try using a VPN if needed

### Disk Space
- Each compiler is approximately 10-20MB
- Total space needed: ~1-2GB for all versions
- Ensure you have sufficient disk space

### Partial Downloads
- The script will skip existing files
- Run the script again to download missing versions
- Failed downloads are automatically cleaned up

## Integration with GraphQL API

Once downloaded, your GraphQL API will automatically use these compilers:

1. **Automatic Detection**: The API will find compilers in the configured directory
2. **Dynamic Selection**: Use `compilerVersion` field in `validateContract` mutation
3. **Fallback**: If a specific version isn't found, it will auto-download it

### Example GraphQL Usage:
```graphql
mutation ValidateContract($contract: ContractValidationInput!) {
  validateContract(contract: $contract) {
    address
    compilerVersion
    validated
  }
}
```

Variables:
```json
{
  "contract": {
    "address": "0xYourContractAddress",
    "compilerVersion": "v0.8.19",
    "sourceCode": "pragma solidity ^0.8.19; ..."
  }
}
```

## Alternative: Automatic Download

The GraphQL API also supports automatic download of compilers on-demand. You don't need to pre-download all versions - just specify the version you need and it will be downloaded automatically.

See `doc/dynamic-compiler-usage.md` for more details on the automatic download feature.
