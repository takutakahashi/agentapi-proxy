package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "fileio_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	filePath := filepath.Join(tmpDir, "test.txt")
	testData := []byte("Hello, World!")

	// Test successful write
	err = AtomicWriteFile(filePath, testData, 0644)
	if err != nil {
		t.Errorf("AtomicWriteFile failed: %v", err)
	}

	// Verify file exists and has correct content
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("File was not created")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Errorf("Failed to read file: %v", err)
	}

	if string(content) != string(testData) {
		t.Errorf("File content mismatch. Expected %q, got %q", testData, content)
	}

	// Verify temporary file was cleaned up
	tempFile := filePath + ".tmp"
	if _, err := os.Stat(tempFile); !os.IsNotExist(err) {
		t.Error("Temporary file was not cleaned up")
	}
}

func TestAtomicWriteFileWithSubdirectory(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "fileio_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	filePath := filepath.Join(tmpDir, "subdir", "test.txt")
	testData := []byte("Hello, World!")

	// Test write to subdirectory (should create directory)
	err = AtomicWriteFile(filePath, testData, 0644)
	if err != nil {
		t.Errorf("AtomicWriteFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("File was not created")
	}

	// Verify directory was created
	dirPath := filepath.Join(tmpDir, "subdir")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}
}

func TestSafeClose(t *testing.T) {
	// Test with nil file
	SafeClose(nil, "test")

	// Test with actual file
	tmpDir, err := os.MkdirTemp("", "fileio_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	filePath := filepath.Join(tmpDir, "test.txt")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// This should not panic
	SafeClose(file, filePath)
}

func TestEnsureDir(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "fileio_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Test creating new directory
	newDir := filepath.Join(tmpDir, "newdir")
	err = EnsureDir(newDir, 0755)
	if err != nil {
		t.Errorf("EnsureDir failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}

	// Test with existing directory (should not fail)
	err = EnsureDir(newDir, 0755)
	if err != nil {
		t.Errorf("EnsureDir failed on existing directory: %v", err)
	}

	// Test creating nested directories
	nestedDir := filepath.Join(tmpDir, "level1", "level2", "level3")
	err = EnsureDir(nestedDir, 0755)
	if err != nil {
		t.Errorf("EnsureDir failed for nested directories: %v", err)
	}

	// Verify nested directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("Nested directory was not created")
	}
}
