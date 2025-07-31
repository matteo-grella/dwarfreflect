// Copyright (c) 2025 Matteo Grella <matteogrella@gmail.com>
// Licensed under the MIT License. See LICENSE file for details.

package dwarfreflect

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustNewFunction(t *testing.T, fn any) *Function {
	t.Helper()
	f, err := NewFunction(fn)
	if err != nil {
		if strings.Contains(err.Error(), "DWARF") {
			t.Skipf("DWARF not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	return f
}

// Test functions with various signatures
func testFunc1(name string, age int) string {
	return fmt.Sprintf("%s is %d years old", name, age)
}

func testFunc2(x, y float64) float64 {
	return x + y
}

func testFunc3() string {
	return "no params"
}

func testFunc4(ctx context.Context, id int, name string) (string, error) {
	if id < 0 {
		return "", fmt.Errorf("invalid id")
	}
	return fmt.Sprintf("id=%d, name=%s", id, name), nil
}

func testFunc5(name string, active bool, scores []int) map[string]interface{} {
	return map[string]interface{}{
		"name":   name,
		"active": active,
		"scores": scores,
	}
}

func testFunc6(ctx1 context.Context, data string, ctx2 context.Context) string {
	return data
}

type testStruct struct {
	Value string
}

func (t *testStruct) Method(prefix string, num int) string {
	return fmt.Sprintf("%s-%s-%d", prefix, t.Value, num)
}

func TestNewFunction(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)

	if fn.functionType.NumIn() != 2 {
		t.Errorf("expected 2 parameters, got %d", fn.functionType.NumIn())
	}

	if len(fn.paramNames) != 2 {
		t.Errorf("expected 2 parameter names, got %d", len(fn.paramNames))
	}

	if fn.funcName == "" {
		t.Error("function name should not be empty")
	}
}

func TestNewFunction_Error(t *testing.T) {
	if _, err := NewFunction("not a function"); err == nil {
		t.Error("expected error for non-function input")
	}
}

func TestNewParams(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	params := fn.NewParams()

	rv := reflect.ValueOf(params)
	if rv.Kind() != reflect.Struct {
		t.Errorf("expected struct, got %v", rv.Kind())
	}

	if rv.NumField() != 2 {
		t.Errorf("expected 2 fields, got %d", rv.NumField())
	}
}

func TestNewParamsPtr(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	params := fn.NewParamsPtr()

	rv := reflect.ValueOf(params)
	if rv.Kind() != reflect.Ptr {
		t.Errorf("expected ptr, got %v", rv.Kind())
	}

	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		t.Errorf("expected struct element, got %v", elem.Kind())
	}
}

func TestNewParamsWithOptions(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)

	opts := StructOptions{
		FieldNamer: func(name string) string {
			return "Custom" + capitalizeFirst(name)
		},
		TagBuilder: func(name string, typ reflect.Type) string {
			return fmt.Sprintf(`custom:"%s"`, name)
		},
	}

	params := fn.NewParams(opts)
	rv := reflect.ValueOf(params)
	rt := rv.Type()

	// Check custom field names
	field0 := rt.Field(0)
	if field0.Name != "CustomName" {
		t.Errorf("expected CustomName, got %s", field0.Name)
	}

	// Check custom tags
	if !strings.Contains(string(field0.Tag), `custom:"name"`) {
		t.Errorf("expected custom tag, got %s", field0.Tag)
	}
}

func TestCall(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	results, err := fn.Call("Alice", 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].String() != "Alice is 30 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCall_WrongArgCount(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	if _, err := fn.Call("Alice"); err == nil {
		t.Error("expected error for wrong arg count")
	}
}

func TestCall_WrongArgType(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	if _, err := fn.Call("Alice", "not an int"); err == nil {
		t.Error("expected error for wrong arg type")
	}
}

func TestCallWithReflect(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	args := []reflect.Value{
		reflect.ValueOf("Bob"),
		reflect.ValueOf(25),
	}
	results, err := fn.CallWithReflect(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Bob is 25 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithStruct(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	params := fn.NewParamsPtr()

	// Set values using reflection
	rv := reflect.ValueOf(params).Elem()
	rv.FieldByName("Name").SetString("Charlie")
	rv.FieldByName("Age").SetInt(35)

	results, err := fn.CallWithStruct(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Charlie is 35 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithStruct_UsingNewParams(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)

	// When using NewParams, we need to handle the interface{} properly
	paramsIface := fn.NewParams()

	// Create a pointer to a copy of the struct to modify it
	rv := reflect.ValueOf(paramsIface)
	paramsPtr := reflect.New(rv.Type())
	paramsPtr.Elem().Set(rv)

	// Now we can set fields
	paramsPtr.Elem().FieldByName("Name").SetString("Charlie")
	paramsPtr.Elem().FieldByName("Age").SetInt(35)

	// Pass the modified struct value (not the pointer)
	results, err := fn.CallWithStruct(paramsPtr.Elem().Interface())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Charlie is 35 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithStruct_Pointer(t *testing.T) {
	// This test demonstrates that CallWithStruct handles both pointer and value types
	fn := mustNewFunction(t, testFunc1)
	params := fn.NewParamsPtr()

	rv := reflect.ValueOf(params).Elem()
	rv.FieldByName("Name").SetString("Diana")
	rv.FieldByName("Age").SetInt(28)

	// CallWithStruct accepts both pointer and value
	results, err := fn.CallWithStruct(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Diana is 28 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithStruct_TypeMismatch(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	wrongStruct := struct{ X int }{X: 42}
	if _, err := fn.CallWithStruct(wrongStruct); err == nil {
		t.Error("expected error for struct type mismatch")
	}
}

func TestCallWithMap(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	results, err := fn.CallWithMap(map[string]any{
		"name": "Eve",
		"age":  40,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Eve is 40 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithMap_MissingParam(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	if _, err := fn.CallWithMap(map[string]any{
		"name": "Frank",
		// missing "age"
	}); err == nil {
		t.Error("expected error for missing parameter")
	}
}

func TestCallWithMap_WrongType(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	if _, err := fn.CallWithMap(map[string]any{
		"name": "Grace",
		"age":  "not an int",
	}); err == nil {
		t.Error("expected error for wrong parameter type")
	}
}

func TestMapToArgs(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	args, err := fn.MapToArgs(map[string]any{
		"name": "Henry",
		"age":  45,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}

	if args[0].(string) != "Henry" {
		t.Errorf("expected Henry, got %v", args[0])
	}

	if args[1].(int) != 45 {
		t.Errorf("expected 45, got %v", args[1])
	}
}

func TestCallWithContext(t *testing.T) {
	fn := mustNewFunction(t, testFunc4)
	ctx := context.Background()
	results, err := fn.CallWithContext(ctx, 123, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].String() != "id=123, name=test" {
		t.Errorf("unexpected result: %s", results[0].String())
	}

	if !results[1].IsNil() {
		t.Error("expected nil error")
	}
}

func TestCallWithContext_NoContextParams(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	ctx := context.Background()
	results, err := fn.CallWithContext(ctx, "Ivy", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "Ivy is 50 years old" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestCallWithNonContextStructAndContext(t *testing.T) {
	fn := mustNewFunction(t, testFunc4)
	params := fn.NewNonContextParamsPtr()

	rv := reflect.ValueOf(params).Elem()
	rv.FieldByName("Id").SetInt(456)
	rv.FieldByName("Name").SetString("test2")

	ctx := context.Background()
	results, err := fn.CallWithNonContextStructAndContext(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].String() != "id=456, name=test2" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestGetContextPositions(t *testing.T) {
	tests := []struct {
		name     string
		fn       interface{}
		expected []int
	}{
		{"no context", testFunc1, []int{}},
		{"one context", testFunc4, []int{0}},
		{"multiple contexts", testFunc6, []int{0, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := mustNewFunction(t, tt.fn)
			positions := fn.GetContextPositions()

			if len(positions) != len(tt.expected) {
				t.Errorf("expected %d positions, got %d", len(tt.expected), len(positions))
				return
			}

			for i, pos := range positions {
				if pos != tt.expected[i] {
					t.Errorf("position %d: expected %d, got %d", i, tt.expected[i], pos)
				}
			}
		})
	}
}

func TestGetNonContextParameters(t *testing.T) {
	fn := mustNewFunction(t, testFunc4)
	names, types := fn.GetNonContextParameters()

	if len(names) != 2 {
		t.Errorf("expected 2 non-context params, got %d", len(names))
	}

	if names[0] != "id" || names[1] != "name" {
		t.Errorf("unexpected parameter names: %v", names)
	}

	if types[0].Kind() != reflect.Int {
		t.Errorf("expected int type for id, got %v", types[0])
	}

	if types[1].Kind() != reflect.String {
		t.Errorf("expected string type for name, got %v", types[1])
	}
}

func TestGetReturnInfo(t *testing.T) {
	tests := []struct {
		name        string
		fn          interface{}
		returnCount int
		hasError    bool
	}{
		{"single return", testFunc1, 1, false},
		{"no return", testFunc3, 1, false},
		{"with error", testFunc4, 2, true},
		{"complex return", testFunc5, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := mustNewFunction(t, tt.fn)
			types, hasError := fn.GetReturnInfo()

			if len(types) != tt.returnCount {
				t.Errorf("expected %d return types, got %d", tt.returnCount, len(types))
			}

			if hasError != tt.hasError {
				t.Errorf("expected hasError=%v, got %v", tt.hasError, hasError)
			}
		})
	}
}

func TestGetBaseFunctionName(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	baseName := fn.GetBaseFunctionName()

	if baseName != "testFunc1" {
		t.Errorf("expected testFunc1, got %s", baseName)
	}
}

func TestMethodFunction(t *testing.T) {
	ts := &testStruct{Value: "test"}
	fn := mustNewFunction(t, ts.Method)

	// Method value has receiver already bound, so only 2 explicit parameters
	if fn.functionType.NumIn() != 2 {
		t.Errorf("expected 2 parameters for method value, got %d", fn.functionType.NumIn())
	}

	// Call the method - receiver is already bound in ts.Method
	results, err := fn.Call("prefix", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].String() != "prefix-test-42" {
		t.Errorf("unexpected method result: %s", results[0].String())
	}
}

func TestUnboundMethodFunction(t *testing.T) {
	// Test with unbound method (method expression)
	fn := mustNewFunction(t, (*testStruct).Method)

	// Unbound method includes receiver as first parameter
	if fn.functionType.NumIn() != 3 {
		t.Errorf("expected 3 parameters for unbound method, got %d", fn.functionType.NumIn())
	}

	// Call the unbound method - need to pass receiver as first argument
	ts := &testStruct{Value: "test"}
	results, err := fn.Call(ts, "prefix", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].String() != "prefix-test-42" {
		t.Errorf("unexpected method result: %s", results[0].String())
	}
}

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"a", "A"},
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"àllo", "Àllo"},
	}

	for _, tt := range tests {
		result := capitalizeFirst(tt.input)
		if result != tt.expected {
			t.Errorf("capitalizeFirst(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestStructTypesCompatible(t *testing.T) {
	type struct1 struct {
		Name string
		Age  int
	}

	type struct2 struct {
		Name string
		Age  int
	}

	type struct3 struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	type struct4 struct {
		Name  string
		Age   int
		Extra bool
	}

	tests := []struct {
		name     string
		t1       reflect.Type
		t2       reflect.Type
		expected bool
	}{
		{
			"same struct",
			reflect.TypeOf(struct1{}),
			reflect.TypeOf(struct1{}),
			true,
		},
		{
			"compatible structs",
			reflect.TypeOf(struct1{}),
			reflect.TypeOf(struct2{}),
			true,
		},
		{
			"different tags ok",
			reflect.TypeOf(struct1{}),
			reflect.TypeOf(struct3{}),
			true,
		},
		{
			"different fields",
			reflect.TypeOf(struct1{}),
			reflect.TypeOf(struct4{}),
			false,
		},
		{
			"not structs",
			reflect.TypeOf("string"),
			reflect.TypeOf(123),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := structTypesCompatible(tt.t1, tt.t2)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComplexTypes(t *testing.T) {
	fn := mustNewFunction(t, testFunc5)

	results, err := fn.Call("test", true, []int{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	resultMap := results[0].Interface().(map[string]interface{})
	if resultMap["name"] != "test" {
		t.Errorf("expected name=test, got %v", resultMap["name"])
	}

	if resultMap["active"] != true {
		t.Errorf("expected active=true, got %v", resultMap["active"])
	}

	scores := resultMap["scores"].([]int)
	if len(scores) != 3 || scores[0] != 1 {
		t.Errorf("unexpected scores: %v", scores)
	}
}

func TestNoParamsFunction(t *testing.T) {
	fn := mustNewFunction(t, testFunc3)

	if len(fn.paramNames) != 0 {
		t.Errorf("expected 0 parameters, got %d", len(fn.paramNames))
	}

	results, err := fn.Call()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].String() != "no params" {
		t.Errorf("unexpected result: %s", results[0].String())
	}
}

func TestGetPackagePath(t *testing.T) {
	fn := mustNewFunction(t, testFunc1)
	pkgPath := fn.GetPackagePath()

	// Should contain "dwarfreflect" since that's our package
	if !strings.Contains(pkgPath, "dwarfreflect") {
		t.Errorf("expected package path to contain 'dwarfreflect', got %s", pkgPath)
	}
}

// mustNewFunctionB mirrors mustNewFunction but works with testing.B to
// simplify benchmarks.
func mustNewFunctionB(b *testing.B, fn any) *Function {
	b.Helper()
	f, err := NewFunction(fn)
	if err != nil {
		if strings.Contains(err.Error(), "DWARF") {
			b.Skipf("DWARF not available: %v", err)
		}
		b.Fatalf("unexpected error: %v", err)
	}
	return f
}

// Benchmark to measure the overhead of using Function.Call compared to a direct call.
func BenchmarkFunctionCall(b *testing.B) {
	fn := mustNewFunctionB(b, testFunc1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.Call("Alice", 30); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark for calling the wrapped function using a parameter map.
func BenchmarkFunctionCallWithMap(b *testing.B) {
	fn := mustNewFunctionB(b, testFunc1)
	args := map[string]any{"name": "Alice", "age": 30}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.CallWithMap(args); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark for calling the wrapped function using a generated struct.
func BenchmarkFunctionCallWithStruct(b *testing.B) {
	fn := mustNewFunctionB(b, testFunc1)
	params := fn.NewParamsPtr()
	rv := reflect.ValueOf(params).Elem()
	rv.FieldByName("Name").SetString("Alice")
	rv.FieldByName("Age").SetInt(30)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.CallWithStruct(params); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark for the baseline direct call without reflection.
func BenchmarkDirectCall(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = testFunc1("Alice", 30)
	}
}
