# dwarfreflect

Enhanced reflection for Go using DWARF debug information to extract function parameter names, enabling automatic struct generation and semantic function calls.

**Try it on the Playground:** https://go.dev/play/p/3y68m9Pq2-1

## Features

- **Extract real parameter names** from compiled functions using DWARF debug info
- **No offline code generation**—everything works dynamically at runtime
- **Auto-generate structs** matching function signatures
- **Semantic function calls** using parameter names (via maps)
- **Flexible struct customization** with field naming and tag building
- **Context-aware handling** for functions with `context.Context` parameters
- **Cross-platform support** (Linux/ELF, macOS/Mach-O, Windows/PE)

## Installation

```bash
go get github.com/matteo-grella/dwarfreflect
```

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/matteo-grella/dwarfreflect"
)

func ExampleFunction(name string, age int, active bool) string {
    return fmt.Sprintf("%s (%d years old, active: %v)", name, age, active)
}

func main() {
    // Wrap your function
    fn, err := dwarfreflect.NewFunction(ExampleFunction)
    if err != nil {
        panic(err)
    }
        
    // Method 1: Call with map using parameter names
    result := fn.CallWithMap(map[string]any{
        "name":   "Alice",
        "age":    30,
        "active": true,
    })
    fmt.Println(result[0].String()) // "Alice (30 years old, active: true)"
    
    // Method 2: Generate a struct and populate it
    params := fn.NewParamsPtr()
    // params is &struct { Name string; Age int; Active bool }
    
    // Method 3: Call with individual arguments
    result = fn.Call("Bob", 25, false)
}
```

## Requirements

⚠️ **DWARF debug information must be present in your binary**

This is the default for Go builds, but debug info is stripped when using:
- `-ldflags="-w"` (strips DWARF)
- `-ldflags="-s -w"` (strips symbols + DWARF)
- External stripping tools

The package returns an error if DWARF info is unavailable.

## Core API

### Creating a Function Wrapper

```go
fn, err := dwarfreflect.NewFunction(yourFunc)
if err != nil {
    // handle error
}
```

### Calling Functions

```go
// Traditional positional arguments
results := fn.Call(arg1, arg2, arg3)

// Semantic calls with parameter names
results := fn.CallWithMap(map[string]any{
    "param1": value1,
    "param2": value2,
})

// Using generated structs
params := fn.NewParamsPtr()
// ... populate params ...
results := fn.CallWithStruct(params)
```

### Struct Generation

```go
// Get struct type matching function parameters
structType := fn.GetStructType()

// Create instances
params := fn.NewParams()    // returns interface{} containing struct value
params := fn.NewParamsPtr()  // returns interface{} containing *struct

// Customize struct generation
params := fn.NewParams(dwarfreflect.StructOptions{
    FieldNamer: func(paramName string) string {
        return "My" + strings.Title(paramName)
    },
    TagBuilder: func(paramName string, paramType reflect.Type) string {
        return fmt.Sprintf(`json:"%s" validate:"required"`, paramName)
    },
})
```

### Context Handling

```go
func MyHandler(ctx context.Context, userID int, action string) error {
    // ...
}

fn, err := dwarfreflect.NewFunction(MyHandler)
if err != nil {
    panic(err)
}

// Automatically inject context
results := fn.CallWithContext(ctx, 123, "update")

// Get non-context parameters only
nonCtxParams := fn.NewNonContextParams()
// Creates struct { UserID int; Action string } without Context field
```

## Advanced Features

### Parameter Inspection

```go
names, types := fn.GetParameterInfo()
// names: ["name", "age", "active"]
// types: [reflect.TypeOf(""), reflect.TypeOf(0), reflect.TypeOf(false)]

positions := fn.GetContextPositions() // [0] if first param is context
```

### Return Type Analysis

```go
returnTypes, hasError := fn.GetReturnInfo()
// Detects if last return value implements error interface
```

### Method Support

```go
// Method values (receiver bound)
obj := &MyType{}
fn, err := dwarfreflect.NewFunction(obj.Method)
if err != nil {
    panic(err)
}

// Method expressions (receiver unbound)
fn, err := dwarfreflect.NewFunction((*MyType).Method)
if err != nil {
    panic(err)
}
```

## Debugging

Check DWARF availability:

```go
available, funcCount, err := dwarfreflect.GetDWARFStatus()
format, execPath, err := dwarfreflect.GetExecutableInfo()
```

## Limitations

- Requires DWARF debug information in the binary
- Parameter names must be preserved (avoid `-ldflags="-w"`)
- Slight performance overhead for initial function analysis
- Not suitable for obfuscated or stripped binaries

## License

MIT License - see LICENSE file for details