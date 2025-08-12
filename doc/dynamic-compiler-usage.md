# Dynamic Solidity Compiler Version Selection

This guide explains how to use the dynamic Solidity compiler version selection feature for contract verification.

## Overview

The API now supports specifying which Solidity compiler version to use when validating smart contracts. **The system automatically downloads missing compiler versions** when needed, making it much easier to use.

This is useful when:

- Your contract was compiled with a specific Solidity version
- You need to match the exact compilation settings used during deployment
- You want to validate contracts compiled with different Solidity versions

## Configuration

### 1. Basic Configuration

The API will automatically download and manage Solidity compilers. You only need to configure the storage directory:

In your `apiserver.json`:

```json
{
  "compiler": {
    "temp": "C:\\Tools\\solidity",  // Directory where compilers will be stored
    "sol": "C:\\Tools\\solidity\\solc-0.8.19.exe"  // Default compiler (optional)
  }
}
```

**Note:** The `sol` field is optional. If not specified, the system will download a default version when needed.

### 2. Automatic Download

The system automatically:
- Downloads missing compiler versions from GitHub releases
- Supports Windows, Linux, and macOS
- Makes files executable on Unix-like systems
- Tests downloaded compilers to ensure they work
- Caches compiler paths for future use

## Usage

### 1. Check Available Compiler Versions

Query to see which compiler versions are available:

```graphql
query {
  availableCompilerVersions
}
```

Response:
```json
{
  "data": {
    "availableCompilerVersions": [
      "v0.8.19",
      "v0.7.6", 
      "v0.6.12"
    ]
  }
}
```

### 2. Pre-download Compiler Versions (Optional)

You can pre-download specific compiler versions to avoid delays during validation:

```graphql
mutation PreDownload($version: String!) {
  preDownloadCompilerVersion(version: $version)
}
```

Variables:
```json
{
  "version": "v0.8.19"
}
```

### 3. Validate Contract with Specific Compiler Version

Use the `compilerVersion` field in your validation mutation. **If the version doesn't exist, it will be automatically downloaded:**

```graphql
mutation Validate($sc: ContractValidationInput!) {
  validateContract(contract: $sc) {
    address
    name
    version
    compiler
    compilerVersion
    validated
  }
}
```

Variables:
```json
{
  "sc": {
    "address": "0xYourContractAddress",
    "name": "MyToken",
    "version": "v1.0.0",
    "compilerVersion": "v0.8.19",  // Will be auto-downloaded if not found
    "optimized": true,
    "optimizeRuns": 200,
    "sourceCode": "pragma solidity ^0.8.19;\n\ncontract MyToken {\n    // Your contract code here\n}"
  }
}
```

### 4. Compiler Version Formats

The `compilerVersion` field accepts these formats:
- `"v0.8.19"` (with 'v' prefix)
- `"0.8.19"` (without 'v' prefix)
- `""` (empty string uses default compiler)

### 5. Automatic Download Behavior

When you specify a compiler version that doesn't exist:

1. **Automatic Download**: The system downloads the compiler from GitHub releases
2. **Platform Detection**: Automatically selects the correct binary for your OS
3. **Installation**: Places the compiler in your configured `compiler.temp` directory
4. **Testing**: Verifies the compiler works by running `--version`
5. **Caching**: Stores the path for future use
6. **Logging**: Provides detailed logs about the download process

## Example Scenarios

### Scenario 1: First-time Use (Automatic Download)

```json
{
  "sc": {
    "address": "0xNewContractAddress",
    "name": "NewToken",
    "compilerVersion": "v0.8.19",  // Will be downloaded automatically
    "sourceCode": "pragma solidity ^0.8.19;\n\ncontract NewToken {\n    // Contract code\n}"
  }
}
```

**What happens:**
1. System checks if `solc-0.8.19` exists
2. If not found, downloads from `https://github.com/ethereum/solidity/releases/download/v0.8.19/solc-windows.exe`
3. Makes it executable and tests it
4. Proceeds with validation

### Scenario 2: Legacy Contract (Solidity 0.6.x)

```json
{
  "sc": {
    "address": "0xLegacyContractAddress",
    "name": "LegacyToken",
    "compilerVersion": "v0.6.12",  // Will be auto-downloaded
    "sourceCode": "pragma solidity ^0.6.12;\n\ncontract LegacyToken {\n    // Legacy contract code\n}"
  }
}
```

### Scenario 3: Use Default Compiler

```json
{
  "sc": {
    "address": "0xContractAddress",
    "name": "MyContract",
    "compilerVersion": "",  // Uses default or downloads if none configured
    "sourceCode": "pragma solidity ^0.8.19;\n\ncontract MyContract {\n    // Contract code\n}"
  }
}
```

## Supported Platforms

The automatic download supports:

- **Windows**: Downloads `solc-windows.exe`
- **macOS**: Downloads `solc-macos`
- **Linux**: Downloads `solc-static-linux`

## Troubleshooting

### Download Fails

If automatic download fails:

1. **Check Internet Connection**: Ensure the API server has internet access
2. **Check Disk Space**: Ensure there's enough space in the `compiler.temp` directory
3. **Check Permissions**: Ensure the API can write to the compiler directory
4. **Check Firewall**: Ensure GitHub releases are accessible
5. **Check Logs**: Look for detailed error messages in the API logs

### Compilation Fails

If compilation fails with the downloaded version:

1. Verify the Solidity version in your source code matches the compiler version
2. Check that the pragma statement is compatible
3. Ensure all import paths are correct
4. Review the compilation error messages in the API logs

### Manual Installation

If automatic download doesn't work, you can still manually install compilers:

**Windows:**
```powershell
# Download manually and place in compiler.temp directory
# Use the PowerShell script: scripts/install-solidity-versions.ps1
```

**Linux/macOS:**
```bash
# Download from https://github.com/ethereum/solidity/releases
# Place in your compiler.temp directory
```

## Best Practices

1. **Pre-download Common Versions**: Use the `preDownloadCompilerVersion` mutation to prepare compilers
2. **Specify Exact Versions**: Always specify the exact compiler version used during deployment
3. **Match Pragma Statements**: Ensure your source code pragma matches the compiler version
4. **Monitor Logs**: Watch API logs for download and compilation information
5. **Regular Updates**: Keep your compiler binaries updated for security

## API Response Fields

When a contract is successfully validated, the response includes:

- `compiler`: The full compiler information (e.g., "Solidity 0.8.19")
- `compilerVersion`: The specific version used (e.g., "v0.8.19")
- `validated`: Timestamp when validation occurred
- `abi`: The contract ABI extracted during compilation

## Performance Notes

- **First Download**: May take 1-2 minutes depending on internet speed
- **Subsequent Uses**: Instant access to cached compilers
- **Concurrent Downloads**: Multiple versions can be downloaded simultaneously
- **Storage**: Each compiler version is typically 10-20MB
