package repository

import (
	"context"
	"ncogearthchain-api-graphql/internal/registry"
	"ncogearthchain-api-graphql/internal/types"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Mock proxy for testing
type mockProxy struct {
	log *mockLogger
	db  *mockDB
}

type mockLogger struct{}

func (ml *mockLogger) Infof(format string, args ...interface{})  {}
func (ml *mockLogger) Warnf(format string, args ...interface{})  {}
func (ml *mockLogger) Errorf(format string, args ...interface{}) {}
func (ml *mockLogger) Debugf(format string, args ...interface{}) {}

type mockDB struct{}

func (md *mockDB) UpdateContract(sc *types.Contract) error {
	return nil
}

// Mock orchestrator for testing
type mockOrchestrator struct {
	shouldSucceed bool
	versionPins   map[string]string
	dependencies  map[string]string
	metadata      map[string]interface{}
}

func (mo *mockOrchestrator) VerifyContract(source, targetBytecode, compilerVersion string) *registry.VerificationResult {
	if mo.shouldSucceed {
		return &registry.VerificationResult{
			Success:      true,
			VersionPins:  mo.versionPins,
			Compiler:     compilerVersion,
			Bytecode:     targetBytecode,
			Attempts:     1,
			Duration:     time.Second,
			Dependencies: mo.dependencies,
			Metadata:     mo.metadata,
		}
	}
	
	return &registry.VerificationResult{
		Success:      false,
		Compiler:     compilerVersion,
		Bytecode:     targetBytecode,
		Attempts:     3,
		Duration:     time.Second * 3,
		Error:        "verification failed",
		Metadata:     map[string]interface{}{"failureReason": "compilation error"},
	}
}

func (mo *mockOrchestrator) GetPackageInfo(packages []string) map[string]*registry.PackageInfo {
	return make(map[string]*registry.PackageInfo)
}

func (mo *mockOrchestrator) GetCompatibilityMatrix(packages []string) map[string]map[string][]string {
	return make(map[string]map[string][]string)
}

func (mo *mockOrchestrator) SuggestOptimalVersions(packages []string) map[string]string {
	return make(map[string]string)
}

func (mo *mockOrchestrator) RefreshPackageCache() {}

func (mo *mockOrchestrator) GetCDNStatus() map[string]bool {
	return map[string]bool{
		"unpkg.com":      true,
		"cdn.jsdelivr.net": true,
		"bundle.run":      true,
		"esm.sh":         true,
	}
}

func (mo *mockOrchestrator) ValidateSourceCode(source string) []string {
	if source == "" {
		return []string{"empty source code"}
	}
	return []string{}
}

func (mo *mockOrchestrator) GetVerificationSummary(result *registry.VerificationResult) map[string]interface{} {
	return map[string]interface{}{
		"success":           result.Success,
		"totalAttempts":    result.Attempts,
		"duration":          result.Duration.String(),
		"compilerVersion":   result.Compiler,
		"dependenciesCount": len(result.Dependencies),
	}
}

// Mock compiler for testing
type mockCompiler struct {
	compilerVersion string
}

func (mc *mockCompiler) CompileContract(source string, dependencies map[string]string, compilerVersion string) (string, error) {
	return "0x1234567890abcdef", nil
}

func (mc *mockCompiler) GetCompilerVersion() string {
	return mc.compilerVersion
}

func TestNewEnhancedContractVerifier(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	if verifier == nil {
		t.Fatal("NewEnhancedContractVerifier returned nil")
	}
	
	if verifier.orchestrator == nil {
		t.Error("orchestrator was not initialized")
	}
	
	if verifier.compiler == nil {
		t.Error("compiler was not initialized")
	}
	
	if verifier.proxy == nil {
		t.Error("proxy was not initialized")
	}
}

func TestEnhancedContractVerifierVerifyContractSuccess(t *testing.T) {
	orchestrator := &mockOrchestrator{
		shouldSucceed: true,
		versionPins: map[string]string{
			"@openzeppelin/contracts": "4.9.6",
		},
		dependencies: map[string]string{
			"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
		},
		metadata: map[string]interface{}{
			"successfulAttempt": 1,
		},
	}
	
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// Create test contract
	contract := &types.Contract{
		Address:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
		TransactionHash: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		SourceCode:     `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`,
		CompilerVersion: "0.8.19",
	}
	
	ctx := context.Background()
	err := verifier.VerifyContract(ctx, contract)
	
	if err != nil {
		t.Errorf("expected no error for successful verification, got %v", err)
	}
	
	// Check if contract was updated
	if contract.Name == "" {
		t.Error("expected contract name to be extracted")
	}
	
	if contract.Compiler == "" {
		t.Error("expected compiler information to be set")
	}
	
	if contract.Validated == nil {
		t.Error("expected validation timestamp to be set")
	}
	
	if contract.Metadata == nil {
		t.Error("expected verification metadata to be set")
	}
}

func TestEnhancedContractVerifierVerifyContractFailure(t *testing.T) {
	orchestrator := &mockOrchestrator{
		shouldSucceed: false,
		metadata: map[string]interface{}{
			"failureReason": "compilation error",
		},
	}
	
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// Create test contract
	contract := &types.Contract{
		Address:        common.HexToAddress("0x1234567890123456789012345678901234567890"),
		TransactionHash: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		SourceCode:     `// Invalid source code`,
		CompilerVersion: "0.8.19",
	}
	
	ctx := context.Background()
	err := verifier.VerifyContract(ctx, contract)
	
	if err == nil {
		t.Error("expected error for failed verification")
	}
	
	// Check if failure metadata was set
	if contract.Metadata == nil {
		t.Error("expected failure metadata to be set")
	}
	
	// Check if metadata contains failure information
	if contract.Metadata["verificationStatus"] != "failed" {
		t.Error("expected verification status to be 'failed'")
	}
}

func TestEnhancedContractVerifierDetermineCompilerVersion(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// Test with specified compiler version
	contract1 := &types.Contract{
		CompilerVersion: "0.8.20",
	}
	
	version := verifier.determineCompilerVersion(contract1)
	if version != "0.8.20" {
		t.Errorf("expected compiler version 0.8.20, got %s", version)
	}
	
	// Test without specified compiler version (should use default)
	contract2 := &types.Contract{
		CompilerVersion: "",
	}
	
	version = verifier.determineCompilerVersion(contract2)
	if version != "0.8.19" {
		t.Errorf("expected compiler version 0.8.19, got %s", version)
	}
}

func TestEnhancedContractVerifierExtractContractName(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// Test with regular contract
	source1 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TestContract {
    uint256 public value;
}`

	name := verifier.extractContractName(source1)
	if name != "TestContract" {
		t.Errorf("expected contract name 'TestContract', got %s", name)
	}
	
	// Test with abstract contract
	source2 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

abstract contract AbstractContract {
    uint256 public value;
}`

	name = verifier.extractContractName(source2)
	if name != "AbstractContract" {
		t.Errorf("expected contract name 'AbstractContract', got %s", name)
	}
	
	// Test with no contract definition
	source3 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// No contract here
function test() {
    // function code
}`

	name = verifier.extractContractName(source3)
	if name != "Unknown" {
		t.Errorf("expected contract name 'Unknown', got %s", name)
	}
	
	// Test with empty source
	name = verifier.extractContractName("")
	if name != "Unknown" {
		t.Errorf("expected contract name 'Unknown' for empty source, got %s", name)
	}
}

func TestEnhancedContractVerifierBuildVerificationMetadata(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	result := &registry.VerificationResult{
		Success:      true,
		VersionPins:  map[string]string{"@openzeppelin/contracts": "4.9.6"},
		Compiler:     "0.8.19",
		Bytecode:     "0x1234567890abcdef",
		Attempts:     1,
		Duration:     time.Second,
		Dependencies: map[string]string{"@openzeppelin/contracts/access/Ownable.sol": "source code"},
		Metadata:     map[string]interface{}{"successfulAttempt": 1},
	}
	
	metadata := verifier.buildVerificationMetadata(result)
	
	if metadata == nil {
		t.Fatal("expected metadata to be returned")
	}
	
	// Check basic metadata fields
	if metadata["verificationMethod"] != "enhanced_registry" {
		t.Error("expected verificationMethod to be 'enhanced_registry'")
	}
	
	if metadata["verificationTime"] == "" {
		t.Error("expected verificationTime to be set")
	}
	
	if metadata["totalAttempts"] != 1 {
		t.Error("expected totalAttempts to be 1")
	}
	
	if metadata["duration"] != "1s" {
		t.Error("expected duration to be '1s'")
	}
	
	if metadata["versionPins"] == nil {
		t.Error("expected versionPins to be set")
	}
	
	if metadata["dependencies"] == nil {
		t.Error("expected dependencies to be set")
	}
	
	if metadata["compilerVersion"] != "0.8.19" {
		t.Error("expected compilerVersion to be '0.8.19'")
	}
	
	// Check orchestrator metadata
	if metadata["successfulAttempt"] != 1 {
		t.Error("expected successfulAttempt to be 1")
	}
}

func TestEnhancedContractVerifierBuildFailureMetadata(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: false}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	result := &registry.VerificationResult{
		Success:      false,
		Compiler:     "0.8.19",
		Bytecode:     "0x1234567890abcdef",
		Attempts:     3,
		Duration:     time.Second * 3,
		Error:        "verification failed",
		Metadata:     map[string]interface{}{"failureReason": "compilation error"},
	}
	
	metadata := verifier.buildFailureMetadata(result)
	
	if metadata == nil {
		t.Fatal("expected metadata to be returned")
	}
	
	// Check basic metadata fields
	if metadata["verificationMethod"] != "enhanced_registry" {
		t.Error("expected verificationMethod to be 'enhanced_registry'")
	}
	
	if metadata["verificationTime"] == "" {
		t.Error("expected verificationTime to be set")
	}
	
	if metadata["verificationStatus"] != "failed" {
		t.Error("expected verificationStatus to be 'failed'")
	}
	
	if metadata["totalAttempts"] != 3 {
		t.Error("expected totalAttempts to be 3")
	}
	
	if metadata["duration"] != "3s" {
		t.Error("expected duration to be '3s'")
	}
	
	if metadata["lastError"] != "verification failed" {
		t.Error("expected lastError to be 'verification failed'")
	}
	
	if metadata["compilerVersion"] != "0.8.19" {
		t.Error("expected compilerVersion to be '0.8.19'")
	}
	
	// Check orchestrator metadata
	if metadata["failureReason"] != "compilation error" {
		t.Error("expected failureReason to be 'compilation error'")
	}
}

func TestEnhancedContractVerifierGetPackageInfo(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	packageInfo := verifier.GetPackageInfo(source)
	
	if packageInfo == nil {
		t.Error("expected package info to be returned")
	}
}

func TestEnhancedContractVerifierGetCompatibilityMatrix(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	matrix := verifier.GetCompatibilityMatrix(source)
	
	if matrix == nil {
		t.Error("expected compatibility matrix to be returned")
	}
}

func TestEnhancedContractVerifierSuggestOptimalVersions(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	suggestions := verifier.SuggestOptimalVersions(source)
	
	if suggestions == nil {
		t.Error("expected optimal version suggestions to be returned")
	}
}

func TestEnhancedContractVerifierRefreshPackageCache(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// This should not panic
	verifier.RefreshPackageCache()
}

func TestEnhancedContractVerifierGetCDNStatus(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	status := verifier.GetCDNStatus()
	
	if status == nil {
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

func TestEnhancedContractVerifierGetVerificationSummary(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	result := &registry.VerificationResult{
		Success:      true,
		VersionPins:  map[string]string{"@openzeppelin/contracts": "4.9.6"},
		Compiler:     "0.8.19",
		Bytecode:     "0x1234567890abcdef",
		Attempts:     1,
		Duration:     time.Second,
		Dependencies: map[string]string{"@openzeppelin/contracts/access/Ownable.sol": "source code"},
		Metadata:     map[string]interface{}{"successfulAttempt": 1},
	}
	
	summary := verifier.GetVerificationSummary(result)
	
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
		t.Error("expected compilerVersion to be '0.8.19' in summary")
	}
	
	if summary["dependenciesCount"] != 1 {
		t.Error("expected dependenciesCount to be 1 in summary")
	}
}

func TestEnhancedContractVerifierValidateSourceCode(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	// Test valid source code
	validSource := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	issues := verifier.ValidateSourceCode(validSource)
	if len(issues) > 0 {
		t.Errorf("expected no validation issues for valid source, got: %v", issues)
	}
	
	// Test invalid source code
	invalidSource := `// SPDX-License-Identifier: MIT

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	issues = verifier.ValidateSourceCode(invalidSource)
	if len(issues) == 0 {
		t.Error("expected validation issues for invalid source")
	}
}

func TestEnhancedContractVerifierGetRegistryStats(t *testing.T) {
	orchestrator := &mockOrchestrator{shouldSucceed: true}
	compiler := &mockCompiler{compilerVersion: "0.8.19"}
	proxy := &mockProxy{
		log: &mockLogger{},
		db:  &mockDB{},
	}
	
	verifier := NewEnhancedContractVerifier(orchestrator, compiler, proxy)
	
	stats := verifier.GetRegistryStats()
	
	if stats == nil {
		t.Fatal("expected registry stats to be returned")
	}
	
	// Check basic stats fields
	if stats["timestamp"] == "" {
		t.Error("expected timestamp to be set")
	}
	
	if stats["cdnStatus"] == nil {
		t.Error("expected cdnStatus to be set")
	}
	
	// Check CDN status
	cdnStatus, ok := stats["cdnStatus"].(map[string]bool)
	if !ok {
		t.Error("expected cdnStatus to be map[string]bool")
	}
	
	expectedCDNs := []string{"unpkg.com", "cdn.jsdelivr.net", "bundle.run", "esm.sh"}
	for _, cdn := range expectedCDNs {
		if _, exists := cdnStatus[cdn]; !exists {
			t.Errorf("expected CDN %s not found in stats", cdn)
		}
	}
}
