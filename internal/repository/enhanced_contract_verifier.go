package repository

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/registry"
	"ncogearthchain-api-graphql/internal/types"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// EnhancedContractVerifier provides advanced contract verification using the registry system
type EnhancedContractVerifier struct {
	orchestrator *registry.ContractOrchestrator
	compiler     registry.CompilerInterface
	proxy        *proxy // Reference to the existing proxy for database operations
}

// NewEnhancedContractVerifier creates a new enhanced contract verifier
func NewEnhancedContractVerifier(
	orchestrator *registry.ContractOrchestrator,
	compiler registry.CompilerInterface,
	proxy *proxy,
) *EnhancedContractVerifier {
	return &EnhancedContractVerifier{
		orchestrator: orchestrator,
		compiler:     compiler,
		proxy:        proxy,
	}
}

// VerifyContract performs enhanced contract verification using the registry system
func (ecv *EnhancedContractVerifier) VerifyContract(ctx context.Context, sc *types.Contract) error {
	startTime := time.Now()
	
	// Step 1: Validate source code
	validationIssues := ecv.orchestrator.ValidateSourceCode(sc.SourceCode)
	if len(validationIssues) > 0 {
		ecv.proxy.log.Warnf("Source code validation issues found: %v", validationIssues)
	}

	// Step 2: Get contract deployment transaction
	tx, err := ecv.proxy.Transaction(&sc.TransactionHash)
	if err != nil {
		return fmt.Errorf("failed to get contract deployment transaction: %w", err)
	}

	// Step 3: Determine compiler version
	compilerVersion := ecv.determineCompilerVersion(sc)
	
	// Step 4: Perform enhanced verification using the orchestrator
	result := ecv.orchestrator.VerifyContract(
		sc.SourceCode,
		hexutil.Encode(tx.Data),
		compilerVersion,
	)

	// Step 5: Process verification result
	if result.Success {
		return ecv.handleSuccessfulVerification(sc, result, tx)
	} else {
		return ecv.handleFailedVerification(sc, result)
	}
}

// determineCompilerVersion determines the appropriate compiler version
func (ecv *EnhancedContractVerifier) determineCompilerVersion(sc *types.Contract) string {
	// Use specified compiler version if available
	if sc.CompilerVersion != "" {
		return sc.CompilerVersion
	}
	
	// Use the orchestrator's compiler version
	return ecv.compiler.GetCompilerVersion()
}

// handleSuccessfulVerification processes a successful verification
func (ecv *EnhancedContractVerifier) handleSuccessfulVerification(
	sc *types.Contract,
	result *registry.VerificationResult,
	tx *types.Transaction,
) error {
	
	// Update contract information
	if len(sc.Name) == 0 {
		// Extract contract name from source code or use default
		sc.Name = ecv.extractContractName(sc.SourceCode)
	}

	// Update compiler information
	sc.Compiler = fmt.Sprintf("Solidity %s", result.Compiler)
	
	// Set validation timestamp
	now := hexutil.Uint64(uint64(time.Now().Unix()))
	sc.Validated = &now

	// Store verification metadata
	sc.Metadata = ecv.buildVerificationMetadata(result)

	// Update the contract in the database
	if err := ecv.proxy.db.UpdateContract(sc); err != nil {
		return fmt.Errorf("failed to update contract in database: %w", err)
	}

	// Clear cache
	ecv.proxy.cache.EvictContract(&sc.Address)

	// Log success
	ecv.proxy.log.Infof("Contract %s [%s] successfully verified using enhanced system", 
		sc.Address.String(), sc.Name)
	ecv.proxy.log.Debugf("Verification details: %+v", result)

	return nil
}

// handleFailedVerification processes a failed verification
func (ecv *EnhancedContractVerifier) handleFailedVerification(
	sc *types.Contract,
	result *registry.VerificationResult,
) error {
	
	// Log detailed failure information
	ecv.proxy.log.Errorf("Contract verification failed after %d attempts", result.Attempts)
	ecv.proxy.log.Errorf("Last error: %s", result.Error)
	
	// Store failure metadata for debugging
	sc.Metadata = ecv.buildFailureMetadata(result)
	
	// Update contract with failure information
	if err := ecv.proxy.db.UpdateContract(sc); err != nil {
		ecv.proxy.log.Errorf("Failed to update contract with failure metadata: %s", err.Error())
	}

	return fmt.Errorf("enhanced contract verification failed: %s", result.Error)
}

// buildVerificationMetadata builds metadata for successful verification
func (ecv *EnhancedContractVerifier) buildVerificationMetadata(result *registry.VerificationResult) map[string]interface{} {
	metadata := map[string]interface{}{
		"verificationMethod": "enhanced_registry",
		"verificationTime":   time.Now().UTC().Format(time.RFC3339),
		"totalAttempts":      result.Attempts,
		"duration":           result.Duration.String(),
		"versionPins":        result.VersionPins,
		"dependencies":       result.Dependencies,
		"compilerVersion":    result.Compiler,
	}
	
	// Add orchestrator metadata
	if result.Metadata != nil {
		for key, value := range result.Metadata {
			metadata[key] = value
		}
	}
	
	return metadata
}

// buildFailureMetadata builds metadata for failed verification
func (ecv *EnhancedContractVerifier) buildFailureMetadata(result *registry.VerificationResult) map[string]interface{} {
	metadata := map[string]interface{}{
		"verificationMethod": "enhanced_registry",
		"verificationTime":   time.Now().UTC().Format(time.RFC3339),
		"verificationStatus": "failed",
		"totalAttempts":      result.Attempts,
		"duration":           result.Duration.String(),
		"lastError":          result.Error,
		"compilerVersion":    result.Compiler,
	}
	
	// Add orchestrator metadata
	if result.Metadata != nil {
		for key, value := range result.Metadata {
			metadata[key] = value
		}
	}
	
	return metadata
}

// extractContractName extracts the contract name from source code
func (ecv *EnhancedContractVerifier) extractContractName(source string) string {
	// Simple regex to find contract name
	// This is a basic implementation - could be enhanced with proper Solidity parsing
	lines := strings.Split(source, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "contract ") {
			// Extract contract name
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
		if strings.HasPrefix(line, "abstract contract ") {
			// Extract abstract contract name
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
	}
	
	return "Unknown"
}

// GetPackageInfo returns detailed information about detected packages
func (ecv *EnhancedContractVerifier) GetPackageInfo(source string) map[string]*registry.PackageInfo {
	// Detect packages from source
	packages := ecv.orchestrator.versionStrategy.DetectPackagesFromSource(source)
	
	// Get package information from registry
	return ecv.orchestrator.GetPackageInfo(packages)
}

// GetCompatibilityMatrix returns compatibility information for packages
func (ecv *EnhancedContractVerifier) GetCompatibilityMatrix(source string) map[string]map[string][]string {
	// Detect packages from source
	packages := ecv.orchestrator.versionStrategy.DetectPackagesFromSource(source)
	
	// Get compatibility matrix
	return ecv.orchestrator.GetCompatibilityMatrix(packages)
}

// SuggestOptimalVersions suggests optimal version combinations
func (ecv *EnhancedContractVerifier) SuggestOptimalVersions(source string) map[string]string {
	// Detect packages from source
	packages := ecv.orchestrator.versionStrategy.DetectPackagesFromSource(source)
	
	// Get optimal version suggestions
	return ecv.orchestrator.SuggestOptimalVersions(packages)
}

// RefreshPackageCache refreshes the package registry cache
func (ecv *EnhancedContractVerifier) RefreshPackageCache() {
	ecv.orchestrator.RefreshPackageCache()
}

// GetCDNStatus returns the status of available CDNs
func (ecv *EnhancedContractVerifier) GetCDNStatus() map[string]bool {
	return ecv.orchestrator.GetCDNStatus()
}

// GetVerificationSummary returns a summary of the verification process
func (ecv *EnhancedContractVerifier) GetVerificationSummary(result *registry.VerificationResult) map[string]interface{} {
	return ecv.orchestrator.GetVerificationSummary(result)
}

// ValidateSourceCode validates source code for common issues
func (ecv *EnhancedContractVerifier) ValidateSourceCode(source string) []string {
	return ecv.orchestrator.ValidateSourceCode(source)
}

// GetRegistryStats returns statistics about the registry system
func (ecv *EnhancedContractVerifier) GetRegistryStats() map[string]interface{} {
	stats := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	
	// Get CDN status
	stats["cdnStatus"] = ecv.GetCDNStatus()
	
	// Get cache information (if available)
	// This could be enhanced to include more detailed registry statistics
	
	return stats
}
