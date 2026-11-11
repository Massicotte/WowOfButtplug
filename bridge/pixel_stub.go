//go:build !windows

package main

import (
	"os"
	"strings"
)

// redPixelNearTop stub for non-Windows testing: reports red when the file
// named by WOWOFBUTTPLUG_PIXEL_FILE contains "red".
func redPixelNearTop() bool {
	path := os.Getenv("WOWOFBUTTPLUG_PIXEL_FILE")
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "red")
}
