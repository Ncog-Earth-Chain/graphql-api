package registry

import (
	"testing"
)

func TestNewMockCompiler(t *testing.T) {
	compilerVersion := "0.8.19"
	compiler := NewMockCompiler(compilerVersion)
	
	if compiler == nil {
		t.Fatal("NewMockCompiler returned nil")
	}
	
	if compiler.compilerVersion != compilerVersion {
		t.Errorf("expected compiler version %s, got %s", compilerVersion, compiler.compilerVersion)
	}
}

func TestMockCompilerCompileContract(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	// Test with valid source code
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TestContract {
    uint256 public value;
    
    function setValue(uint256 _value) public {
        value = _value;
    }
}`

	dependencies := map[string]string{
		"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
	}

	bytecode, err := compiler.CompileContract(source, dependencies, "0.8.19")
	
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	
	if bytecode == "" {
		t.Error("expected bytecode to be returned")
	}
	
	// Check if bytecode has expected format (0x + hash + padding)
	if !hasPrefix(bytecode, "0x") {
		t.Error("expected bytecode to start with 0x")
	}
	
	if len(bytecode) < 2000 {
		t.Errorf("expected bytecode to be at least 2000 characters, got %d", len(bytecode))
	}
}

func TestMockCompilerCompileContractEmptySource(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	// Test with empty source code
	bytecode, err := compiler.CompileContract("", make(map[string]string), "0.8.19")
	
	if err == nil {
		t.Error("expected error for empty source code")
	}
	
	if bytecode != "" {
		t.Error("expected empty bytecode for error case")
	}
}

func TestMockCompilerCompileContractEmptyCompilerVersion(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TestContract {
    uint256 public value;
}`

	// Test with empty compiler version (should use default)
	bytecode, err := compiler.CompileContract(source, make(map[string]string), "")
	
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	
	if bytecode == "" {
		t.Error("expected bytecode to be returned")
	}
}

func TestMockCompilerGetCompilerVersion(t *testing.T) {
	compilerVersion := "0.8.19"
	compiler := NewMockCompiler(compilerVersion)
	
	result := compiler.GetCompilerVersion()
	
	if result != compilerVersion {
		t.Errorf("expected compiler version %s, got %s", compilerVersion, result)
	}
}

func TestMockCompilerGenerateMockBytecode(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	source := "contract TestContract {}"
	dependencies := map[string]string{
		"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
	}
	compilerVersion := "0.8.19"
	
	bytecode := compiler.generateMockBytecode(source, dependencies, compilerVersion)
	
	if bytecode == "" {
		t.Error("expected bytecode to be generated")
	}
	
	// Check if bytecode has expected format
	if !hasPrefix(bytecode, "0x") {
		t.Error("expected bytecode to start with 0x")
	}
	
	if len(bytecode) < 2000 {
		t.Errorf("expected bytecode to be at least 2000 characters, got %d", len(bytecode))
	}
	
	// Test deterministic behavior - same inputs should produce same output
	bytecode2 := compiler.generateMockBytecode(source, dependencies, compilerVersion)
	if bytecode != bytecode2 {
		t.Error("expected deterministic bytecode generation")
	}
}

func TestMockCompilerValidateCompilation(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	// Test valid source code
	validSource := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	dependencies := map[string]string{
		"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
	}

	issues := compiler.ValidateCompilation(validSource, dependencies)
	
	if len(issues) > 0 {
		t.Errorf("expected no validation issues for valid source, got: %v", issues)
	}
	
	// Test source code missing pragma
	invalidSource1 := `// SPDX-License-Identifier: MIT

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	issues = compiler.ValidateCompilation(invalidSource1, dependencies)
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

	issues = compiler.ValidateCompilation(invalidSource2, dependencies)
	if len(issues) == 0 {
		t.Error("expected validation issues for source missing contract definition")
	}
	
	// Test missing dependency
	invalidSource3 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	emptyDependencies := make(map[string]string)
	issues = compiler.ValidateCompilation(invalidSource3, emptyDependencies)
	if len(issues) == 0 {
		t.Error("expected validation issues for missing dependency")
	}
	
	// Test incomplete import statement
	invalidSource4 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract {
    // contract code
}`

	issues = compiler.ValidateCompilation(invalidSource4, dependencies)
	if len(issues) == 0 {
		t.Error("expected validation issues for incomplete import statement")
	}
	
	// Test incomplete function definition
	invalidSource5 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TestContract {
    function setValue {
        // missing parentheses
    }
}`

	issues = compiler.ValidateCompilation(invalidSource5, dependencies)
	if len(issues) == 0 {
		t.Error("expected validation issues for incomplete function definition")
	}
}

func TestMockCompilerExtractImports(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@chainlink/contracts/v0.8/AutomationCompatible.sol";
import "./MyContract.sol";

contract TestContract is Ownable {
    // contract code
}`

	imports := compiler.extractImports(source)
	
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

func TestMockCompilerExtractImportsEdgeCases(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	// Test with no imports
	source1 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TestContract {
    uint256 public value;
}`

	imports := compiler.extractImports(source1)
	if len(imports) != 0 {
		t.Errorf("expected no imports, got %d", len(imports))
	}
	
	// Test with commented imports
	source2 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	imports = compiler.extractImports(source2)
	if len(imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(imports))
	}
	
	if imports[0] != "@openzeppelin/contracts/access/Ownable.sol" {
		t.Errorf("expected import '@openzeppelin/contracts/access/Ownable.sol', got %s", imports[0])
	}
	
	// Test with malformed import (no quotes)
	source3 := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import @openzeppelin/contracts/access/Ownable.sol;

contract TestContract {
    // contract code
}`

	imports = compiler.extractImports(source3)
	if len(imports) != 0 {
		t.Errorf("expected no imports for malformed import, got %d", len(imports))
	}
}

func TestMockCompilerGetCompilationMetadata(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	source := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";

contract TestContract is Ownable {
    constructor() Ownable(msg.sender) {}
}`

	dependencies := map[string]string{
		"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
		"./MyContract.sol": "// Local contract source code",
	}

	metadata := compiler.GetCompilationMetadata(source, dependencies)
	
	if metadata == nil {
		t.Fatal("expected metadata to be returned")
	}
	
	// Check basic metadata fields
	if metadata["compilerVersion"] != "0.8.19" {
		t.Errorf("expected compiler version 0.8.19, got %v", metadata["compilerVersion"])
	}
	
	if metadata["sourceLength"] != len(source) {
		t.Errorf("expected source length %d, got %v", len(source), metadata["sourceLength"])
	}
	
	if metadata["dependencies"] != 2 {
		t.Errorf("expected 2 dependencies, got %v", metadata["dependencies"])
	}
	
	// Check imports
	imports, ok := metadata["imports"].([]string)
	if !ok {
		t.Error("expected imports to be a string slice")
	}
	
	if len(imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(imports))
	}
	
	if imports[0] != "@openzeppelin/contracts/access/Ownable.sol" {
		t.Errorf("expected import '@openzeppelin/contracts/access/Ownable.sol', got %s", imports[0])
	}
	
	// Check dependency details
	dependencyDetails, ok := metadata["dependencyDetails"].(map[string]int)
	if !ok {
		t.Error("expected dependencyDetails to be a map[string]int")
	}
	
	if len(dependencyDetails) != 2 {
		t.Errorf("expected 2 dependency details, got %d", len(dependencyDetails))
	}
	
	// Check specific dependency lengths
	if dependencyDetails["@openzeppelin/contracts/access/Ownable.sol"] != 25 {
		t.Errorf("expected Ownable dependency length 25, got %d", 
			dependencyDetails["@openzeppelin/contracts/access/Ownable.sol"])
	}
	
	if dependencyDetails["./MyContract.sol"] != 25 {
		t.Errorf("expected MyContract dependency length 25, got %d", 
			dependencyDetails["./MyContract.sol"])
	}
}

func TestMockCompilerDeterministicBehavior(t *testing.T) {
	compiler := NewMockCompiler("0.8.19")
	
	source := "contract TestContract { uint256 public value; }"
	dependencies := map[string]string{
		"@openzeppelin/contracts/access/Ownable.sol": "// OpenZeppelin source code",
	}
	compilerVersion := "0.8.19"
	
	// Generate bytecode multiple times with same inputs
	bytecode1 := compiler.generateMockBytecode(source, dependencies, compilerVersion)
	bytecode2 := compiler.generateMockBytecode(source, dependencies, compilerVersion)
	bytecode3 := compiler.generateMockBytecode(source, dependencies, compilerVersion)
	
	// All should be identical
	if bytecode1 != bytecode2 || bytecode2 != bytecode3 {
		t.Error("expected deterministic bytecode generation")
	}
	
	// Test with different inputs - should produce different bytecode
	differentSource := "contract DifferentContract { uint256 public value; }"
	differentBytecode := compiler.generateMockBytecode(differentSource, dependencies, compilerVersion)
	
	if bytecode1 == differentBytecode {
		t.Error("expected different inputs to produce different bytecode")
	}
	
	// Test with different dependencies - should produce different bytecode
	differentDependencies := map[string]string{
		"@chainlink/contracts/v0.8/AutomationCompatible.sol": "// Chainlink source code",
	}
	differentBytecode2 := compiler.generateMockBytecode(source, differentDependencies, compilerVersion)
	
	if bytecode1 == differentBytecode2 {
		t.Error("expected different dependencies to produce different bytecode")
	}
	
	// Test with different compiler version - should produce different bytecode
	differentBytecode3 := compiler.generateMockBytecode(source, dependencies, "0.8.20")
	
	if bytecode1 == differentBytecode3 {
		t.Error("expected different compiler version to produce different bytecode")
	}
}

// Helper function to check if a string has a specific prefix
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
