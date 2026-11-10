package main

import (
	"fmt"
	"os"
	"regexp"

	"golang.org/x/sys/unix"
)

const cpuInformationPath = "/proc/cpuinfo"

var cpuModelPattern = regexp.MustCompile(`(?m)^(?:model name|Hardware|Model)\s*:\s*(.+)$`)

func readCPUModel() (string, error) {
	content, err := os.ReadFile(cpuInformationPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cpuInformationPath, err)
	}
	return parseCPUModel(string(content))
}

func parseCPUModel(cpuInformation string) (string, error) {
	match := cpuModelPattern.FindStringSubmatch(cpuInformation)
	if match == nil {
		return "", errCPUModelNotFound
	}
	return match[1], nil
}

func readRAMBytes() (uint64, error) {
	var system unix.Sysinfo_t
	if err := unix.Sysinfo(&system); err != nil {
		return 0, fmt.Errorf("calling sysinfo: %w", err)
	}
	return uint64(system.Totalram) * uint64(system.Unit), nil
}
