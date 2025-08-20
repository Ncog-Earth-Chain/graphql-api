# Download All Solidity Compiler Versions Script (PowerShell)
# This script downloads all Solidity compiler versions listed in internal/solidity/releases.go
# Usage: .\download-all-solc-versions.ps1 [target_directory]

param(
    [string]$TargetDir = ".\solidity-compilers"
)

# Function to write colored output
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

# Function to detect architecture
function Get-Architecture {
    if ([Environment]::Is64BitOperatingSystem) {
        return "x86_64"
    } else {
        return "x86"
    }
}

# Function to get filename for Windows
function Get-Filename {
    param([string]$Version)
    return "solc-windows.exe"
}

# Function to download a single version
function Download-Version {
    param(
        [string]$Version,
        [string]$Filename,
        [string]$TargetPath
    )
    
    $DownloadUrl = "https://github.com/ethereum/solidity/releases/download/$Version/$Filename"
    
    Write-Status "Downloading $Version ($Filename)..."
    
    try {
        # Create temporary file for download
        $TempFile = "$TargetPath.tmp"
        
        # Download with progress
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $DownloadUrl -OutFile $TempFile -UseBasicParsing
        
        # Move to final location
        Move-Item -Path $TempFile -Destination $TargetPath -Force
        
        Write-Success "Downloaded $Version successfully"
        return $true
    }
    catch {
        Write-Error "Failed to download $Version`: $($_.Exception.Message)"
        # Clean up temp file if it exists
        if (Test-Path $TempFile) {
            Remove-Item $TempFile -Force
        }
        return $false
    }
}

# Function to test a downloaded compiler (basic check)
function Test-Compiler {
    param(
        [string]$CompilerPath,
        [string]$Version
    )
    
    Write-Status "Testing $Version..."
    
    try {
        # Check if file exists and has reasonable size (> 1MB)
        $FileInfo = Get-Item $CompilerPath
        if ($FileInfo.Length -lt 1MB) {
            Write-Error "Test failed for $Version (file too small: $($FileInfo.Length) bytes)"
            return $false
        }
        
        Write-Success "Test passed for $Version (file size: $([math]::Round($FileInfo.Length / 1MB, 2)) MB)"
        return $true
    }
    catch {
        Write-Error "Test failed for $Version`: $($_.Exception.Message)"
        return $false
    }
}

# Main script
function Main {
    Write-Status "Starting download of all Solidity compiler versions..."
    Write-Status "Target directory: $TargetDir"
    
    # Detect architecture
    $Arch = Get-Architecture
    Write-Status "Detected architecture: $Arch"
    
    # Create target directory
    if (-not (Test-Path $TargetDir)) {
        Write-Status "Creating target directory: $TargetDir"
        New-Item -ItemType Directory -Path $TargetDir -Force | Out-Null
    }
    
    # Solidity versions from releases.go
    $Versions = @(
        "v0.8.30", "v0.8.29", "v0.8.28", "v0.8.27", "v0.8.26", "v0.8.25", "v0.8.24", "v0.8.23", "v0.8.22", "v0.8.21", "v0.8.20"
        "v0.8.19", "v0.8.18", "v0.8.17", "v0.8.16", "v0.8.15", "v0.8.14", "v0.8.13", "v0.8.12", "v0.8.11", "v0.8.10", "v0.8.9"
        "v0.8.8", "v0.8.7", "v0.8.6", "v0.8.5", "v0.8.4", "v0.8.3", "v0.8.2", "v0.8.1", "v0.8.0"
        "v0.7.6", "v0.7.5", "v0.7.4", "v0.7.3", "v0.7.2", "v0.7.1", "v0.7.0"
        "v0.6.12", "v0.6.11", "v0.6.10", "v0.6.9", "v0.6.8", "v0.6.7", "v0.6.6", "v0.6.5", "v0.6.4", "v0.6.3", "v0.6.2", "v0.6.1", "v0.6.0"
        "v0.5.17", "v0.5.16", "v0.5.15", "v0.5.14", "v0.5.13", "v0.5.12", "v0.5.11", "v0.5.10", "v0.5.9", "v0.5.8", "v0.5.7", "v0.5.6", "v0.5.5", "v0.5.4", "v0.5.3", "v0.5.2", "v0.5.1", "v0.5.0"
        "v0.4.26", "v0.4.25", "v0.4.24", "v0.4.23", "v0.4.22", "v0.4.21", "v0.4.20", "v0.4.19", "v0.4.18", "v0.4.17", "v0.4.16", "v0.4.15", "v0.4.14", "v0.4.13", "v0.4.12", "v0.4.11", "v0.4.10", "v0.4.9", "v0.4.8", "v0.4.7", "v0.4.6", "v0.4.5", "v0.4.4", "v0.4.3", "v0.4.2", "v0.4.1", "v0.4.0"
        "v0.3.6", "v0.3.5", "v0.3.4", "v0.3.3", "v0.3.2", "v0.3.1", "v0.3.0"
        "v0.2.2", "v0.2.1", "v0.2.0"
        "v0.1.7", "v0.1.6", "v0.1.5", "v0.1.4", "v0.1.3", "v0.1.2"
    )
    
    Write-Status "Found $($Versions.Count) versions to download"
    
    # Counters for summary
    $Successful = 0
    $Failed = 0
    $Skipped = 0
    
    # Download each version
    foreach ($Version in $Versions) {
        # Get filename for this platform
        $Filename = Get-Filename $Version
        
        # Build target path
        $TargetPath = Join-Path $TargetDir "solc-$($Version.TrimStart('v')).exe"
        
        # Check if already exists
        if (Test-Path $TargetPath) {
            Write-Warning "Skipping $Version (already exists)"
            $Skipped++
            continue
        }
        
        # Download the version
        if (Download-Version -Version $Version -Filename $Filename -TargetPath $TargetPath) {
            # Test the downloaded compiler
            if (Test-Compiler -CompilerPath $TargetPath -Version $Version) {
                $Successful++
            } else {
                $Failed++
                # Remove failed download
                Remove-Item $TargetPath -Force -ErrorAction SilentlyContinue
            }
        } else {
            $Failed++
        }
        
        # Small delay to be nice to GitHub
        Start-Sleep -Seconds 1
    }
    
    # Print summary
    Write-Host ""
    Write-Status "Download Summary:"
    Write-Success "Successfully downloaded: $Successful"
    Write-Warning "Skipped (already exists): $Skipped"
    if ($Failed -gt 0) {
        Write-Error "Failed: $Failed"
    }
    
    Write-Status "All compilers downloaded to: $TargetDir"
    Write-Status "You can now use these compilers with your GraphQL API by setting the compiler.temp.path configuration to: $TargetDir"
}

# Run main function
Main
