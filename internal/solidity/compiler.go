// Package solidity implements Solidity processor used to analyze
// and verify Solidity based contracts.
package solidity

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:generate sh ./tools/compile_releases.sh "../../../solidity"

// CompilerManager manages multiple Solidity compiler versions.
//
// mu guards compilers. One repository instance is shared process-wide
// (repository.R(), a sync.Once singleton), and graphql-go resolves a request's root fields
// concurrently -- AvailableCompilerVersions returns an error, which marks the field Async,
// so a single HTTP POST aliasing that field N times fans out across up to MaxParallelism
// (default 10) goroutines, all of them walking every entry of solidityReleases and writing
// this map. Without the lock the Go runtime detects the collision and calls fatal(), which
// no panic handler can recover: the whole API process dies. Reproduced with
// "fatal error: concurrent map writes" from 16 goroutines against this map.
type CompilerManager struct {
	basePath    string
	mu          sync.RWMutex
	compilers   map[string]string
	defaultPath string
}

// lookupCompiler reads the resolved-path cache under the read lock.
func (cm *CompilerManager) lookupCompiler(version string) (string, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	path, ok := cm.compilers[version]
	return path, ok
}

// rememberCompiler records a resolved path under the write lock.
func (cm *CompilerManager) rememberCompiler(version, path string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.compilers[version] = path
}

// NewCompilerManager creates a new compiler manager
func NewCompilerManager(basePath, defaultPath string) *CompilerManager {
	return &CompilerManager{
		basePath:    basePath,
		compilers:   make(map[string]string),
		defaultPath: defaultPath,
	}
}

// GetCompilerPath returns the path to the specified Solidity compiler version
func (cm *CompilerManager) GetCompilerPath(version string) (string, error) {
	// If no version specified, use default
	if version == "" {
		return cm.defaultPath, nil
	}

	// Normalize version format (remove 'v' prefix if present)
	normalizedVersion := strings.TrimPrefix(version, "v")

	if path, ok := cm.resolveLocal(normalizedVersion); ok {
		return path, nil
	}

	// If not found, try to download and install it
	return cm.downloadAndInstallCompiler(normalizedVersion)
}

// resolveLocal reports where an already-installed compiler for this version lives, WITHOUT
// reaching the network. Split out of GetCompilerPath so availability can be asked as a
// question rather than as an instruction to install -- see IsVersionSupported.
func (cm *CompilerManager) resolveLocal(normalizedVersion string) (string, bool) {
	// Check if we have this version cached
	if path, exists := cm.lookupCompiler(normalizedVersion); exists {
		return path, true
	}

	// Try to find the compiler in the base path
	compilerPath := cm.buildCompilerPath(normalizedVersion)

	// Check if the compiler exists and is executable
	if _, err := exec.LookPath(compilerPath); err == nil {
		cm.rememberCompiler(normalizedVersion, compilerPath)
		return compilerPath, true
	}

	// Try alternative naming patterns
	for _, altPath := range cm.getAlternativePaths(normalizedVersion) {
		if _, err := exec.LookPath(altPath); err == nil {
			cm.rememberCompiler(normalizedVersion, altPath)
			return altPath, true
		}
	}

	return "", false
}

// buildCompilerPath builds the expected compiler path for the given version
func (cm *CompilerManager) buildCompilerPath(version string) string {
	compilerPath := filepath.Join(cm.basePath, fmt.Sprintf("solc-%s", version))

	// On Windows, add .exe extension
	if runtime.GOOS == "windows" && !strings.HasSuffix(compilerPath, ".exe") {
		compilerPath += ".exe"
	}

	return compilerPath
}

// getAlternativePaths returns alternative naming patterns for the compiler
func (cm *CompilerManager) getAlternativePaths(version string) []string {
	paths := []string{
		filepath.Join(cm.basePath, fmt.Sprintf("solc-v%s", version)),
		filepath.Join(cm.basePath, fmt.Sprintf("solc_%s", version)),
	}

	// Add .exe extensions for Windows
	if runtime.GOOS == "windows" {
		paths = append(paths,
			filepath.Join(cm.basePath, fmt.Sprintf("solc-%s.exe", version)),
			filepath.Join(cm.basePath, fmt.Sprintf("solc-v%s.exe", version)),
			filepath.Join(cm.basePath, fmt.Sprintf("solc_%s.exe", version)),
		)
	}

	return paths
}

// downloadAndInstallCompiler downloads and installs a specific Solidity version
func (cm *CompilerManager) downloadAndInstallCompiler(version string) (string, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(cm.basePath, 0755); err != nil {
		return "", fmt.Errorf("failed to create compiler directory: %w", err)
	}

	// Determine the download URL based on platform
	downloadURL, err := cm.getDownloadURL(version)
	if err != nil {
		return "", err
	}

	// Build the target path
	targetPath := cm.buildCompilerPath(version)

	// Download the compiler
	if err := cm.downloadFile(downloadURL, targetPath); err != nil {
		return "", fmt.Errorf("failed to download Solidity %s: %w", version, err)
	}

	// Make the file executable (on Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(targetPath, 0755); err != nil {
			return "", fmt.Errorf("failed to make compiler executable: %w", err)
		}
	}

	// Test the compiler
	if err := cm.testCompiler(targetPath); err != nil {
		return "", fmt.Errorf("downloaded compiler test failed: %w", err)
	}

	// Cache the path
	cm.rememberCompiler(version, targetPath)

	return targetPath, nil
}

// getDownloadURL returns the download URL for the specified Solidity version
func (cm *CompilerManager) getDownloadURL(version string) (string, error) {
	baseURL := fmt.Sprintf("https://github.com/ethereum/solidity/releases/download/v%s", version)

	var fileName string
	switch runtime.GOOS {
	case "windows":
		fileName = "solc-windows.exe"
	case "darwin":
		fileName = "solc-macos"
	case "linux":
		fileName = "solc-static-linux"
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return fmt.Sprintf("%s/%s", baseURL, fileName), nil
}

// downloadFile downloads a file from the given URL to the target path
func (cm *CompilerManager) downloadFile(url, targetPath string) error {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Make the request
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	// Create the target file
	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer file.Close()

	// Copy the response body to the file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// testCompiler tests if the downloaded compiler works
func (cm *CompilerManager) testCompiler(compilerPath string) error {
	// Run the compiler with --version to test it
	cmd := exec.Command(compilerPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compiler test failed: %s, output: %s", err.Error(), string(output))
	}

	return nil
}

// IsVersionSupported checks if a specific Solidity version is supported
// IsVersionSupported reports whether this version is ALREADY INSTALLED. It deliberately does
// not install anything.
//
// It used to call GetCompilerPath, which falls through to downloadAndInstallCompiler. Since
// GetAvailableVersions loops over every entry of solidityReleases, the unauthenticated
// GraphQL field `availableCompilerVersions` made the server attempt ~64 sequential solc
// downloads from github.com -- roughly a gigabyte -- each with the 5-minute client timeout
// below, on any instance whose compiler directory was cold. Measured: with the map race
// fixed so the process no longer died first, a single test doing this ran past 600 seconds
// entirely inside HTTP/2 requests to GitHub. Asking WHAT IS AVAILABLE must not be a request
// to go and fetch it.
func (cm *CompilerManager) IsVersionSupported(version string) bool {
	_, ok := cm.resolveLocal(strings.TrimPrefix(version, "v"))
	return ok
}

// GetAvailableVersions returns a list of available compiler versions
func (cm *CompilerManager) GetAvailableVersions() []string {
	var versions []string
	for _, release := range solidityReleases {
		if cm.IsVersionSupported(release) {
			versions = append(versions, release)
		}
	}
	return versions
}
