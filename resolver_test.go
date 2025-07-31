// Copyright (c) 2025 Matteo Grella <matteogrella@gmail.com>
// Licensed under the MIT License. See LICENSE file for details.

package dwarfreflect

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableFormat_String(t *testing.T) {
	tests := []struct {
		format   ExecutableFormat
		expected string
	}{
		{FormatELF, "ELF"},
		{FormatPE, "PE"},
		{FormatMachO, "Mach-O"},
		{FormatUnknown, "Unknown"},
		{ExecutableFormat(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.format.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetectExecutableFormat(t *testing.T) {
	// Test with current executable
	execPath, err := os.Executable()
	if err != nil {
		t.Skipf("Cannot get executable path: %v", err)
	}

	format, err := DetectExecutableFormat(execPath)
	if err != nil {
		t.Fatalf("DetectExecutableFormat failed: %v", err)
	}

	// Verify format matches expected for current OS
	switch runtime.GOOS {
	case "linux", "freebsd", "netbsd", "openbsd":
		if format != FormatELF {
			t.Errorf("Expected ELF format on %s, got %v", runtime.GOOS, format)
		}
	case "windows":
		if format != FormatPE {
			t.Errorf("Expected PE format on Windows, got %v", format)
		}
	case "darwin":
		if format != FormatMachO {
			t.Errorf("Expected Mach-O format on macOS, got %v", format)
		}
	}

	// Test with non-existent file
	_, err = DetectExecutableFormat("/non/existent/file")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test with invalid file (create temporary text file)
	tmpFile, err := os.CreateTemp("", "test*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("not an executable")
	tmpFile.Close()

	format, err = DetectExecutableFormat(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for non-executable file")
	}
	if format != FormatUnknown {
		t.Errorf("Expected FormatUnknown for text file, got %v", format)
	}
}

func TestGenerateFunctionKeyCandidates(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:  "simple function",
			input: "main.funcName",
			expected: []string{
				"main.funcName",
			},
		},
		{
			name:  "package function",
			input: "github.com/user/repo/pkg.funcName",
			expected: []string{
				"github.com/user/repo/pkg.funcName",
				"pkg.funcName",
			},
		},
		{
			name:  "method with pointer receiver",
			input: "github.com/user/repo/pkg.(*Type).Method",
			expected: []string{
				"github.com/user/repo/pkg.(*Type).Method",
				"pkg.(*Type).Method",
				"github.com/user/repo/pkg.(*Type).Method",
			},
		},
		{
			name:  "method with value receiver",
			input: "pkg.Type.Method",
			expected: []string{
				"pkg.Type.Method",
			},
		},
		{
			name:  "deeply nested package",
			input: "github.com/org/project/internal/sub/pkg.Function",
			expected: []string{
				"github.com/org/project/internal/sub/pkg.Function",
				"pkg.Function",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateFunctionKeyCandidates(tt.input)

			// Check if all expected candidates are present
			for _, exp := range tt.expected {
				found := false
				for _, candidate := range got {
					if candidate == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected candidate %q not found in %v", exp, got)
				}
			}
		})
	}
}

func TestExtractPackagePath(t *testing.T) {
	tests := []struct {
		funcName string
		expected string
	}{
		{"main.funcName", "main"},
		{"pkg.funcName", "pkg"},
		{"github.com/user/repo/pkg.funcName", "github.com/user/repo/pkg"},
		{"github.com/user/repo/pkg.(*Type).Method", "github.com/user/repo/pkg"},
		{"github.com/user/repo/pkg.Type.Method", "github.com/user/repo/pkg"},
		{"funcNameOnly", "main"},
		{"github.com/org/project/internal/sub/pkg.Function", "github.com/org/project/internal/sub/pkg"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			got := extractPackagePath(tt.funcName)
			if got != tt.expected {
				t.Errorf("extractPackagePath(%q) = %q, want %q", tt.funcName, got, tt.expected)
			}
		})
	}
}

func TestDWARFResolver_extractParametersFromDWARF(t *testing.T) {
	// This test would require mocking dwarf.Reader, which is complex
	// Instead, we'll test the integration with a real function

	// Define a test function with known parameters
	testFunc := func(name string, age int, active bool) (string, error) {
		return fmt.Sprintf("%s:%d:%v", name, age, active), nil
	}

	// Initialize the resolver
	initResolver()
	if resolverInitErr != nil {
		t.Skipf("DWARF not available: %v", resolverInitErr)
	}

	// Try to get parameter names for this function
	fnValue := reflect.ValueOf(testFunc)
	pc := fnValue.Pointer()
	runtimeFunc := runtime.FuncForPC(pc)
	funcName := runtimeFunc.Name()

	paramNames, err := globalResolver.discoverParameterNames(funcName, 3)
	if err != nil {
		t.Skipf("DWARF not available: %v", err)
	}

	// If we get here, DWARF was available
	if len(paramNames) != 3 {
		t.Errorf("Expected 3 parameters, got %d: %v", len(paramNames), paramNames)
	}
}

func TestGetDWARFStatus(t *testing.T) {
	// This might fail if the test binary doesn't have DWARF info
	available, funcCount, err := GetDWARFStatus()

	if err != nil && !strings.Contains(err.Error(), "DWARF") {
		t.Errorf("Unexpected error: %v", err)
	}

	if available && funcCount == 0 {
		t.Error("DWARF available but no functions found")
	}
}

func TestGetExecutableInfo(t *testing.T) {
	format, path, err := GetExecutableInfo()
	if err != nil {
		t.Fatalf("GetExecutableInfo failed: %v", err)
	}

	if path == "" {
		t.Error("Expected non-empty executable path")
	}

	if format == FormatUnknown {
		t.Error("Expected known executable format")
	}

	// Verify path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Executable path does not exist: %s", path)
	}
}

func TestIsDWARFSupported(t *testing.T) {
	supported, reason, err := IsDWARFSupported()
	if err != nil {
		t.Fatalf("IsDWARFSupported failed: %v", err)
	}

	if reason == "" {
		t.Error("Expected non-empty reason")
	}

	// On all major platforms, DWARF should be supported
	switch runtime.GOOS {
	case "linux", "darwin", "windows", "freebsd", "netbsd", "openbsd":
		if !supported {
			t.Errorf("Expected DWARF to be supported on %s", runtime.GOOS)
		}
	}
}

func TestDebugDWARFParameters(t *testing.T) {
	// Test with a non-existent function
	_, _, err := DebugDWARFParameters("non.existent.Function")
	if err == nil {
		t.Error("Expected error for non-existent function")
	}

	// Test with a real function (if DWARF is available)
	testFunc := func(a string, b int) (result string, err error) {
		return a + fmt.Sprint(b), nil
	}

	fnValue := reflect.ValueOf(testFunc)
	pc := fnValue.Pointer()
	runtimeFunc := runtime.FuncForPC(pc)
	funcName := runtimeFunc.Name()

	inputParams, allParams, err := DebugDWARFParameters(funcName)
	if err != nil {
		t.Skipf("DWARF not available: %v", err)
	}

	// If DWARF is available, verify the results
	if len(inputParams) > len(allParams) {
		t.Error("Input params should be subset of all params")
	}

	// Check for return parameters
	hasReturnParams := false
	for _, param := range allParams {
		if strings.HasPrefix(param, "~r") {
			hasReturnParams = true
			break
		}
	}

	if hasReturnParams && len(inputParams) >= len(allParams) {
		t.Error("Expected input params to be fewer than all params when return params exist")
	}
}

func TestGetAllDWARFFunctions(t *testing.T) {
	functions := GetAllDWARFFunctions()

	// Should return a map (possibly empty if no DWARF)
	if functions == nil {
		t.Error("Expected non-nil map")
	}

	// If DWARF is available, there should be some functions
	if len(functions) > 0 {
		// Verify we got parameter arrays
		for funcName, params := range functions {
			if funcName == "" {
				t.Error("Found empty function name")
			}
			// params can be empty array
			if params == nil {
				t.Errorf("Function %s has nil params", funcName)
			}
		}
	}
}

func TestDWARFResolver_loadDWARFData(t *testing.T) {
	resolver := &DWARFResolver{
		functionMap: make(map[string][]string),
	}

	err := resolver.loadDWARFData()

	// This might fail if test binary has no DWARF
	if err != nil {
		if !strings.Contains(err.Error(), "DWARF") && !strings.Contains(err.Error(), "debug") {
			t.Errorf("Unexpected error: %v", err)
		}
		return
	}

	// If successful, verify we got valid data
	if resolver.dwarfData == nil {
		t.Error("Expected non-nil DWARF data after successful load")
	}

	if resolver.executablePath == "" {
		t.Error("Expected executable path to be set")
	}
}

func TestTestDWARFExtraction(t *testing.T) {
	funcCount, err := TestDWARFExtraction()

	// This might fail if test binary doesn't have DWARF info
	if err != nil {
		// Check for expected error messages
		errStr := err.Error()
		if !strings.Contains(errStr, "DWARF") &&
			!strings.Contains(errStr, "debug") &&
			!strings.Contains(errStr, "format") {
			t.Errorf("Unexpected error: %v", err)
		}
		return
	}

	// If successful, we should have found some functions
	if funcCount == 0 {
		t.Log("Warning: DWARF extraction succeeded but found 0 functions")
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Test concurrent access to resolver methods
	const goroutines = 10

	// Initialize resolver
	initResolver()
	if resolverInitErr != nil {
		t.Skipf("DWARF not available: %v", resolverInitErr)
	}

	done := make(chan bool, goroutines)

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- true }()

			// These should be safe to call concurrently
			GetAllDWARFFunctions()
			GetDWARFStatus()
			GetExecutableInfo()
			IsDWARFSupported()
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestResolverInitialization(t *testing.T) {
	// Test that resolver is properly initialized
	// Note: We can't test the sync.Once behavior directly because
	// other tests may have already initialized it

	// Ensure resolver is initialized
	initResolver()
	if resolverInitErr != nil {
		t.Skipf("DWARF not available: %v", resolverInitErr)
	}

	if globalResolver == nil {
		t.Fatal("globalResolver should be initialized")
	}

	// Verify resolver has required fields
	if globalResolver.functionMap == nil {
		t.Error("functionMap should be initialized")
	}

	// Test that multiple calls don't panic (sync.Once ensures only first call executes)
	for i := 0; i < 3; i++ {
		initResolver()
	}
}

// Benchmark for function name candidate generation
func BenchmarkGenerateFunctionKeyCandidates(b *testing.B) {
	testCases := []string{
		"main.simpleFunc",
		"github.com/user/repo/pkg.Function",
		"github.com/user/repo/pkg.(*Type).Method",
		"github.com/org/project/internal/sub/pkg.ComplexFunction",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			generateFunctionKeyCandidates(tc)
		}
	}
}

// Benchmark for package path extraction
func BenchmarkExtractPackagePath(b *testing.B) {
	testCases := []string{
		"main.simpleFunc",
		"github.com/user/repo/pkg.Function",
		"github.com/user/repo/pkg.(*Type).Method",
		"github.com/org/project/internal/sub/pkg.ComplexFunction",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			extractPackagePath(tc)
		}
	}
}
