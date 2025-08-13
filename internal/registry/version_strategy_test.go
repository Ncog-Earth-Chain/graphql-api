package registry

import (
	"testing"
)

func TestNewVersionStrategy(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	if strategy == nil {
		t.Fatal("NewVersionStrategy returned nil")
	}
	
	if strategy.registry == nil {
		t.Error("registry was not initialized")
	}
}

func TestDetectPackagesFromSource(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// This contract uses OpenZeppelin v5.0.1
import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";
import "@uniswap/v3-core/contracts/UniswapV3Factory.sol";

contract TestContract is Ownable {
    // contract code
}`

	packages := strategy.DetectPackagesFromSource(source)
	
	expectedPackages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/v3-core",
	}
	
	if len(packages) != len(expectedPackages) {
		t.Errorf("expected %d packages, got %d", len(expectedPackages), len(packages))
	}
	
	for _, expected := range expectedPackages {
		found := false
		for _, pkg := range packages {
			if pkg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected package %s not found", expected)
		}
	}
}

func TestVersionStrategyExtractImports(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";
import "./MyContract.sol";

contract TestContract is Ownable {
    // contract code
}`

	imports := strategy.extractImports(source)
	
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

func TestExtractPackagesFromComments(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// This contract uses OpenZeppelin v5.0.1
// Also uses Chainlink for oracle data
// And Uniswap for DEX functionality

contract TestContract {
    // contract code
}`

	packages := strategy.extractPackagesFromComments(source)
	
	expectedPackages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/",
	}
	
	for _, expected := range expectedPackages {
		found := false
		for _, pkg := range packages {
			if pkg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected package %s not found in comments", expected)
		}
	}
}

func TestVersionStrategyExtractPackageName(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
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
		result := strategy.extractPackageName(test.importPath)
		if result != test.expected {
			t.Errorf("extractPackageName(%s) = %s, expected %s", 
				test.importPath, result, test.expected)
		}
	}
}

func TestRemoveDuplicates(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	packages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@openzeppelin/contracts", // duplicate
		"@uniswap/v3-core",
		"@chainlink/contracts",    // duplicate
	}
	
	result := strategy.removeDuplicates(packages)
	
	expectedCount := 3
	if len(result) != expectedCount {
		t.Errorf("expected %d unique packages, got %d", expectedCount, len(result))
	}
	
	// Check if all expected packages are present
	expectedPackages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/v3-core",
	}
	
	for _, expected := range expectedPackages {
		found := false
		for _, pkg := range result {
			if pkg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected package %s not found in deduplicated result", expected)
		}
	}
}

func TestIsStableVersion(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	tests := []struct {
		version  string
		expected bool
	}{
		{"1.0.0", true},
		{"2.1.5", true},
		{"3.0.0", true},
		{"1.0.0-alpha", false},
		{"2.0.0-beta", false},
		{"3.0.0-rc1", false},
		{"1.0.0-dev", false},
		{"2.0.0-preview", false},
	}
	
	for _, test := range tests {
		result := strategy.isStableVersion(test.version)
		if result != test.expected {
			t.Errorf("isStableVersion(%s) = %t, expected %t", 
				test.version, result, test.expected)
		}
	}
}

func TestGetStableVersion(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	// Test with known packages
	tests := []struct {
		packageName string
		expected    string
	}{
		{"@openzeppelin/contracts", "4.9.6"},
		{"@chainlink/contracts", "0.0.16"},
		{"@uniswap/v3-core", "1.0.1"},
	}
	
	for _, test := range tests {
		result := strategy.getStableVersion(test.packageName)
		if result != test.expected {
			t.Errorf("getStableVersion(%s) = %s, expected %s", 
				test.packageName, result, test.expected)
		}
	}
}

func TestGetMajorVersion(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	tests := []struct {
		version  string
		expected string
	}{
		{"1.0.0", "1"},
		{"2.1.5", "2"},
		{"3.0.0", "3"},
		{"10.5.2", "10"},
		{"0.1.0", "0"},
		{"invalid", "invalid"},
	}
	
	for _, test := range tests {
		result := strategy.getMajorVersion(test.version)
		if result != test.expected {
			t.Errorf("getMajorVersion(%s) = %s, expected %s", 
				test.version, result, test.expected)
		}
	}
}

func TestGenerateVersionAttempts(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	packages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
	}
	
	attempts := strategy.GenerateVersionAttempts(packages)
	
	if len(attempts) == 0 {
		t.Error("expected version attempts to be generated")
	}
	
	// Check if attempts contain the expected packages
	for _, attempt := range attempts {
		if len(attempt) == 0 {
			t.Error("expected attempt to contain version pins")
		}
		
		// Check if attempt contains at least one of the packages
		found := false
		for _, pkg := range packages {
			if _, exists := attempt[pkg]; exists {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected attempt to contain at least one package")
		}
	}
}

func TestVersionStrategyGetCompatibilityMatrix(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	packages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
	}
	
	matrix := strategy.GetCompatibilityMatrix(packages)
	
	if len(matrix) == 0 {
		t.Error("expected compatibility matrix to be generated")
	}
	
	// Check if matrix contains the expected packages
	for _, pkg := range packages {
		if _, exists := matrix[pkg]; !exists {
			t.Errorf("expected package %s to be in compatibility matrix", pkg)
		}
	}
}

func TestVersionStrategySuggestOptimalVersions(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	packages := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/v3-core",
	}
	
	suggestions := strategy.SuggestOptimalVersions(packages)
	
	if len(suggestions) == 0 {
		t.Error("expected optimal version suggestions to be generated")
	}
	
	// Check if suggestions contain the expected packages
	for _, pkg := range packages {
		if version, exists := suggestions[pkg]; !exists {
			t.Errorf("expected package %s to be in suggestions", pkg)
		} else if version == "" {
			t.Errorf("expected package %s to have a suggested version", pkg)
		}
	}
}

func TestSortByPriority(t *testing.T) {
	registry := NewPackageRegistry()
	strategy := NewVersionStrategy(registry)
	
	packages := []string{
		"@uniswap/v3-core",
		"@openzeppelin/contracts",
		"@chainlink/contracts",
	}
	
	priorityOrder := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/v3-core",
	}
	
	sorted := strategy.sortByPriority(packages, priorityOrder)
	
	if len(sorted) != len(packages) {
		t.Errorf("expected %d packages in sorted result, got %d", len(packages), len(sorted))
	}
	
	// Check if packages are sorted by priority
	// @openzeppelin/contracts should be first (highest priority)
	if sorted[0] != "@openzeppelin/contracts" {
		t.Errorf("expected first package to be @openzeppelin/contracts, got %s", sorted[0])
	}
	
	// @chainlink/contracts should be second
	if sorted[1] != "@chainlink/contracts" {
		t.Errorf("expected second package to be @chainlink/contracts, got %s", sorted[1])
	}
	
	// @uniswap/v3-core should be third
	if sorted[2] != "@uniswap/v3-core" {
		t.Errorf("expected third package to be @uniswap/v3-core, got %s", sorted[2])
	}
}
