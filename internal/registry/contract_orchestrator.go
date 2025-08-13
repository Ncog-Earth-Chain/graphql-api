package registry

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// VerificationResult represents the result of a contract verification attempt
type VerificationResult struct {
	Success      bool                   `json:"success"`
	VersionPins  map[string]string     `json:"versionPins"`
	Compiler     string                 `json:"compiler"`
	Bytecode     string                 `json:"bytecode"`
	Error        string                 `json:"error,omitempty"`
	Attempts     int                    `json:"attempts"`
	Duration     time.Duration          `json:"duration"`
	Dependencies map[string]string     `json:"dependencies"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// ContractOrchestrator coordinates the contract verification process
type ContractOrchestrator struct {
	registry         *PackageRegistry
	dependencyResolver *DependencyResolver
	versionStrategy  *VersionStrategy
	compiler         CompilerInterface
}

// CompilerInterface defines the interface for contract compilation
type CompilerInterface interface {
	CompileContract(source string, dependencies map[string]string, compilerVersion string) (string, error)
	GetCompilerVersion() string
}

// NewContractOrchestrator creates a new contract orchestrator instance
func NewContractOrchestrator(
	registry *PackageRegistry,
	dependencyResolver *DependencyResolver,
	versionStrategy *VersionStrategy,
	compiler CompilerInterface,
) *ContractOrchestrator {
	return &ContractOrchestrator{
		registry:         registry,
		dependencyResolver: dependencyResolver,
		versionStrategy:  versionStrategy,
		compiler:         compiler,
	}
}

// VerifyContract performs the complete contract verification process
func (co *ContractOrchestrator) VerifyContract(
	source string,
	targetBytecode string,
	compilerVersion string,
) *VerificationResult {
	startTime := time.Now()
	
	result := &VerificationResult{
		Success:      false,
		VersionPins:  make(map[string]string),
		Compiler:     compilerVersion,
		Bytecode:     targetBytecode,
		Attempts:     0,
		Duration:     0,
		Dependencies: make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	// Step 1: Detect packages from source
	detectedPackages := co.versionStrategy.DetectPackagesFromSource(source)
	result.Metadata["detectedPackages"] = detectedPackages
	result.Metadata["totalPackages"] = len(detectedPackages)

	if len(detectedPackages) == 0 {
		result.Error = "No packages detected in source code"
		result.Duration = time.Since(startTime)
		return result
	}

	// Step 2: Generate version attempts
	versionAttempts := co.versionStrategy.GenerateVersionAttempts(detectedPackages)
	result.Metadata["totalVersionAttempts"] = len(versionAttempts)

	// Step 3: Try each version combination
	for attemptIndex, versionPins := range versionAttempts {
		result.Attempts++
		
		log.Printf("Attempt %d/%d: Trying version pins: %v", 
			attemptIndex+1, len(versionAttempts), versionPins)

		// Try to verify with this version combination
		success, err := co.tryVerification(source, targetBytecode, compilerVersion, versionPins, result)
		
		if success {
			result.Success = true
			result.VersionPins = versionPins
			result.Duration = time.Since(startTime)
			result.Metadata["successfulAttempt"] = attemptIndex + 1
			result.Metadata["successfulVersionPins"] = versionPins
			
			log.Printf("Verification successful on attempt %d with versions: %v", 
				attemptIndex+1, versionPins)
			return result
		}

		// Log the error for this attempt
		log.Printf("Attempt %d failed: %v", attemptIndex+1, err)
		result.Metadata[fmt.Sprintf("attempt_%d_error", attemptIndex+1)] = err.Error()
	}

	// If we get here, all attempts failed
	result.Error = fmt.Sprintf("All %d verification attempts failed", len(versionAttempts))
	result.Duration = time.Since(startTime)
	result.Metadata["failureReason"] = "All version combinations failed"
	
	return result
}

// tryVerification attempts to verify a contract with specific version pins
func (co *ContractOrchestrator) tryVerification(
	source string,
	targetBytecode string,
	compilerVersion string,
	versionPins map[string]string,
	result *VerificationResult,
) (bool, error) {
	
	// Step 1: Resolve all dependencies with the given version pins
	dependencies, err := co.dependencyResolver.ResolveAllDependencies(source, versionPins)
	if err != nil {
		return false, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	// Store resolved dependencies
	for importPath, depSource := range dependencies {
		result.Dependencies[importPath] = depSource
	}

	// Step 2: Compile the contract with resolved dependencies
	compiledBytecode, err := co.compiler.CompileContract(source, dependencies, compilerVersion)
	if err != nil {
		return false, fmt.Errorf("compilation failed: %w", err)
	}

	// Step 3: Compare bytecodes
	if co.compareBytecodes(compiledBytecode, targetBytecode) {
		return true, nil
	}

	return false, fmt.Errorf("bytecode mismatch")
}

// compareBytecodes compares two bytecode strings for verification
func (co *ContractOrchestrator) compareBytecodes(compiled, target string) bool {
	// Clean and normalize bytecodes
	compiledClean := co.cleanBytecode(compiled)
	targetClean := co.cleanBytecode(target)

	// Direct comparison
	if compiledClean == targetClean {
		return true
	}

	// Try removing metadata (common cause of mismatch)
	compiledNoMeta := co.removeMetadata(compiledClean)
	targetNoMeta := co.removeMetadata(targetClean)
	
	if compiledNoMeta == targetNoMeta {
		return true
	}

	// Try masking link references
	compiledMasked := co.maskLinkReferences(compiledNoMeta)
	targetMasked := co.maskLinkReferences(targetNoMeta)
	
	if compiledMasked == targetMasked {
		return true
	}

	return false
}

// cleanBytecode removes common formatting differences
func (co *ContractOrchestrator) cleanBytecode(bytecode string) string {
	// Remove 0x prefix if present
	if strings.HasPrefix(bytecode, "0x") {
		bytecode = bytecode[2:]
	}
	
	// Remove whitespace and newlines
	bytecode = strings.ReplaceAll(bytecode, " ", "")
	bytecode = strings.ReplaceAll(bytecode, "\n", "")
	bytecode = strings.ReplaceAll(bytecode, "\r", "")
	bytecode = strings.ReplaceAll(bytecode, "\t", "")
	
	return strings.ToLower(bytecode)
}

// removeMetadata removes metadata from bytecode
func (co *ContractOrchestrator) removeMetadata(bytecode string) string {
	// Look for metadata hash (32 bytes at the end)
	if len(bytecode) > 64 {
		// Remove last 64 characters (32 bytes = 64 hex chars)
		return bytecode[:len(bytecode)-64]
	}
	return bytecode
}

// maskLinkReferences masks library link references
func (co *ContractOrchestrator) maskLinkReferences(bytecode string) string {
	// Replace library placeholders with zeros
	// This is a simplified approach - in practice, you might need more sophisticated detection
	// Look for patterns like __$...$__ and replace with zeros
	// For now, return as-is
	return bytecode
}

// GetPackageInfo returns detailed information about detected packages
func (co *ContractOrchestrator) GetPackageInfo(packages []string) map[string]*PackageInfo {
	packageInfo := make(map[string]*PackageInfo)
	
	for _, pkg := range packages {
		if info, err := co.registry.DiscoverPackage(pkg); err == nil {
			packageInfo[pkg] = info
		}
	}
	
	return packageInfo
}

// GetCompatibilityMatrix returns compatibility information for packages
func (co *ContractOrchestrator) GetCompatibilityMatrix(packages []string) map[string]map[string][]string {
	return co.versionStrategy.GetCompatibilityMatrix(packages)
}

// SuggestOptimalVersions suggests optimal version combinations
func (co *ContractOrchestrator) SuggestOptimalVersions(packages []string) map[string]string {
	return co.versionStrategy.SuggestOptimalVersions(packages)
}

// DetectPackagesFromSource exposes package detection from source code
func (co *ContractOrchestrator) DetectPackagesFromSource(source string) []string {
    return co.versionStrategy.DetectPackagesFromSource(source)
}

// RefreshPackageCache refreshes the package registry cache
func (co *ContractOrchestrator) RefreshPackageCache() {
	co.registry.RefreshCache()
}

// GetCDNStatus returns the status of available CDNs
func (co *ContractOrchestrator) GetCDNStatus() map[string]bool {
	return co.dependencyResolver.GetCDNStatus()
}

// ValidateSourceCode validates the source code for common issues
func (co *ContractOrchestrator) ValidateSourceCode(source string) []string {
	var issues []string
	
	// Check for basic syntax issues
	if !strings.Contains(source, "pragma solidity") {
		issues = append(issues, "Missing pragma solidity statement")
	}
	
	// Check for contract definition
	if !strings.Contains(source, "contract ") && !strings.Contains(source, "abstract contract ") {
		issues = append(issues, "No contract definition found")
	}
	
	// Check for import issues
	imports := co.versionStrategy.extractImports(source)
	for _, importPath := range imports {
		if strings.Contains(importPath, "..") {
			issues = append(issues, fmt.Sprintf("Relative import path detected: %s", importPath))
		}
	}
	
	// Check for common compilation issues
	if strings.Contains(source, "import") && !strings.Contains(source, "from") {
		// This might indicate an incomplete import statement
		issues = append(issues, "Potential incomplete import statement")
	}
	
	return issues
}

// GetVerificationSummary returns a summary of the verification process
func (co *ContractOrchestrator) GetVerificationSummary(result *VerificationResult) map[string]interface{} {
	summary := map[string]interface{}{
		"success":           result.Success,
		"totalAttempts":    result.Attempts,
		"duration":          result.Duration.String(),
		"compilerVersion":   result.Compiler,
		"dependenciesCount": len(result.Dependencies),
	}
	
	if result.Success {
		summary["successfulVersions"] = result.VersionPins
		summary["successfulAttempt"] = result.Metadata["successfulAttempt"]
	} else {
		summary["lastError"] = result.Error
		summary["failureReason"] = result.Metadata["failureReason"]
	}
	
	return summary
}
