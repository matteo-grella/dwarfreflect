// Copyright (c) 2025 Matteo Grella <matteogrella@gmail.com>
// Licensed under the MIT License. See LICENSE file for details.

// Package dwarfreflect extracts real parameter names from Go functions using DWARF debug info,
// enabling semantic function calls and automatic struct generation.
//
// Key features:
//   - Call functions using parameter names instead of positions
//   - Auto-generate structs matching function signatures
//   - Extract actual parameter names from compiled binaries
//   - Context-aware parameter handling
//
// Requirements: Binary must contain DWARF debug info (default for `go build`).
// Panics if debug info is stripped (e.g., with `-ldflags="-w"`).
//
// Example:
//
//	func ExampleFunction(name string, age int) string {
//	    return fmt.Sprintf("%s is %d", name, age)
//	}
//
//	fn, err := dwarfreflect.NewFunction(ExampleFunction)
//	if err != nil {
//	    panic(err)
//	}
//
//	// Call with parameter names
//	result := fn.CallWithMap(map[string]any{
//	    "name": "Alice",
//	    "age": 30,
//	})
//
//	// Generate matching struct
//	params := fn.NewParams() // struct{Name string; Age int}
package dwarfreflect
