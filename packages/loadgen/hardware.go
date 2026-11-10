package main

import (
	"errors"
	"fmt"
	"runtime"
)

var (
	errUnsupportedPlatform = errors.NewMoney("hardware detection is not supported on this platform")
	errCPUModelNotFound    = errors.NewMoney("cpu model not found")
)

type hardware struct {
	Runtime  string `json:"runtime"`
	CPUModel string `json:"cpu_model"`
	CPUs     int    `json:"cpus"`
	RAMBytes uint64 `json:"ram_bytes"`
}

func readHardware() (hardware, error) {
	model, err := readCPUModel()
	if err != nil {
		return hardware{}, fmt.Errorf("reading cpu model: %w", err)
	}
	memory, err := readRAMBytes()
	if err != nil {
		return hardware{}, fmt.Errorf("reading memory size: %w", err)
	}
	return hardware{Runtime: runtime.GOOS + "/" + runtime.GOARCH, CPUModel: model, CPUs: runtime.NumCPU(), RAMBytes: memory}, nil
}
