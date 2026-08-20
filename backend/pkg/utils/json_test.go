package utils

import (
	"os"
	"path/filepath"
	"testing"
)

type TestStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestDefaultJSONWriteOptions(t *testing.T) {
	options := DefaultJSONWriteOptions()

	if options.Indent != "  " {
		t.Errorf("Expected indent to be '  ', got %q", options.Indent)
	}
	if options.FileMode != 0644 {
		t.Errorf("Expected file mode to be 0644, got %o", options.FileMode)
	}
	if !options.Atomic {
		t.Error("Expected atomic to be true")
	}
}

func TestWriteJSONFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "json_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testData := TestStruct{
		Name:  "test",
		Value: 123,
	}

	filePath := filepath.Join(tmpDir, "test.json")

	// Test with default options
	options := DefaultJSONWriteOptions()
	err = WriteJSONFile(filePath, testData, options)
	if err != nil {
		t.Errorf("WriteJSONFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("JSON file was not created")
	}

	// Read and verify content
	var readData TestStruct
	err = ReadJSONFile(filePath, &readData)
	if err != nil {
		t.Errorf("ReadJSONFile failed: %v", err)
	}

	if readData.Name != testData.Name || readData.Value != testData.Value {
		t.Errorf("Data mismatch. Expected %+v, got %+v", testData, readData)
	}
}

func TestWriteJSONFileDefault(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "json_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testData := TestStruct{
		Name:  "default_test",
		Value: 456,
	}

	filePath := filepath.Join(tmpDir, "default_test.json")

	// Test with default function
	err = WriteJSONFileDefault(filePath, testData)
	if err != nil {
		t.Errorf("WriteJSONFileDefault failed: %v", err)
	}

	// Verify by reading back
	var readData TestStruct
	err = ReadJSONFile(filePath, &readData)
	if err != nil {
		t.Errorf("ReadJSONFile failed: %v", err)
	}

	if readData.Name != testData.Name || readData.Value != testData.Value {
		t.Errorf("Data mismatch. Expected %+v, got %+v", testData, readData)
	}
}

func TestWriteJSONFileNonAtomic(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "json_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testData := TestStruct{
		Name:  "non_atomic",
		Value: 789,
	}

	filePath := filepath.Join(tmpDir, "non_atomic.json")

	// Test with non-atomic option
	options := JSONWriteOptions{
		Indent:   "",
		FileMode: 0644,
		Atomic:   false,
	}
	err = WriteJSONFile(filePath, testData, options)
	if err != nil {
		t.Errorf("WriteJSONFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("JSON file was not created")
	}
}

func TestReadJSONFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "json_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Test reading non-existent file
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.json")
	var data TestStruct
	err = ReadJSONFile(nonExistentPath, &data)
	if err == nil {
		t.Error("Expected error when reading non-existent file")
	}

	// Test reading invalid JSON
	invalidJSONPath := filepath.Join(tmpDir, "invalid.json")
	err = os.WriteFile(invalidJSONPath, []byte("invalid json"), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid JSON file: %v", err)
	}

	err = ReadJSONFile(invalidJSONPath, &data)
	if err == nil {
		t.Error("Expected error when reading invalid JSON")
	}
}

func TestMarshalJSONString(t *testing.T) {
	testData := TestStruct{
		Name:  "marshal_test",
		Value: 999,
	}

	jsonStr, err := MarshalJSONString(testData)
	if err != nil {
		t.Errorf("MarshalJSONString failed: %v", err)
	}

	expectedSubstring := `"name":"marshal_test"`
	if !contains(jsonStr, expectedSubstring) {
		t.Errorf("Expected JSON to contain %q, got %q", expectedSubstring, jsonStr)
	}
}

func TestMarshalJSONIndentString(t *testing.T) {
	testData := TestStruct{
		Name:  "indent_test",
		Value: 888,
	}

	jsonStr, err := MarshalJSONIndentString(testData, "  ")
	if err != nil {
		t.Errorf("MarshalJSONIndentString failed: %v", err)
	}

	// Check that it contains indentation
	if !contains(jsonStr, "  \"name\"") {
		t.Errorf("Expected indented JSON, got %q", jsonStr)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
