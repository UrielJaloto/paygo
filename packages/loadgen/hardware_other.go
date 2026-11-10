//go:build !linux && !darwin && !windows

package main

func readCPUModel() (string, error) {
	return "", errUnsupportedPlatform
}

func readRAMBytes() (uint64, error) {
	return 0, errUnsupportedPlatform
}
