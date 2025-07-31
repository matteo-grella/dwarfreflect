// Copyright (c) 2025 Matteo Grella <matteogrella@gmail.com>
// Licensed under the MIT License. See LICENSE file for details.

package dwarfreflect

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"unicode"
)

// StructOptions customizes struct generation from function parameters.
type StructOptions struct {
	// FieldNamer transforms parameter names to struct field names.
	// Default: capitalizeFirst (makes fields exported).
	FieldNamer func(paramName string) string

	// TagBuilder creates struct tags for each parameter.
	// Receives parameter name and type, returns complete tag string.
	TagBuilder func(paramName string, paramType reflect.Type) string
}

// Function wraps a Go function to enable enhanced reflection capabilities
// including parameter name extraction and struct generation.
type Function struct {
	function     reflect.Value
	functionType reflect.Type
	paramNames   []string
	paramTypes   []reflect.Type
	structType   reflect.Type
	funcName     string
	packagePath  string
}

// NewFunction creates a Function wrapper that extracts parameter names from DWARF debug info.
// It returns an error if the provided value is not a function or if DWARF information
// is unavailable.
//
// Example:
//
//	func MyFunc(name string, age int) string { return "" }
//	fn := dwarfreflect.NewFunction(MyFunc)
func NewFunction(fn any) (*Function, error) {
	resolverOnce.Do(initResolver)
	if resolverInitErr != nil {
		return nil, resolverInitErr
	}

	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("NewFunction requires a function")
	}

	// Get function runtime information
	pc := fnValue.Pointer()
	runtimeFunc := runtime.FuncForPC(pc)
	funcName := runtimeFunc.Name()
	packagePath := extractPackagePath(funcName)

	paramTypes := make([]reflect.Type, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		paramTypes[i] = fnType.In(i)
	}

	paramNames, err := globalResolver.discoverParameterNames(funcName, len(paramTypes))
	if err != nil {
		return nil, err
	}

	structType := createStructType(paramNames, paramTypes)

	return &Function{
		function:     fnValue,
		functionType: fnType,
		paramNames:   paramNames,
		paramTypes:   paramTypes,
		structType:   structType,
		funcName:     funcName,
		packagePath:  packagePath,
	}, nil
}

// NewParams creates a struct instance matching all function parameters.
// Returns interface{} containing the struct value.
//
// Example:
//
//	params := fn.NewParams() // struct{Name string; Age int}
func (t *Function) NewParams(opts ...StructOptions) interface{} {
	var structType reflect.Type
	if len(opts) > 0 {
		structType = t.GetStructTypeWithOptions(opts[0])
	} else {
		structType = t.structType
	}
	return reflect.New(structType).Elem().Interface()
}

// NewParamsPtr creates a pointer to a struct matching all function parameters.
// Returns interface{} containing *struct.
//
// Example:
//
//	params := fn.NewParamsPtr() // &struct{Name string; Age int}
func (t *Function) NewParamsPtr(opts ...StructOptions) interface{} {
	var structType reflect.Type
	if len(opts) > 0 {
		structType = t.GetStructTypeWithOptions(opts[0])
	} else {
		structType = t.structType
	}
	return reflect.New(structType).Interface()
}

// NewNonContextParams creates a struct instance excluding context.Context parameters.
// Useful for JSON unmarshaling or form binding where context doesn't belong.
//
// Example:
//
//	func Handler(ctx context.Context, userID int) {}
//	params := fn.NewNonContextParams() // struct{UserID int} (no Context field)
func (t *Function) NewNonContextParams(opts ...StructOptions) interface{} {
	var structType reflect.Type
	if len(opts) > 0 {
		structType = t.GetNonContextStructTypeWithOptions(opts[0])
	} else {
		structType = t.GetNonContextStructType()
	}
	return reflect.New(structType).Elem().Interface()
}

// NewNonContextParamsPtr creates a pointer to struct excluding context.Context parameters.
// Returns interface{} containing *struct.
func (t *Function) NewNonContextParamsPtr(opts ...StructOptions) interface{} {
	var structType reflect.Type
	if len(opts) > 0 {
		structType = t.GetNonContextStructTypeWithOptions(opts[0])
	} else {
		structType = t.GetNonContextStructType()
	}
	return reflect.New(structType).Interface()
}

// GetStructType returns the reflect.Type for a struct matching all function parameters.
func (t *Function) GetStructType() reflect.Type {
	return t.structType
}

// GetStructTypeWithOptions returns a customized struct type for all function parameters.
func (t *Function) GetStructTypeWithOptions(opts StructOptions) reflect.Type {
	return t.createStructTypeFromParams(t.paramNames, t.paramTypes, opts)
}

// GetNonContextStructType returns a struct type excluding context.Context parameters.
func (t *Function) GetNonContextStructType() reflect.Type {
	paramNames, paramTypes := t.GetNonContextParameters()
	return t.createStructTypeFromParams(paramNames, paramTypes, StructOptions{})
}

// GetNonContextStructTypeWithOptions returns a customized struct type excluding context.Context parameters.
func (t *Function) GetNonContextStructTypeWithOptions(opts StructOptions) reflect.Type {
	paramNames, paramTypes := t.GetNonContextParameters()
	return t.createStructTypeFromParams(paramNames, paramTypes, opts)
}

// createStructType creates an anonymous struct type from parameter info
func createStructType(paramNames []string, paramTypes []reflect.Type) reflect.Type {
	fields := make([]reflect.StructField, len(paramNames))

	for i, name := range paramNames {
		// Capitalize first letter for exported field
		fieldName := capitalizeFirst(name)

		fields[i] = reflect.StructField{
			Name: fieldName,
			Type: paramTypes[i],
			Tag:  reflect.StructTag(fmt.Sprintf(`json:"%s" param:"%s"`, name, name)),
		}
	}

	return reflect.StructOf(fields)
}

func (t *Function) createStructTypeFromParams(paramNames []string, paramTypes []reflect.Type, opts StructOptions) reflect.Type {
	// Set default field namer if not provided
	fieldNamer := opts.FieldNamer
	if fieldNamer == nil {
		fieldNamer = capitalizeFirst
	}

	// Create struct fields
	fields := make([]reflect.StructField, len(paramNames))
	for i, paramName := range paramNames {
		fieldName := fieldNamer(paramName)

		var tag reflect.StructTag
		if opts.TagBuilder != nil {
			tagString := opts.TagBuilder(paramName, paramTypes[i])
			tag = reflect.StructTag(tagString)
		}

		fields[i] = reflect.StructField{
			Name: fieldName,
			Type: paramTypes[i],
			Tag:  tag,
		}
	}

	return reflect.StructOf(fields)
}

// Call invokes the function with individual arguments.
// Arguments must match parameter types and count exactly.
//
// Example:
//
//	results := fn.Call("Alice", 30, true)
func (t *Function) Call(args ...any) ([]reflect.Value, error) {
	if len(args) != len(t.paramTypes) {
		return nil, fmt.Errorf("wrong number of arguments: expected %d, got %d",
			len(t.paramTypes), len(args))
	}

	// Prepare function arguments and populate struct
	callArgs := make([]reflect.Value, len(args))
	for i, arg := range args {
		argValue := reflect.ValueOf(arg)

		// Validate type compatibility
		if !argValue.Type().AssignableTo(t.paramTypes[i]) {
			return nil, fmt.Errorf("argument %d (%s): cannot assign %v to %v",
				i, t.paramNames[i], argValue.Type(), t.paramTypes[i])
		}

		callArgs[i] = argValue
	}

	return t.function.Call(callArgs), nil
}

// CallWithReflect invokes the function with reflect.Value arguments.
// Lower-level version of Call for advanced use cases.
func (t *Function) CallWithReflect(args []reflect.Value) ([]reflect.Value, error) {
	if len(args) != len(t.paramTypes) {
		return nil, fmt.Errorf("wrong number of arguments: expected %d, got %d",
			len(t.paramTypes), len(args))
	}

	// Validate types
	for i, arg := range args {
		if !arg.Type().AssignableTo(t.paramTypes[i]) {
			return nil, fmt.Errorf("argument %d (%s): cannot assign %v to %v",
				i, t.paramNames[i], arg.Type(), t.paramTypes[i])
		}
	}

	return t.function.Call(args), nil
}

// CallWithStruct invokes the function using values from a generated struct.
// The struct must match the type returned by GetStructType().
//
// Example:
//
//	params := fn.NewParamsPtr().(*struct{Name string; Age int})
//	params.Name, params.Age = "Alice", 30
//	results := fn.CallWithStruct(params)
func (t *Function) CallWithStruct(argStruct any) ([]reflect.Value, error) {
	structValue := reflect.ValueOf(argStruct)

	if structValue.Kind() == reflect.Ptr {
		structValue = structValue.Elem()
	}

	if structValue.Type() != t.structType {
		return nil, fmt.Errorf("struct type mismatch: expected %v, got %v",
			t.structType, structValue.Type())
	}

	// Extract values from struct fields
	args := make([]reflect.Value, len(t.paramNames))
	for i, paramName := range t.paramNames {
		fieldName := capitalizeFirst(paramName)
		fieldValue := structValue.FieldByName(fieldName)
		args[i] = fieldValue
	}

	// Call the function
	return t.function.Call(args), nil
}

// CallWithContext invokes the function with automatic context injection.
// Provide non-context arguments only; context.Context parameters are injected automatically.
//
// Example:
//
//	func Handler(ctx context.Context, userID int, action string) {}
//	results := fn.CallWithContext(ctx, 123, "update") // Only provide userID and action
func (t *Function) CallWithContext(ctx context.Context, args ...any) ([]reflect.Value, error) {
	contextPositions := t.GetContextPositions()
	if len(contextPositions) == 0 {
		// No context parameters - just call normally
		return t.Call(args...)
	}

	// Create full argument list with context injected
	fullArgs := make([]any, len(t.paramTypes))
	argIndex := 0

	for i := 0; i < len(t.paramTypes); i++ {
		if slices.Contains(contextPositions, i) {
			fullArgs[i] = ctx
		} else {
			if argIndex >= len(args) {
				return nil, fmt.Errorf("not enough arguments: expected %d non-context args, got %d",
					len(t.paramTypes)-len(contextPositions), len(args))
			}
			fullArgs[i] = args[argIndex]
			argIndex++
		}
	}

	return t.Call(fullArgs...)
}

// CallWithNonContextStructAndContext invokes the function using a non-context struct plus context injection.
// The struct should be created with NewNonContextParams().
//
// Example:
//
//	params := fn.NewNonContextParams() // struct without Context field
//	results := fn.CallWithNonContextStructAndContext(ctx, params)
func (t *Function) CallWithNonContextStructAndContext(ctx context.Context, argStruct any) ([]reflect.Value, error) {
	structValue := reflect.ValueOf(argStruct)
	if structValue.Kind() == reflect.Ptr {
		structValue = structValue.Elem()
	}

	nonContextStructType := t.GetNonContextStructType()
	if !structTypesCompatible(structValue.Type(), nonContextStructType) {
		return nil, fmt.Errorf("struct type mismatch: expected %v, got %v",
			nonContextStructType, structValue.Type())
	}

	// Extract values from non-context struct fields
	nonContextNames, _ := t.GetNonContextParameters()
	args := make([]any, len(nonContextNames))
	for i, paramName := range nonContextNames {
		fieldName := capitalizeFirst(paramName)
		fieldValue := structValue.FieldByName(fieldName)
		args[i] = fieldValue.Interface()
	}

	// Use existing CallWithContext which handles context injection
	return t.CallWithContext(ctx, args...)
}

// CallWithMap invokes the function using a map of parameter names to values.
// Enables semantic function calls using actual parameter names.
// Extra keys in the map are ignored for flexibility.
//
// Example:
//
//	results := fn.CallWithMap(map[string]any{
//	    "name": "Alice",
//	    "age": 30,
//	    "active": true,
//	})
func (t *Function) CallWithMap(argMap map[string]any) ([]reflect.Value, error) {
	args, err := t.MapToArgs(argMap)
	if err != nil {
		return nil, err
	}

	callArgs := make([]reflect.Value, len(args))
	for i, arg := range args {
		callArgs[i] = reflect.ValueOf(arg)
	}

	return t.function.Call(callArgs), nil
}

// MapToArgs converts a parameter map to a []any slice in correct parameter order.
// Used internally by CallWithMap but exposed for advanced use cases.
func (t *Function) MapToArgs(argMap map[string]any) ([]any, error) {
	if len(argMap) != len(t.paramTypes) {
		return nil, fmt.Errorf("wrong number of arguments: expected %d, got %d",
			len(t.paramTypes), len(argMap))
	}

	var missing []string
	for _, paramName := range t.paramNames {
		if _, exists := argMap[paramName]; !exists {
			missing = append(missing, paramName)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"missing required parameters %v (function %s expects %v)",
			missing, t.funcName, t.paramNames,
		)
	}

	// Prepare function arguments in the correct parameter order
	args := make([]any, len(t.paramNames))
	for i, paramName := range t.paramNames {
		argValue := argMap[paramName] // At this point every paramName is in argMap

		// Validate type compatibility
		rv := reflect.ValueOf(argValue)
		if !rv.Type().AssignableTo(t.paramTypes[i]) {
			return nil, fmt.Errorf(
				"parameter %q: cannot assign %v to %v",
				paramName, rv.Type(), t.paramTypes[i],
			)
		}

		args[i] = argMap[paramName]
	}

	return args, nil
}

// GetParameterInfo returns the parameter names and types extracted from the function.
//
// Example:
//
//	names, types := fn.GetParameterInfo()
//	// names: ["name", "age", "active"]
//	// types: [string, int, bool]
func (t *Function) GetParameterInfo() ([]string, []reflect.Type) {
	return t.paramNames, t.paramTypes
}

// GetFunctionName returns the full runtime function name.
//
// Example: "github.com/user/repo/pkg.ProcessUser"
func (t *Function) GetFunctionName() string {
	return t.funcName
}

// GetBaseFunctionName returns just the function name without package path.
//
// Handle different runtime name formats:
//
//	"main.processUser" -> "processUser"
//	"pkg.(*Type).Method" -> "Method"
//	"github.com/user/repo/pkg.funcName" -> "funcName"
func (t *Function) GetBaseFunctionName() string {
	parts := strings.Split(t.funcName, ".")
	if len(parts) > 0 {
		lastName := parts[len(parts)-1]
		lastName = strings.Trim(lastName, "()") // Remove any parentheses for method names
		return lastName
	}
	return t.funcName
}

// GetPackagePath returns the package path where the function is defined.
//
// Example: "github.com/user/repo/pkg"
func (t *Function) GetPackagePath() string {
	return t.packagePath
}

// GetContextPositions returns the parameter indices where context.Context appears.
// Used internally for context injection.
//
// Example: [0, 2] means context is the 1st and 3rd parameter
func (t *Function) GetContextPositions() []int {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	var positions []int

	for i, paramType := range t.paramTypes {
		if paramType == contextType {
			positions = append(positions, i)
		}
	}

	return positions
}

// GetNonContextParameters returns parameter names and types excluding context.Context.
// Used for creating structs without context fields.
func (t *Function) GetNonContextParameters() ([]string, []reflect.Type) {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	var names []string
	var types []reflect.Type

	for i, paramType := range t.paramTypes {
		if paramType != contextType {
			names = append(names, t.paramNames[i])
			types = append(types, paramType)
		}
	}

	return names, types
}

// GetReturnTypes returns the types of all function return values.
func (t *Function) GetReturnTypes() []reflect.Type {
	returnTypes := make([]reflect.Type, t.functionType.NumOut())
	for i := 0; i < t.functionType.NumOut(); i++ {
		returnTypes[i] = t.functionType.Out(i)
	}
	return returnTypes
}

// GetReturnInfo returns return types and whether the last return implements error interface.
// Useful for error handling patterns.
//
// Example:
//
//	types, hasError := fn.GetReturnInfo()
//	// hasError = true if last return type implements error
func (t *Function) GetReturnInfo() ([]reflect.Type, bool) {
	returnTypes := t.GetReturnTypes()

	if len(returnTypes) == 0 {
		return returnTypes, false
	}

	// Check if last return type implements error interface
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	lastIsError := returnTypes[len(returnTypes)-1].Implements(errorType)

	return returnTypes, lastIsError
}

// structTypesCompatible checks if two struct types have the same fields (ignoring tags).
func structTypesCompatible(t1, t2 reflect.Type) bool {
	if t1.Kind() != reflect.Struct || t2.Kind() != reflect.Struct {
		return false
	}

	if t1.NumField() != t2.NumField() {
		return false
	}

	for i := 0; i < t1.NumField(); i++ {
		field1 := t1.Field(i)
		field2 := t2.Field(i)

		if field1.Name != field2.Name || field1.Type != field2.Type {
			return false
		}
	}

	return true
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
