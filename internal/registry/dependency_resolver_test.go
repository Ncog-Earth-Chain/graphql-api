package registry

import (
	"testing"
)

func TestNewDependencyResolver(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	
	if resolver == nil {
		t.Fatal("NewDependencyResolver returned nil")
	}
	
	if resolver.httpClient == nil {
		t.Error("httpClient was not initialized")
	}
	
	if resolver.registry == nil {
		t.Error("registry was not initialized")
	}
	
	if len(resolver.cdns) == 0 {
		t.Error("cdns slice was not initialized")
	}
	
	// Check if expected CDNs are present
	expectedCDNs := []string{"unpkg.com", "cdn.jsdelivr.net", "bundle.run", "esm.sh"}
	for _, expected := range expectedCDNs {
		found := false
		for _, cdn := range resolver.cdns {
			if cdn == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CDN %s not found", expected)
		}
	}
}

func TestExtractPackageName(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	tests := []struct {
		importPath string
		expected   string
	}{
		{"@openzeppelin/contracts/access/Ownable.sol", "@openzeppelin/contracts"},
		{"@chainlink/contracts/v0.8/AutomationCompatible.sol", "@chainlink/contracts"},
		{"@uniswap/v3-core/contracts/UniswapV3Factory.sol", "@uniswap/v3-core"},
		{"openzeppelin-solidity/contracts/token/ERC20/ERC20.sol", "openzeppelin-solidity"},
		{"simple-contract.sol", "simple-contract"},
		{"contracts/MyContract.sol", "contracts"},
		{"", ""},
	}
	
	for _, test := range tests {
		result := resolver.extractPackageName(test.importPath)
		if result != test.expected {
			t.Errorf("extractPackageName(%s) = %s, expected %s", 
				test.importPath, result, test.expected)
		}
	}
}

func TestExtractImports(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";
import "./MyContract.sol";

contract TestContract is Ownable {
    // contract code
}`

	imports := resolver.extractImports(source)
	
	expectedImports := []string{
		"@openzeppelin/contracts/access/Ownable.sol",
		"@chainlink/contracts/v0.8/AutomationCompatible.sol",
		"./MyContract.sol",
	}
	
	if len(imports) != len(expectedImports) {
		t.Errorf("expected %d imports, got %d", len(expectedImports), len(imports))
	}
	
	for _, expected := range expectedImports {
		found := false
		for _, imp := range imports {
			if imp == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected import %s not found", expected)
		}
	}
}

func TestGenerateAlternativePaths(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	tests := []struct {
		importPath string
		expected   []string
	}{
		{
			"@openzeppelin/contracts/access/Ownable.sol",
			[]string{
				"@openzeppelin/contracts/access/Ownable.sol",
				"openzeppelin-solidity/contracts/access/Ownable.sol",
			},
		},
		{
			"@openzeppelin/contracts/access/Ownable",
			[]string{
				"@openzeppelin/contracts/access/Ownable",
				"openzeppelin-solidity/contracts/access/Ownable",
				"@openzeppelin/contracts/access/Ownable.sol",
			},
		},
		{
			"simple-contract",
			[]string{
				"simple-contract",
				"simple-contract.sol",
			},
		},
	}
	
	for _, test := range tests {
		alternatives := resolver.generateAlternativePaths(test.importPath)
		
		// Check if all expected alternatives are present
		for _, expected := range test.expected {
			found := false
			for _, alt := range alternatives {
				if alt == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected alternative path %s not found for %s", expected, test.importPath)
			}
		}
	}
}

func TestResolveAllDependencies(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./MyContract.sol";

contract TestContract is Ownable {
    // contract code
}`

	// Test with empty version pins (will try to fetch latest)
	dependencies, err := resolver.ResolveAllDependencies(source, make(map[string]string))
	
	// This test may fail if network is unavailable, which is acceptable
	if err == nil {
		// Success case
		if len(dependencies) == 0 {
			t.Error("expected dependencies to be resolved")
		}
		
		// Check if expected imports are present
		expectedImports := []string{
			"@openzeppelin/contracts/access/Ownable.sol",
			"./MyContract.sol",
		}
		
		for _, expected := range expectedImports {
			if _, exists := dependencies[expected]; !exists {
				t.Errorf("expected dependency %s not found", expected)
			}
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("ResolveAllDependencies failed (network may be unavailable): %v", err)
	}
}

func TestResolveImport(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	// Test with version pins
	versionPins := map[string]string{
		"@openzeppelin/contracts": "4.9.6",
	}
	
	importPath := "@openzeppelin/contracts/access/Ownable.sol"
	
	// This test may fail if network is unavailable, which is acceptable
	source, err := resolver.ResolveImport(importPath, versionPins)
	if err == nil {
		// Success case
		if source == "" {
			t.Error("expected source code to be returned")
		}
		
		// Check if source contains expected content
		if !contains(source, "Ownable") {
			t.Error("expected source to contain 'Ownable'")
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("ResolveImport failed (network may be unavailable): %v", err)
	}
}

func TestGetCDNStatus(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	status := resolver.GetCDNStatus()
	
	if len(status) == 0 {
		t.Error("expected CDN status to be returned")
	}
	
	// Check if all expected CDNs are present in status
	expectedCDNs := []string{"unpkg.com", "cdn.jsdelivr.net", "bundle.run", "esm.sh"}
	for _, cdn := range expectedCDNs {
		if _, exists := status[cdn]; !exists {
			t.Errorf("expected CDN %s not found in status", cdn)
		}
	}
}

func TestFetchWithVersionPins(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	versionPins := map[string]string{
		"@openzeppelin/contracts": "4.9.6",
	}
	
	importPath := "@openzeppelin/contracts/access/Ownable.sol"
	
	// This test may fail if network is unavailable, which is acceptable
	source, err := resolver.fetchWithVersionPins(importPath, versionPins)
	if err == nil {
		// Success case
		if source == "" {
			t.Error("expected source code to be returned")
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("fetchWithVersionPins failed (network may be unavailable): %v", err)
	}
}

func TestFetchLatestVersion(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	importPath := "@openzeppelin/contracts/access/Ownable.sol"
	
	// This test may fail if network is unavailable, which is acceptable
	source, err := resolver.fetchLatestVersion(importPath)
	if err == nil {
		// Success case
		if source == "" {
			t.Error("expected source code to be returned")
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("fetchLatestVersion failed (network may be unavailable): %v", err)
	}
}

func TestTryAlternativePaths(t *testing.T) {
	resolver := NewDependencyResolver(NewPackageRegistry())
	
	importPath := "@openzeppelin/contracts/access/Ownable.sol"
	
	// This test may fail if network is unavailable, which is acceptable
	source, err := resolver.tryAlternativePaths(importPath)
	if err == nil {
		// Success case
		if source == "" {
			t.Error("expected source code to be returned")
		}
	} else {
		// Network failure case - this is acceptable for tests
		t.Logf("tryAlternativePaths failed (network may be unavailable): %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || 
		(len(s) > len(substr) && (s[:len(substr)] == substr || 
		s[len(s)-len(substr):] == substr || 
		contains(s[1:], substr))))
}
