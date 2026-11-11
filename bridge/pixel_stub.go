//go:build !windows

package main

import (
	"os"
	"strconv"
	"strings"
)

// samplePixels stub for non-Windows testing. The file named by
// WOWOFBUTTPLUG_PIXEL_FILE holds the current sample: "<event> <intensity>"
// (e.g. "damage 0.8") or "none" for idle.
func samplePixels() (event string, intensity float64) {
	path := os.Getenv("WOWOFBUTTPLUG_PIXEL_FILE")
	if path == "" {
		return "", 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0
	}
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) == 0 || fields[0] == "none" {
		return "", 0
	}
	event = fields[0]
	intensity = 0.5
	if len(fields) >= 2 {
		if v, err := strconv.ParseFloat(fields[1], 64); err == nil {
			intensity = v
		}
	}
	return event, intensity
}
