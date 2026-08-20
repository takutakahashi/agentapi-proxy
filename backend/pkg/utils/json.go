package utils

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONWriteOptions holds options for JSON writing
type JSONWriteOptions struct {
	Indent   string
	FileMode os.FileMode
	Atomic   bool
}

// DefaultJSONWriteOptions returns default options for JSON writing
func DefaultJSONWriteOptions() JSONWriteOptions {
	return JSONWriteOptions{
		Indent:   "  ",
		FileMode: 0644,
		Atomic:   true,
	}
}

// WriteJSONFile writes a Go object to a JSON file with the given options
func WriteJSONFile(filePath string, data interface{}, options JSONWriteOptions) error {
	var jsonData []byte
	var err error

	if options.Indent != "" {
		jsonData, err = json.MarshalIndent(data, "", options.Indent)
	} else {
		jsonData, err = json.Marshal(data)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if options.Atomic {
		return AtomicWriteFile(filePath, jsonData, options.FileMode)
	}

	return os.WriteFile(filePath, jsonData, options.FileMode)
}

// WriteJSONFileDefault writes a Go object to a JSON file with default options
func WriteJSONFileDefault(filePath string, data interface{}) error {
	return WriteJSONFile(filePath, data, DefaultJSONWriteOptions())
}

// ReadJSONFile reads a JSON file into a Go object
func ReadJSONFile(filePath string, target interface{}) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON from %s: %w", filePath, err)
	}

	return nil
}

// MarshalJSONString marshals a Go object to a JSON string
func MarshalJSONString(data interface{}) (string, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonBytes), nil
}

// MarshalJSONIndentString marshals a Go object to an indented JSON string
func MarshalJSONIndentString(data interface{}, indent string) (string, error) {
	jsonBytes, err := json.MarshalIndent(data, "", indent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON with indent: %w", err)
	}
	return string(jsonBytes), nil
}
