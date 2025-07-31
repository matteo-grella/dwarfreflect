// Copyright (c) 2025 Matteo Grella <matteogrella@gmail.com>
// Licensed under the MIT License. See LICENSE file for details.

package dwarfreflect

import (
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
)

// Global DWARF resolver for parameter name discovery from binary debug info
var (
	globalResolver  *DWARFResolver
	resolverOnce    sync.Once
	resolverInitErr error
)

// ExecutableFormat represents the type of executable file
type ExecutableFormat int

const (
	FormatUnknown ExecutableFormat = iota
	FormatELF                      // Linux, FreeBSD, etc.
	FormatPE                       // Windows
	FormatMachO                    // macOS, iOS
)

// FormatString returns a human-readable string for the executable format
func (f ExecutableFormat) String() string {
	switch f {
	case FormatELF:
		return "ELF"
	case FormatPE:
		return "PE"
	case FormatMachO:
		return "Mach-O"
	default:
		return "Unknown"
	}
}

// DWARFResolver extracts parameter names from DWARF debug information in the binary
type DWARFResolver struct {
	mu             sync.RWMutex
	functionMap    map[string][]string // maps function names to parameter names
	dwarfData      *dwarf.Data
	executablePath string
}

// initResolver initializes the global DWARF resolver
func initResolver() {
	globalResolver = &DWARFResolver{
		functionMap: make(map[string][]string),
	}

	// Try to initialize DWARF data from current executable
	if err := globalResolver.loadDWARFData(); err != nil {
		resolverInitErr = err
		return
	}
}

// DetectExecutableFormat determines the executable format by examining magic bytes
func DetectExecutableFormat(filename string) (ExecutableFormat, error) {
	file, err := os.Open(filename)
	if err != nil {
		return FormatUnknown, err
	}
	defer file.Close()

	// Read first 4 bytes to check magic numbers
	magic := make([]byte, 4)
	if _, err := file.Read(magic); err != nil {
		return FormatUnknown, err
	}

	// Check magic numbers
	switch {
	case magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F':
		return FormatELF, nil
	case magic[0] == 'M' && magic[1] == 'Z': // PE files start with "MZ"
		return FormatPE, nil
	case (magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xce) || // Mach-O 32-bit big endian
		(magic[0] == 0xce && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe) || // Mach-O 32-bit little endian
		(magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xcf) || // Mach-O 64-bit big endian
		(magic[0] == 0xcf && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe): // Mach-O 64-bit little endian
		return FormatMachO, nil
	default:
		return FormatUnknown, fmt.Errorf("unknown executable format, magic bytes: %x", magic)
	}
}

// loadDWARFData loads DWARF debugging information from the current executable (cross-platform)
func (dr *DWARFResolver) loadDWARFData() error {
	executablePath, err := os.Executable() // get current executable path
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	dr.executablePath = executablePath

	format, err := DetectExecutableFormat(executablePath)
	if err != nil {
		return fmt.Errorf("failed to detect executable format: %v", err)
	}

	// Extract DWARF data based on format
	var dwarfData *dwarf.Data
	switch format {
	case FormatELF:
		elfFile, err := elf.Open(executablePath)
		if err != nil {
			return fmt.Errorf("failed to open ELF file: %v", err)
		}
		defer elfFile.Close()
		dwarfData, err = elfFile.DWARF()
		if err != nil {
			return fmt.Errorf("failed to extract DWARF from ELF file: %v", err)
		}

	case FormatPE:
		peFile, err := pe.Open(executablePath)
		if err != nil {
			return fmt.Errorf("failed to open PE file: %v", err)
		}
		defer peFile.Close()
		dwarfData, err = peFile.DWARF()
		if err != nil {
			return fmt.Errorf("failed to extract DWARF from PE file: %v", err)
		}

	case FormatMachO:

		machoFile, err := macho.Open(executablePath)
		if err != nil {
			return fmt.Errorf("failed to open Mach-O file: %v", err)
		}
		defer machoFile.Close()
		dwarfData, err = machoFile.DWARF()
		if err != nil {
			return fmt.Errorf("failed to extract DWARF from Mach-O file: %v", err)
		}

	default:
		return fmt.Errorf("unsupported executable format: %v (%s)", format, format.String())
	}

	dr.dwarfData = dwarfData
	return dr.indexFunctions()
}

// indexFunctions parses DWARF info and builds function parameter index
func (dr *DWARFResolver) indexFunctions() error {
	reader := dr.dwarfData.Reader()

	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}

		// Look for function/subprogram entries
		if entry.Tag == dwarf.TagSubprogram {
			funcName := ""
			if nameField := entry.AttrField(dwarf.AttrName); nameField != nil {
				funcName = nameField.Val.(string)
			}

			if funcName != "" && entry.Children {
				paramNames := dr.extractParametersFromDWARF(reader)
				dr.functionMap[funcName] = paramNames
			}
		}
	}

	return nil
}

// extractParametersFromDWARF extracts parameter names from DWARF child entries
// Note: This includes both input parameters AND return value parameters (~r0, ~r1, etc.)
// Filtering happens later in discoverParameterNames()
func (dr *DWARFResolver) extractParametersFromDWARF(reader *dwarf.Reader) []string {
	var paramNames []string

	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}

		// Stop when we hit the end of children (entry with Tag 0)
		if entry.Tag == 0 {
			break
		}

		// Look for formal parameters (includes both input and return value parameters)
		if entry.Tag == dwarf.TagFormalParameter {
			if nameField := entry.AttrField(dwarf.AttrName); nameField != nil {
				paramName := nameField.Val.(string)
				paramNames = append(paramNames, paramName)
			}
		}
	}

	return paramNames
}

// discoverParameterNames tries to find parameter names in DWARF debug info
func (dr *DWARFResolver) discoverParameterNames(funcName string, paramCount int) ([]string, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	// Try various function name formats to match runtime names with DWARF
	candidates := generateFunctionKeyCandidates(funcName)

	for _, candidate := range candidates {
		if allParams, exists := dr.functionMap[candidate]; exists {
			// Filter out return value parameters - only take the first paramCount parameters
			// Go DWARF includes both input parameters AND return value parameters (like ~r0, ~r1)
			// Input parameters come first, return values come after
			if len(allParams) >= paramCount {
				inputParams := allParams[:paramCount]

				// Additional validation: skip obvious return value parameters
				// Return values often start with ~r (like ~r0, ~r1) or have suspicious names
				var validParams []string
				for i, param := range inputParams {
					// Skip parameters that look like return values
					if strings.HasPrefix(param, "~r") && (i >= paramCount/2) {
						// If we see ~r parameters and we're past halfway through expected params,
						// this might indicate we're hitting return values
						break
					}
					validParams = append(validParams, param)
				}

				// Return the filtered parameters if we got the expected count
				if len(validParams) == paramCount {
					return validParams, nil
				}
				// If validation filtered too many, return the first paramCount as-is
				if len(inputParams) == paramCount {
					return inputParams, nil
				}
			}
		}
	}

	// Get executable format for better error message
	format, execPath, _ := GetExecutableInfo()

	// Return detailed error explaining why parameter names couldn't be extracted
	return nil, fmt.Errorf(`dwarfreflect: Cannot extract real parameter names for function %q

Possible causes:
• Binary built with -ldflags="-w" (strips DWARF debug info)
• Binary built with -ldflags="-s -w" (strips symbols + debug info)
• Binary was stripped using external tools (strip command)
• Test binary without debug info (use -ldflags="" in test configuration)

Current executable: %s (format: %s)
Available DWARF functions: %d

Solutions:
• Build with debug info: go build (default)
• For tests: use -ldflags=""

Function: %s | Expected parameters: %d`,
		funcName, execPath, format, len(dr.functionMap), funcName, paramCount)
}

// generateFunctionKeyCandidates creates possible lookup keys from runtime function name
func generateFunctionKeyCandidates(runtimeName string) []string {
	candidates := []string{runtimeName}

	// Handle different runtime name formats
	// e.g., "github.com/user/repo/pkg.funcName" -> ["github.com/user/repo/pkg.funcName", "pkg.funcName"]
	parts := strings.Split(runtimeName, "/")
	if len(parts) > 1 {
		// Try with just the last part: "pkg.funcName"
		candidates = append(candidates, parts[len(parts)-1])
	}

	// Handle method names: "pkg.(*Type).Method" or "pkg.Type.Method"
	if strings.Contains(runtimeName, ".") {
		lastDot := strings.LastIndex(runtimeName, ".")
		if lastDot > 0 {
			beforeLast := runtimeName[:lastDot]
			methodName := runtimeName[lastDot+1:]

			// Try different receiver formats
			if strings.Contains(beforeLast, "(*") {
				candidates = append(candidates, beforeLast+"."+methodName)
			}
		}
	}

	return candidates
}

// extractPackagePath extracts package path from runtime function name
func extractPackagePath(funcName string) string {
	// Handle function names like:
	// "main.funcName" -> "main"
	// "github.com/user/repo/pkg.funcName" -> "github.com/user/repo/pkg"
	// "github.com/user/repo/pkg.(*Type).Method" -> "github.com/user/repo/pkg"

	lastSlash := strings.LastIndex(funcName, "/")
	if lastSlash == -1 {
		// No slashes, probably "main.funcName"
		parts := strings.Split(funcName, ".")
		if len(parts) > 1 {
			return parts[0]
		}
		return "main"
	}

	// Find the first dot after the last slash
	remaining := funcName[lastSlash+1:]
	firstDot := strings.Index(remaining, ".")
	if firstDot == -1 {
		return funcName // Fallback
	}

	return funcName[:lastSlash+1+firstDot]
}

// GetDWARFStatus returns information about DWARF debug info availability
func GetDWARFStatus() (available bool, funcCount int, err error) {
	resolverOnce.Do(initResolver)

	if resolverInitErr != nil {
		return false, 0, resolverInitErr
	}

	if globalResolver.dwarfData == nil {
		return false, 0, fmt.Errorf("DWARF debug information not available")
	}

	globalResolver.mu.RLock()
	funcCount = len(globalResolver.functionMap)
	globalResolver.mu.RUnlock()

	return true, funcCount, nil
}

// GetExecutableInfo returns information about the current executable
func GetExecutableInfo() (ExecutableFormat, string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return FormatUnknown, "", err
	}

	format, err := DetectExecutableFormat(execPath)
	if err != nil {
		return FormatUnknown, execPath, err
	}

	return format, execPath, nil
}

// IsDWARFSupported checks if DWARF is likely supported for the current platform and format
func IsDWARFSupported() (bool, string, error) {
	format, _, err := GetExecutableInfo()
	if err != nil {
		return false, "", err
	}

	var supported bool
	var reason string

	switch format {
	case FormatELF:
		// ELF files on Linux/Unix typically support DWARF
		supported = true
		reason = "ELF format supports DWARF debug information"
	case FormatPE:
		// PE files can have DWARF (e.g., from MinGW/GCC) but often use PDB instead
		supported = true
		reason = "PE format may contain DWARF (common with GCC/MinGW builds)"
	case FormatMachO:
		// Mach-O files on macOS support DWARF
		supported = true
		reason = "Mach-O format supports DWARF debug information"
	default:
		supported = false
		reason = fmt.Sprintf("Unknown executable format: %v", format)
	}

	if supported {
		switch runtime.GOOS {
		case "windows":
			if format != FormatPE {
				reason += " (Warning: Non-PE format on Windows is unusual)"
			}
		case "darwin":
			if format != FormatMachO {
				reason += " (Warning: Non-Mach-O format on macOS is unusual)"
			}
		case "linux", "freebsd", "netbsd", "openbsd":
			if format != FormatELF {
				reason += " (Warning: Non-ELF format on Unix-like OS is unusual)"
			}
		}
	}

	return supported, reason, nil
}

// TestDWARFExtraction tests if DWARF extraction works for the current executable
func TestDWARFExtraction() (int, error) {
	format, execPath, err := GetExecutableInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get executable info: %v", err)
	}

	// Create a test resolver
	resolver := &DWARFResolver{
		functionMap: make(map[string][]string),
	}

	if err := resolver.loadDWARFData(); err != nil {
		return 0, fmt.Errorf("DWARF extraction failed (%s format, %s): %v", format, execPath, err)
	}

	if resolver.dwarfData == nil {
		return 0, fmt.Errorf("DWARF data is nil after extraction")
	}

	// Try to read at least one entry to verify the data is valid
	reader := resolver.dwarfData.Reader()
	entry, err := reader.Next()
	if err != nil {
		return 0, fmt.Errorf("failed to read DWARF entries: %v", err)
	}

	if entry == nil {
		return 0, fmt.Errorf("no DWARF entries found")
	}

	return len(resolver.functionMap), nil
}

// DebugDWARFParameters helps debug parameter extraction issues by showing all DWARF parameters
func DebugDWARFParameters(funcName string) (inputParams []string, allParams []string, err error) {
	resolverOnce.Do(initResolver)

	if resolverInitErr != nil {
		return nil, nil, resolverInitErr
	}

	globalResolver.mu.RLock()
	defer globalResolver.mu.RUnlock()

	candidates := generateFunctionKeyCandidates(funcName)

	for _, candidate := range candidates {
		if params, exists := globalResolver.functionMap[candidate]; exists {
			allParams = params
			break
		}
	}

	if len(allParams) == 0 {
		return nil, nil, fmt.Errorf("function %q not found in DWARF data", funcName)
	}

	// Try to identify where input parameters end
	inputEndIndex := len(allParams)
	for i, param := range allParams {
		if strings.HasPrefix(param, "~r") { // return parameters often start with ~r
			inputEndIndex = i
			break
		}
	}

	if inputEndIndex > 0 {
		inputParams = allParams[:inputEndIndex]
	} else {
		inputParams = allParams // Fallback: assume all are input params
	}

	return inputParams, allParams, nil
}

// GetAllDWARFFunctions returns all functions found in DWARF data for debugging
func GetAllDWARFFunctions() map[string][]string {
	resolverOnce.Do(initResolver)

	if resolverInitErr != nil {
		return map[string][]string{}
	}

	globalResolver.mu.RLock()
	defer globalResolver.mu.RUnlock()

	// Return a copy to avoid concurrent access issues
	result := make(map[string][]string)
	for k, v := range globalResolver.functionMap {
		paramsCopy := make([]string, len(v))
		copy(paramsCopy, v)
		result[k] = paramsCopy
	}

	return result
}
