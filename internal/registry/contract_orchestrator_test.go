package registry

import (
	"testing"
	"time"
)

// MockCompiler implementation for testing
type testMockCompiler struct {
	compilerVersion string
}

func (mc *testMockCompiler) CompileContract(source string, dependencies map[string]string, compilerVersion string) (string, error) {
	// Simple mock that returns a deterministic bytecode
	return "0x1234567890abcdef", nil
}

func (mc *testMockCompiler) GetCompilerVersion() string {
	return mc.compilerVersion
}

func TestNewContractOrchestrator(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	if orchestrator == nil {
		t.Fatal("NewContractOrchestrator returned nil")
	}
	
	if orchestrator.registry == nil {
		t.Error("registry was not initialized")
	}
	
	if orchestrator.dependencyResolver == nil {
		t.Error("dependencyResolver was not initialized")
	}
	
	if orchestrator.versionStrategy == nil {
		t.Error("versionStrategy was not initialized")
	}
	
	if orchestrator.compiler == nil {
		t.Error("compiler was not initialized")
	}
}

func TestVerifyContract(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    uint256 public value;
    
    constructor() Ownable(msg.sender) {}
    
    function setValue(uint256 _value) public onlyOwner {
        value = _value;
    }
}`

	targetBytecode := "0x1234567890abcdef"
	compilerVersion := "0.8.19"
	
	result := orchestrator.VerifyContract(source, targetBytecode, compilerVersion)
	
	if result == nil {
		t.Fatal("expected verification result to be returned")
	}
	
	// Check basic result structure
	if result.Compiler != compilerVersion {
		t.Errorf("expected compiler version %s, got %s", compilerVersion, result.Compiler)
	}
	
	if result.Bytecode != targetBytecode {
		t.Errorf("expected target bytecode %s, got %s", targetBytecode, result.Bytecode)
	}
	
	if result.Duration == 0 {
		t.Error("expected duration to be set")
	}
	
	// Check metadata
	if result.Metadata == nil {
		t.Error("expected metadata to be set")
	}
	
	if result.Metadata["detectedPackages"] == nil {
		t.Error("expected detectedPackages in metadata")
	}
	
	if result.Metadata["totalPackages"] == nil {
		t.Error("expected totalPackages in metadata")
	}
	
	if result.Metadata["totalVersionAttempts"] == nil {
		t.Error("expected totalVersionAttempts in metadata")
	}
}

func TestCompareBytecodes(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	// Test exact match
	compiled := "0x1234567890abcdef"
	target := "0x1234567890abcdef"
	
	if !orchestrator.compareBytecodes(compiled, target) {
		t.Error("expected exact bytecode match to return true")
	}
	
	// Test with 0x prefix
	compiled = "0x1234567890abcdef"
	target = "1234567890abcdef"
	
	if !orchestrator.compareBytecodes(compiled, target) {
		t.Error("expected bytecode match without 0x prefix to return true")
	}
	
	// Test with whitespace
	compiled = "0x1234567890abcdef"
	target = "0x 1234567890abcdef"
	
	if !orchestrator.compareBytecodes(compiled, target) {
		t.Error("expected bytecode match with whitespace to return true")
	}
	
	// Test mismatch
	compiled = "0x1234567890abcdef"
	target = "0x1234567890fedcba"
	
	if orchestrator.compareBytecodes(compiled, target) {
		t.Error("expected bytecode mismatch to return false")
	}
}

func TestCleanBytecode(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	tests := []struct {
		input    string
		expected string
	}{
		{"0x1234567890abcdef", "1234567890abcdef"},
		{"0x 1234567890abcdef", "1234567890abcdef"},
		{"0x\n1234567890abcdef", "1234567890abcdef"},
		{"0x\r1234567890abcdef", "1234567890abcdef"},
		{"0x\t1234567890abcdef", "1234567890abcdef"},
		{"1234567890ABCDEF", "1234567890abcdef"},
		{"1234567890abcdef", "1234567890abcdef"},
	}
	
	for _, test := range tests {
		result := orchestrator.cleanBytecode(test.input)
		if result != test.expected {
			t.Errorf("cleanBytecode(%s) = %s, expected %s", 
				test.input, result, test.expected)
		}
	}
}

func TestRemoveMetadata(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	tests := []struct {
		input    string
		expected string
	}{
		{"1234567890abcdef", "1234567890abcdef"}, // Less than 64 chars
		{"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"}, // Exactly 64 chars
		{"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"}, // More than 64 chars
	}
	
	for _, test := range tests {
		result := orchestrator.removeMetadata(test.input)
		if result != test.expected {
			t.Errorf("removeMetadata(%s) = %s, expected %s", 
				test.input, result, test.expected)
		}
	}
}

func TestMaskLinkReferences(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	// Test that the function returns the input as-is (current implementation)
	input := "1234567890abcdef"
	result := orchestrator.maskLinkReferences(input)
	
	if result != input {
		t.Errorf("maskLinkReferences(%s) = %s, expected %s", input, result, input)
	}
}

func TestValidateSourceCode(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	// Test valid source code
	validSource := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	issues := orchestrator.ValidateSourceCode(validSource)
	if len(issues) > 0 {
		t.Errorf("expected no validation issues for valid source, got: %v", issues)
	}
	
	// Test source code missing pragma
	invalidSource1 := `// SPDX-License-Identifier: MIT

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	issues = orchestrator.ValidateSourceCode(invalidSource1)
	if len(issues) == 0 {
		t.Error("expected validation issues for source missing pragma")
	}
	
	// Test source code missing contract definition
	invalidSource2 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

// No contract definition
function test() {
    // function code
}`

	issues = orchestrator.ValidateSourceCode(invalidSource2)
	if len(issues) == 0 {
		t.Error("expected validation issues for source missing contract definition")
	}
}

func TestGetPackageInfo(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	packages := []string{"@openzeppelin/contracts", "@chainlink/contracts"}
	
	packageInfo := orchestrator.GetPackageInfo(packages)
	
	if len(packageInfo) == 0 {
		t.Error("expected package info to be returned")
	}
	
	// Check if all packages have info (may be nil if network unavailable)
	for _, pkg := range packages {
		if _, exists := packageInfo[pkg]; !exists {
			t.Errorf("expected package %s to be in package info", pkg)
		}
	}
}

func TestGetCompatibilityMatrix(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	packages := []string{"@openzeppelin/contracts", "@chainlink/contracts"}
	
	matrix := orchestrator.GetCompatibilityMatrix(packages)
	
	if len(matrix) == 0 {
		t.Error("expected compatibility matrix to be returned")
	}
	
	// Check if all packages are in the matrix
	for _, pkg := range packages {
		if _, exists := matrix[pkg]; !exists {
			t.Errorf("expected package %s to be in compatibility matrix", pkg)
		}
	}
}

func TestSuggestOptimalVersions(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	packages := []string{"@openzeppelin/contracts", "@chainlink/contracts", "@uniswap/v3-core"}
	
	suggestions := orchestrator.SuggestOptimalVersions(packages)
	
	if len(suggestions) == 0 {
		t.Error("expected optimal version suggestions to be returned")
	}
	
	// Check if all packages have suggestions
	for _, pkg := range packages {
		if version, exists := suggestions[pkg]; !exists {
			t.Errorf("expected package %s to be in suggestions", pkg)
		} else if version == "" {
			t.Errorf("expected package %s to have a suggested version", pkg)
		}
	}
}

func TestRefreshPackageCache(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	// Add some data to cache
	registry.cache["test-package"] = &PackageInfo{
		Name:        "test-package",
		Versions:    []string{"1.0.0"},
		LastUpdated: time.Now(),
	}
	
	// Verify cache has data
	if len(registry.cache) == 0 {
		t.Error("expected cache to have data before refresh")
	}
	
	// Refresh cache
	orchestrator.RefreshPackageCache()
	
	// Verify cache is empty
	if len(registry.cache) != 0 {
		t.Error("expected cache to be empty after refresh")
	}
}

func TestGetCDNStatus(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	status := orchestrator.GetCDNStatus()
	
	if len(status) == 0 {
		t.Error("expected CDN status to be returned")
	}
	
	// Check if expected CDNs are present
	expectedCDNs := []string{"unpkg.com", "cdn.jsdelivr.net", "bundle.run", "esm.sh"}
	for _, cdn := range expectedCDNs {
		if _, exists := status[cdn]; !exists {
			t.Errorf("expected CDN %s not found in status", cdn)
		}
	}
}

func TestGetVerificationSummary(t *testing.T) {
	registry := NewPackageRegistry()
	resolver := NewDependencyResolver(registry)
	strategy := NewVersionStrategy(registry)
	compiler := &testMockCompiler{compilerVersion: "0.8.19"}
	
	orchestrator := NewContractOrchestrator(registry, resolver, strategy, compiler)
	
	// Create a test result
	result := &VerificationResult{
		Success:      true,
		VersionPins:  map[string]string{"@openzeppelin/contracts": "4.9.6"},
		Compiler:     "0.8.19",
		Bytecode:     "0x1234567890abcdef",
		Attempts:     1,
		Duration:     time.Second,
		Dependencies: map[string]string{"@openzeppelin/contracts/access/Ownable.sol": "source code"},
		Metadata:     map[string]interface{}{"successfulAttempt": 1},
	}
	
	summary := orchestrator.GetVerificationSummary(result)
	
	if summary == nil {
		t.Fatal("expected verification summary to be returned")
	}
	
	// Check basic summary fields
	if summary["success"] != true {
		t.Error("expected success to be true in summary")
	}
	
	if summary["totalAttempts"] != 1 {
		t.Error("expected totalAttempts to be 1 in summary")
	}
	
	if summary["compilerVersion"] != "0.8.19" {
		t.Error("expected compilerVersion to be 0.8.19 in summary")
	}
	
	if summary["dependenciesCount"] != 1 {
		t.Error("expected dependenciesCount to be 1 in summary")
	}
	
	// Check success-specific fields
	if summary["successfulVersions"] == nil {
		t.Error("expected successfulVersions in summary")
	}
	
	if summary["successfulAttempt"] == nil {
		t.Error("expected successfulAttempt in summary")
	}
}
