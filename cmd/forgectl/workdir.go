package main

import (
	"fmt"
	"os"
)

// workingDirectory reports where forgectl was invoked from. The working copy is
// discovered upward from here, so running from a subdirectory works (FR-001).
func workingDirectory() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("reading the working directory: %w", err)
	}

	return dir, nil
}
