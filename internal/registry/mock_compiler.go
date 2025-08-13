package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// MockCompiler is a mock implementation of CompilerInterface for testing
type MockCompiler struct {
	compilerVersion string
}

// NewMockCompiler creates a new mock compiler instance
func NewMockCompiler(compilerVersion string) *MockCompiler {
	return &MockCompiler{
		compilerVersion: compilerVersion,
	}
}

// CompileContract simulates contract compilation and returns a mock bytecode
func (mc *MockCompiler) CompileContract(source string, dependencies map[string]string, compilerVersion string) (string, error) {
	// Validate inputs
	if source == "" {
		return "", fmt.Errorf("source code cannot be empty")
	}
	
	if compilerVersion == "" {
		compilerVersion = mc.compilerVersion
	}
	
	// Generate a deterministic bytecode based on source and dependencies
	bytecode := mc.generateMockBytecode(source, dependencies, compilerVersion)
	
	return bytecode, nil
}

// GetCompilerVersion returns the compiler version
func (mc *MockCompiler) GetCompilerVersion() string {
	return mc.compilerVersion
}

// generateMockBytecode generates a deterministic mock bytecode
func (mc *MockCompiler) generateMockBytecode(source string, dependencies map[string]string, compilerVersion string) string {
	// Create a hash of the source code and dependencies
	hashInput := source + compilerVersion
	
	// Add dependencies to the hash
	for importPath, depSource := range dependencies {
		hashInput += importPath + depSource
	}
	
	// Generate SHA256 hash
	hash := sha256.Sum256([]byte(hashInput))
	hashHex := hex.EncodeToString(hash[:])
	
	// Create a mock bytecode with the hash
	// Format: 0x + 32 bytes of hash + some padding
	padding := "0000000000000000000000000000000000000000000000000000000000000000"
	
	// Add some realistic bytecode patterns
	bytecode := "0x" + hashHex + padding
	
	// Ensure it's a reasonable length (typical Solidity bytecode is 1000+ bytes)
	if len(bytecode) < 2000 {
		bytecode += strings.Repeat("0", 2000-len(bytecode))
	}
	
	return bytecode
}

// ValidateCompilation checks if the compilation would succeed
func (mc *MockCompiler) ValidateCompilation(source string, dependencies map[string]string) []string {
	var issues []string
	
	// Check for basic syntax
	if !strings.Contains(source, "pragma solidity") {
		issues = append(issues, "Missing pragma solidity statement")
	}
	
	if !strings.Contains(source, "contract ") && !strings.Contains(source, "abstract contract ") {
		issues = append(issues, "No contract definition found")
	}
	
	// Check for import issues
	imports := mc.extractImports(source)
	for _, importPath := range imports {
		if _, exists := dependencies[importPath]; !exists {
			issues = append(issues, fmt.Sprintf("Missing dependency: %s", importPath))
		}
	}
	
	// Check for common compilation errors
	if strings.Contains(source, "import") && !strings.Contains(source, "from") {
		issues = append(issues, "Incomplete import statement")
	}
	
	if strings.Contains(source, "function") && !strings.Contains(source, "(") {
		issues = append(issues, "Incomplete function definition")
	}
	
	return issues
}

// extractImports extracts import statements from Solidity source
func (mc *MockCompiler) extractImports(source string) []string {
	var imports []string
	
	// Simple regex-like extraction
	lines := strings.Split(source, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "import") {
			// Extract the import path
			if strings.Contains(line, "\"") {
				start := strings.Index(line, "\"")
				end := strings.LastIndex(line, "\"")
				if start != -1 && end != -1 && end > start {
					importPath := line[start+1 : end]
					imports = append(imports, importPath)
				}
			}
		}
	}
	
	return imports
}

// GetCompilationMetadata returns metadata about the compilation
func (mc *MockCompiler) GetCompilationMetadata(source string, dependencies map[string]string) map[string]interface{} {
	metadata := map[string]interface{}{
		"compilerVersion": mc.compilerVersion,
		"sourceLength":    len(source),
		"dependencies":    len(dependencies),
		"imports":         mc.extractImports(source),
	}
	
	// Add dependency details
	dependencyDetails := make(map[string]int)
	for importPath, depSource := range dependencies {
		dependencyDetails[importPath] = len(depSource)
	}
	metadata["dependencyDetails"] = dependencyDetails
	
	return metadata
}
