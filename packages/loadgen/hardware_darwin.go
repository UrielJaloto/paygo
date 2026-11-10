package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const (
	cpuBrandSysctl   = "machdep.cpu.brand_string"
	memorySizeSysctl = "hw.memsize"
)

func readCPUModel() (string, error) {
	model, err := unix.Sysctl(cpuBrandSysctl)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cpuBrandSysctl, err)
	}
	if model == "" {
		return "", errCPUModelNotFound
	}
	return model, nil
}

func readRAMBytes() (uint64, error) {
	size, err := unix.SysctlUint64(memorySizeSysctl)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", memorySizeSysctl, err)
	}
	return size, nil
}
