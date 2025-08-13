package registry

import (
	"testing"
	"time"
)

func TestNewPackageRegistry(t *testing.T) {
	registry := NewPackageRegistry()
	
	if registry == nil {
		t.Fatal("NewPackageRegistry returned nil")
	}
	
	if registry.cache == nil {
		t.Error("cache was not initialized")
	}
	
	if registry.httpClient == nil {
		t.Error("httpClient was not initialized")
	}
	
	if registry.cacheTTL != 24*time.Hour {
		t.Errorf("expected cacheTTL to be 24 hours, got %v", registry.cacheTTL)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.0.0", "1.0", 0},
		{"1.0", "1.0.0", 0},
	}
	
	for _, test := range tests {
		result := compareVersions(test.v1, test.v2)
		if result != test.expected {
			t.Errorf("compareVersions(%s, %s) = %d, expected %d", 
				test.v1, test.v2, result, test.expected)
		}
	}
}

func TestPackageRegistryCache(t *testing.T) {
	registry := NewPackageRegistry()
	
	// Test cache operations
	packageName := "test-package"
	
	// Initially cache should be empty
	if _, exists := registry.cache[packageName]; exists {
		t.Error("cache should be empty initially")
	}
	
	// Add to cache
	registry.cache[packageName] = &PackageInfo{
		Name:     packageName,
		Versions: []string{"1.0.0", "1.0.1"},
		CDN:      "unpkg.com",
		LastUpdated: time.Now(),
	}
	
	// Check if in cache
	if _, exists := registry.cache[packageName]; !exists {
		t.Error("package should be in cache after adding")
	}
	
	// Test cache refresh
	registry.RefreshCache()
	if len(registry.cache) != 0 {
		t.Error("cache should be empty after refresh")
	}
}

func TestPackageRegistryDiscoveryMethods(t *testing.T) {
	registry := NewPackageRegistry()
	
	// Test with a known package (this will make actual HTTP requests)
	// Note: These tests may fail if network is unavailable
	packageName := "@openzeppelin/contracts"
	
	// Test NPM discovery (may fail if network unavailable)
	info, err := registry.discoverFromNPM(packageName)
	if err == nil {
		// Success case
		if info.Name != packageName {
			t.Errorf("expected package name %s, got %s", packageName, info.Name)
		}
		if len(info.Versions) == 0 {
			t.Error("expected versions to be populated")
		}
		if info.Source != "npm" {
			t.Errorf("expected source to be 'npm', got %s", info.Source)
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("NPM discovery failed (network may be unavailable): %v", err)
	}
	
	// Test GitHub discovery (may fail if network unavailable)
	info, err = registry.discoverFromGitHub(packageName)
	if err == nil {
		// Success case
		if info.Name != packageName {
			t.Errorf("expected package name %s, got %s", packageName, info.Name)
		}
		if info.Source != "github" {
			t.Errorf("expected source to be 'github', got %s", info.Source)
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("GitHub discovery failed (network may be unavailable): %v", err)
	}
}

func TestPackageRegistryFallback(t *testing.T) {
	registry := NewPackageRegistry()
	
	// Test with a non-existent package
	packageName := "non-existent-package-12345"
	
	info, err := registry.DiscoverPackage(packageName)
	
	// Should return fallback info even on error
	if info == nil {
		t.Fatal("expected fallback info to be returned")
	}
	
	if info.Name != packageName {
		t.Errorf("expected package name %s, got %s", packageName, info.Name)
	}
	
	if info.CDN != "unpkg.com" {
		t.Errorf("expected fallback CDN 'unpkg.com', got %s", info.CDN)
	}
	
	if info.Source != "fallback" {
		t.Errorf("expected fallback source 'fallback', got %s", info.Source)
	}
	
	// Should have an error
	if err == nil {
		t.Error("expected error for non-existent package")
	}
}

func TestPackageRegistryVersionMethods(t *testing.T) {
	registry := NewPackageRegistry()
	
	// Test with a package that should exist
	packageName := "@openzeppelin/contracts"
	
	// Test GetVersionsForPackage
	versions, err := registry.GetVersionsForPackage(packageName)
	if err == nil {
		// Success case
		if len(versions) == 0 {
			t.Error("expected versions to be populated")
		}
		
		// Test GetLatestVersion
		latest, err := registry.GetLatestVersion(packageName)
		if err != nil {
			t.Errorf("failed to get latest version: %v", err)
		}
		if latest == "" {
			t.Error("expected latest version to be non-empty")
		}
		
		// Latest version should be first in versions list
		if versions[0] != latest {
			t.Errorf("expected first version to be latest, got %s vs %s", versions[0], latest)
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("Version methods failed (network may be unavailable): %v", err)
	}
}

func TestPackageInfoStructure(t *testing.T) {
	info := &PackageInfo{
		Name:        "test-package",
		Versions:    []string{"1.0.0", "1.0.1", "2.0.0"},
		CDN:         "unpkg.com",
		ImportPaths: []string{"test-package", "test-package/contracts"},
		LastUpdated: time.Now(),
		Source:      "test",
	}
	
	if info.Name != "test-package" {
		t.Errorf("expected name 'test-package', got %s", info.Name)
	}
	
	if len(info.Versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(info.Versions))
	}
	
	if info.CDN != "unpkg.com" {
		t.Errorf("expected CDN 'unpkg.com', got %s", info.CDN)
	}
	
	if len(info.ImportPaths) != 2 {
		t.Errorf("expected 2 import paths, got %d", len(info.ImportPaths))
	}
	
	if info.Source != "test" {
		t.Errorf("expected source 'test', got %s", info.Source)
	}
}
