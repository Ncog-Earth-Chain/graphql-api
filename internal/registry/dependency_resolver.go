package registry

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DependencyResolver handles fetching and resolving package dependencies
type DependencyResolver struct {
	httpClient *http.Client
	cdns       []string
	registry   *PackageRegistry
}

// NewDependencyResolver creates a new dependency resolver instance
func NewDependencyResolver(registry *PackageRegistry) *DependencyResolver {
	return &DependencyResolver{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cdns: []string{
			"unpkg.com",
			"cdn.jsdelivr.net",
			"bundle.run",
			"esm.sh",
		},
		registry: registry,
	}
}

// ResolveImport resolves an import statement to source code
func (dr *DependencyResolver) ResolveImport(importPath string, versionPins map[string]string) (string, error) {
	// Try to resolve with version pins first
	if len(versionPins) > 0 {
		source, err := dr.fetchWithVersionPins(importPath, versionPins)
		if err == nil {
			return source, nil
		}
	}

	// Try to resolve without version pins (latest version)
	source, err := dr.fetchLatestVersion(importPath)
	if err == nil {
		return source, nil
	}

	// Try alternative import paths
	source, err = dr.tryAlternativePaths(importPath)
	if err == nil {
		return source, nil
	}

	return "", fmt.Errorf("failed to resolve import %s from any source", importPath)
}

// fetchWithVersionPins attempts to fetch with specific version pins
func (dr *DependencyResolver) fetchWithVersionPins(importPath string, versionPins map[string]string) (string, error) {
	// Try each CDN with version pins
	for _, cdn := range dr.cdns {
		for packageName, version := range versionPins {
			if strings.Contains(importPath, packageName) {
				// Replace package name with versioned package name
				versionedPath := strings.Replace(importPath, packageName, packageName+"@"+version, 1)
				url := fmt.Sprintf("https://%s/%s", cdn, versionedPath)
				
				source, err := dr.httpGetText(url)
				if err == nil {
					return source, nil
				}
			}
		}
	}
	return "", fmt.Errorf("failed to fetch with version pins")
}

// fetchLatestVersion attempts to fetch the latest version
func (dr *DependencyResolver) fetchLatestVersion(importPath string) (string, error) {
	// Extract package name from import path
	packageName := dr.extractPackageName(importPath)
	if packageName == "" {
		return "", fmt.Errorf("could not extract package name from import path")
	}

	// Get latest version from registry
	latestVersion, err := dr.registry.GetLatestVersion(packageName)
	if err != nil {
		return "", fmt.Errorf("failed to get latest version for %s: %w", packageName, err)
	}

	// Try to fetch with latest version
	versionedPath := strings.Replace(importPath, packageName, packageName+"@"+latestVersion, 1)
	
	for _, cdn := range dr.cdns {
		url := fmt.Sprintf("https://%s/%s", cdn, versionedPath)
		source, err := dr.httpGetText(url)
		if err == nil {
			return source, nil
		}
	}

	return "", fmt.Errorf("failed to fetch latest version from any CDN")
}

// tryAlternativePaths tries alternative import path formats
func (dr *DependencyResolver) tryAlternativePaths(importPath string) (string, error) {
	alternatives := dr.generateAlternativePaths(importPath)
	
	for _, cdn := range dr.cdns {
		for _, altPath := range alternatives {
			url := fmt.Sprintf("https://%s/%s", cdn, altPath)
			source, err := dr.httpGetText(url)
			if err == nil {
				return source, nil
			}
		}
	}
	
	return "", fmt.Errorf("failed to resolve with alternative paths")
}

// generateAlternativePaths generates alternative import path formats
func (dr *DependencyResolver) generateAlternativePaths(importPath string) []string {
	alternatives := []string{importPath}
	
	// Try without @openzeppelin prefix
	if strings.HasPrefix(importPath, "@openzeppelin/") {
		alternatives = append(alternatives, strings.TrimPrefix(importPath, "@openzeppelin/"))
	}
	
	// Try with openzeppelin-solidity prefix (legacy)
	if strings.HasPrefix(importPath, "@openzeppelin/contracts/") {
		legacyPath := strings.Replace(importPath, "@openzeppelin/contracts/", "openzeppelin-solidity/", 1)
		alternatives = append(alternatives, legacyPath)
	}
	
	// Try with .sol extension if missing
	if !strings.HasSuffix(importPath, ".sol") {
		alternatives = append(alternatives, importPath+".sol")
	}
	
	// Try without .sol extension if present
	if strings.HasSuffix(importPath, ".sol") {
		alternatives = append(alternatives, strings.TrimSuffix(importPath, ".sol"))
	}
	
	return alternatives
}

// extractPackageName extracts the package name from an import path
func (dr *DependencyResolver) extractPackageName(importPath string) string {
	// Handle scoped packages (@org/package)
	if strings.HasPrefix(importPath, "@") {
		parts := strings.Split(importPath, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	
	// Handle regular packages
	parts := strings.Split(importPath, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	
	return ""
}

// httpGetText performs an HTTP GET request and returns the response as text
func (dr *DependencyResolver) httpGetText(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers for better compatibility
	req.Header.Set("User-Agent", "GraphQL-API-Contract-Verifier/1.0")
	req.Header.Set("Accept", "text/plain,application/octet-stream,*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	// Try up to 3 times with exponential backoff
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			time.Sleep(backoff)
		}

		resp, err := dr.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		defer resp.Body.Close()

		// Don't retry on 404 errors
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("resource not found: %s", url)
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
			continue
		}

		// Read and decode response
		var reader io.Reader = resp.Body
		
		// Handle gzip compression
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzReader, err := gzip.NewReader(resp.Body)
			if err != nil {
				lastErr = fmt.Errorf("failed to create gzip reader: %w", err)
				continue
			}
			defer gzReader.Close()
			reader = gzReader
		}

		body, err := io.ReadAll(reader)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		return string(body), nil
	}

	return "", fmt.Errorf("all attempts failed, last error: %w", lastErr)
}

// ResolveAllDependencies resolves all dependencies for a contract
func (dr *DependencyResolver) ResolveAllDependencies(source string, versionPins map[string]string) (map[string]string, error) {
	dependencies := make(map[string]string)
	imports := dr.extractImports(source)
	
	for _, importPath := range imports {
		source, err := dr.ResolveImport(importPath, versionPins)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve import %s: %w", importPath, err)
		}
		dependencies[importPath] = source
		
		// Recursively resolve nested dependencies
		nestedDeps, err := dr.ResolveAllDependencies(source, versionPins)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve nested dependencies for %s: %w", importPath, err)
		}
		
		// Merge nested dependencies
		for nestedPath, nestedSource := range nestedDeps {
			if _, exists := dependencies[nestedPath]; !exists {
				dependencies[nestedPath] = nestedSource
			}
		}
	}
	
	return dependencies, nil
}

// extractImports extracts all import statements from Solidity source code
func (dr *DependencyResolver) extractImports(source string) []string {
	var imports []string
	
	// Regex to match import statements
	importRegex := regexp.MustCompile(`import\s+["']([^"']+)["']\s*;?`)
	matches := importRegex.FindAllStringSubmatch(source, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			imports = append(imports, match[1])
		}
	}
	
	return imports
}

// GetCDNStatus checks the status of available CDNs
func (dr *DependencyResolver) GetCDNStatus() map[string]bool {
	status := make(map[string]bool)
	
	for _, cdn := range dr.cdns {
		url := fmt.Sprintf("https://%s", cdn)
		resp, err := dr.httpClient.Get(url)
		if err != nil {
			status[cdn] = false
			continue
		}
		resp.Body.Close()
		status[cdn] = resp.StatusCode == http.StatusOK
	}
	
	return status
}
