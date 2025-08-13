package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PackageInfo represents information about a package
type PackageInfo struct {
	Name         string   `json:"name"`
	Versions     []string `json:"versions"`
	CDN          string   `json:"cdn"`
	ImportPaths  []string `json:"importPaths"`
	LastUpdated  time.Time `json:"lastUpdated"`
	Source       string   `json:"source"`
}

// PackageRegistry manages package discovery and version information
type PackageRegistry struct {
	cache     map[string]*PackageInfo
	cacheTTL  time.Duration
	httpClient *http.Client
}

// NewPackageRegistry creates a new package registry instance
func NewPackageRegistry() *PackageRegistry {
	return &PackageRegistry{
		cache:     make(map[string]*PackageInfo),
		cacheTTL:  24 * time.Hour, // Cache for 24 hours
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DiscoverPackage automatically discovers package information from multiple sources
func (pr *PackageRegistry) DiscoverPackage(packageName string) (*PackageInfo, error) {
	// Check cache first
	if cached, exists := pr.cache[packageName]; exists && time.Since(cached.LastUpdated) < pr.cacheTTL {
		return cached, nil
	}

	// Try different discovery methods
	info, err := pr.discoverFromNPM(packageName)
	if err == nil {
		pr.cache[packageName] = info
		return info, nil
	}

	info, err = pr.discoverFromGitHub(packageName)
	if err == nil {
		pr.cache[packageName] = info
		return info, nil
	}

	info, err = pr.discoverFromCustomRegistry(packageName)
	if err == nil {
		pr.cache[packageName] = info
		return info, nil
	}

	// If all discovery methods fail, return a basic info structure
	basicInfo := &PackageInfo{
		Name:        packageName,
		Versions:    []string{},
		CDN:         "unpkg.com",
		ImportPaths: []string{packageName},
		LastUpdated: time.Now(),
		Source:      "fallback",
	}
	
	pr.cache[packageName] = basicInfo
	return basicInfo, fmt.Errorf("failed to discover package %s from any source", packageName)
}

// discoverFromNPM discovers package information from npm registry
func (pr *PackageRegistry) discoverFromNPM(packageName string) (*PackageInfo, error) {
	url := fmt.Sprintf("https://registry.npmjs.org/%s", packageName)
	
	resp, err := pr.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("npm registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm registry returned status %d", resp.StatusCode)
	}

	var npmData struct {
		Versions map[string]interface{} `json:"versions"`
		Time     map[string]string      `json:"time"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&npmData); err != nil {
		return nil, fmt.Errorf("failed to decode npm response: %w", err)
	}

	versions := make([]string, 0, len(npmData.Versions))
	for version := range npmData.Versions {
		// Filter out pre-release versions
		if !strings.Contains(version, "-") && !strings.Contains(version, "alpha") && !strings.Contains(version, "beta") && !strings.Contains(version, "rc") {
			versions = append(versions, version)
		}
	}

	// Sort versions (newest first)
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})

	// Limit to latest 50 versions to avoid overwhelming the system
	if len(versions) > 50 {
		versions = versions[:50]
	}

	return &PackageInfo{
		Name:        packageName,
		Versions:    versions,
		CDN:         "unpkg.com",
		ImportPaths: []string{packageName},
		LastUpdated: time.Now(),
		Source:      "npm",
	}, nil
}

// discoverFromGitHub discovers package information from GitHub releases
func (pr *PackageRegistry) discoverFromGitHub(packageName string) (*PackageInfo, error) {
	// Try common GitHub organization patterns
	orgs := []string{"openzeppelin", "chainlink", "uniswap", "aave", "compound-finance"}
	
	for _, org := range orgs {
		if strings.Contains(packageName, org) {
			repoName := strings.TrimPrefix(packageName, "@"+org+"/")
			url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", org, repoName)
			
			resp, err := pr.httpClient.Get(url)
			if err != nil {
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				continue
			}

			var releases []struct {
				TagName string `json:"tag_name"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
				continue
			}

			versions := make([]string, 0, len(releases))
			for _, release := range releases {
				version := strings.TrimPrefix(release.TagName, "v")
				if version != "" {
					versions = append(versions, version)
				}
			}

			if len(versions) > 0 {
				// Sort versions (newest first)
				sort.Slice(versions, func(i, j int) bool {
					return compareVersions(versions[i], versions[j]) > 0
				})

				// Limit to latest 50 versions
				if len(versions) > 50 {
					versions = versions[:50]
				}

				return &PackageInfo{
					Name:        packageName,
					Versions:    versions,
					CDN:         "unpkg.com",
					ImportPaths: []string{packageName},
					LastUpdated: time.Now(),
					Source:      "github",
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no GitHub releases found for package %s", packageName)
}

// discoverFromCustomRegistry discovers package information from custom registries
func (pr *PackageRegistry) discoverFromCustomRegistry(packageName string) (*PackageInfo, error) {
	// Custom registry endpoints for specific packages
	customRegistries := map[string]string{
		"@openzeppelin/contracts": "https://api.openzeppelin.org/versions",
		"@chainlink/contracts":    "https://api.chain.link/versions",
		"@uniswap/v3-core":       "https://api.uniswap.org/versions",
	}

	if endpoint, exists := customRegistries[packageName]; exists {
		resp, err := pr.httpClient.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("custom registry request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var versions []string
			if err := json.NewDecoder(resp.Body).Decode(&versions); err == nil {
				return &PackageInfo{
					Name:        packageName,
					Versions:    versions,
					CDN:         "unpkg.com",
					ImportPaths: []string{packageName},
					LastUpdated: time.Now(),
					Source:      "custom",
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no custom registry found for package %s", packageName)
}

// GetVersionsForPackage returns all available versions for a package
func (pr *PackageRegistry) GetVersionsForPackage(packageName string) ([]string, error) {
	info, err := pr.DiscoverPackage(packageName)
	if err != nil {
		return nil, err
	}
	return info.Versions, nil
}

// GetLatestVersion returns the latest version for a package
func (pr *PackageRegistry) GetLatestVersion(packageName string) (string, error) {
	info, err := pr.DiscoverPackage(packageName)
	if err != nil {
		return "", err
	}
	if len(info.Versions) == 0 {
		return "", fmt.Errorf("no versions available for package %s", packageName)
	}
	return info.Versions[0], nil
}

// RefreshCache forces a refresh of the package cache
func (pr *PackageRegistry) RefreshCache() {
	pr.cache = make(map[string]*PackageInfo)
}

// compareVersions compares two semantic versions
func compareVersions(v1, v2 string) int {
	// Simple version comparison - can be enhanced with proper semver parsing
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")
	
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	
	for i := 0; i < maxLen; i++ {
		var num1, num2 int
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &num1)
		}
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &num2)
		}
		
		if num1 > num2 {
			return 1
		}
		if num1 < num2 {
			return -1
		}
	}
	
	return 0
}
