package cwl

import (
	"testing"
)

func TestParseType_Simple(t *testing.T) {
	testCases := []struct {
		input    interface{}
		expected string
		nullable bool
	}{
		{"string", "string", false},
		{"int", "int", false},
		{"File", "File", false},
		{"Directory", "Directory", false},
		{"boolean", "boolean", false},
		{"null", "null", false},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result, err := ParseType(tc.input)
			if err != nil {
				t.Fatalf("Failed to parse type %v: %v", tc.input, err)
			}
			if result.Type != tc.expected {
				t.Errorf("Expected type %s, got %s", tc.expected, result.Type)
			}
			if result.Nullable != tc.nullable {
				t.Errorf("Expected nullable %v, got %v", tc.nullable, result.Nullable)
			}
		})
	}
}

func TestParseType_NullableShorthand(t *testing.T) {
	// Test the ? shorthand for nullable types
	result, err := ParseType("File?")
	if err != nil {
		t.Fatalf("Failed to parse File?: %v", err)
	}
	if result.Type != "File" {
		t.Errorf("Expected type File, got %s", result.Type)
	}
	if !result.Nullable {
		t.Error("Expected nullable=true for File?")
	}

	result, err = ParseType("string?")
	if err != nil {
		t.Fatalf("Failed to parse string?: %v", err)
	}
	if result.Type != "string" {
		t.Errorf("Expected type string, got %s", result.Type)
	}
	if !result.Nullable {
		t.Error("Expected nullable=true for string?")
	}
}

func TestParseType_ArrayShorthand(t *testing.T) {
	// Test the [] shorthand for array types
	result, err := ParseType("File[]")
	if err != nil {
		t.Fatalf("Failed to parse File[]: %v", err)
	}
	if result.Type != TypeArray {
		t.Errorf("Expected type array, got %s", result.Type)
	}
	if result.Items == nil || result.Items.Type != "File" {
		t.Error("Expected items type File")
	}
}

func TestParseType_UnionWithNull(t *testing.T) {
	// Test union type with null (["null", "File"])
	input := []interface{}{"null", "File"}
	result, err := ParseType(input)
	if err != nil {
		t.Fatalf("Failed to parse union type: %v", err)
	}
	if result.Type != "File" {
		t.Errorf("Expected base type File, got %s", result.Type)
	}
	if !result.Nullable {
		t.Error("Expected nullable=true for union with null")
	}
}

func TestParseType_ArrayMap(t *testing.T) {
	// Test map-style array definition
	input := map[string]interface{}{
		"type":  "array",
		"items": "string",
	}

	result, err := ParseType(input)
	if err != nil {
		t.Fatalf("Failed to parse array map: %v", err)
	}
	if result.Type != TypeArray {
		t.Errorf("Expected type array, got %s", result.Type)
	}
	if result.Items == nil || result.Items.Type != "string" {
		t.Error("Expected items type string")
	}
}

func TestParseType_RecordType(t *testing.T) {
	input := map[string]interface{}{
		"type": "record",
		"name": "MyRecord",
		"fields": []interface{}{
			map[string]interface{}{
				"name": "field1",
				"type": "string",
			},
			map[string]interface{}{
				"name": "field2",
				"type": "int",
			},
		},
	}

	result, err := ParseType(input)
	if err != nil {
		t.Fatalf("Failed to parse record type: %v", err)
	}
	if result.Type != TypeRecord {
		t.Errorf("Expected type record, got %s", result.Type)
	}
	if result.Name != "MyRecord" {
		t.Errorf("Expected name MyRecord, got %s", result.Name)
	}
	if len(result.Fields) != 2 {
		t.Errorf("Expected 2 fields, got %d", len(result.Fields))
	}
}

func TestParseType_EnumType(t *testing.T) {
	input := map[string]interface{}{
		"type":    "enum",
		"name":    "FileFormat",
		"symbols": []interface{}{"pdb", "mmcif"},
	}

	result, err := ParseType(input)
	if err != nil {
		t.Fatalf("Failed to parse enum type: %v", err)
	}
	if result.Type != TypeEnum {
		t.Errorf("Expected type enum, got %s", result.Type)
	}
	if len(result.Symbols) != 2 {
		t.Errorf("Expected 2 symbols, got %d", len(result.Symbols))
	}
	if result.Symbols[0] != "pdb" || result.Symbols[1] != "mmcif" {
		t.Errorf("Expected symbols [pdb, mmcif], got %v", result.Symbols)
	}
}

func TestCWLType_String(t *testing.T) {
	testCases := []struct {
		cwlType  *CWLType
		expected string
	}{
		{&CWLType{Type: "string"}, "string"},
		{&CWLType{Type: "File", Nullable: true}, "File?"},
		{&CWLType{Type: TypeArray, Items: &CWLType{Type: "string"}}, "string[]"},
		{&CWLType{Type: TypeArray, Items: &CWLType{Type: "File"}, Nullable: true}, "File[]?"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := tc.cwlType.String()
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestCWLType_IsOptional(t *testing.T) {
	testCases := []struct {
		cwlType  *CWLType
		expected bool
	}{
		{&CWLType{Type: "string"}, false},
		{&CWLType{Type: "string", Nullable: true}, true},
		{&CWLType{Type: "null"}, true},
	}

	for _, tc := range testCases {
		result := tc.cwlType.IsOptional()
		if result != tc.expected {
			t.Errorf("Expected IsOptional=%v for type %v, got %v", tc.expected, tc.cwlType, result)
		}
	}
}

func TestCWLType_BaseType(t *testing.T) {
	testCases := []struct {
		cwlType  *CWLType
		expected string
	}{
		{&CWLType{Type: "string"}, "string"},
		{&CWLType{Type: "string", Nullable: true}, "string"},
		{&CWLType{Type: TypeArray, Items: &CWLType{Type: "File"}}, TypeArray},
	}

	for _, tc := range testCases {
		result := tc.cwlType.BaseType()
		if result != tc.expected {
			t.Errorf("Expected BaseType=%s for type %v, got %s", tc.expected, tc.cwlType, result)
		}
	}
}
