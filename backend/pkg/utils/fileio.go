package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a file atomically using a temporary file
// This prevents corruption if the process is interrupted during writing
func AtomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create temporary file
	tempFile := filePath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file %s: %w", tempFile, err)
	}

	// Write data
	_, writeErr := file.Write(data)

	// Close file
	closeErr := file.Close()

	// Check for write error
	if writeErr != nil {
		// Clean up temporary file on write error
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to write to temporary file %s: %w", tempFile, writeErr)
	}

	// Check for close error
	if closeErr != nil {
		// Clean up temporary file on close error
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to close temporary file %s: %w", tempFile, closeErr)
	}

	// Set file permissions
	if err := os.Chmod(tempFile, perm); err != nil {
		// Clean up temporary file on chmod error
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to set permissions on temporary file %s: %w", tempFile, err)
	}

	// Atomically move temporary file to final location
	if err := os.Rename(tempFile, filePath); err != nil {
		// Clean up temporary file on rename error
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename temporary file %s to %s: %w", tempFile, filePath, err)
	}

	return nil
}

// SafeClose safely closes a file with error logging
func SafeClose(file *os.File, fileName string) {
	if file != nil {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file %s: %v\n", fileName, err)
		}
	}
}

// EnsureDir ensures that a directory exists, creating it if necessary
func EnsureDir(dir string, perm os.FileMode) error {
	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return nil
}
