package main

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	processorKey       = `HARDWARE\DESCRIPTION\System\CentralProcessor\0`
	processorNameValue = "ProcessorNameString"
)

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	length                   uint32
	memoryLoad               uint32
	totalPhysical            uint64
	availablePhysical        uint64
	totalPageFile            uint64
	availablePageFile        uint64
	totalVirtual             uint64
	availableVirtual         uint64
	availableExtendedVirtual uint64
}

func readCPUModel() (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, processorKey, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", processorKey, err)
	}
	defer key.Close()
	model, _, err := key.GetStringValue(processorNameValue)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", processorNameValue, err)
	}
	if strings.TrimSpace(model) == "" {
		return "", errCPUModelNotFound
	}
	return strings.TrimSpace(model), nil
}

func readRAMBytes() (uint64, error) {
	status := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	succeeded, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if succeeded == 0 {
		return 0, fmt.Errorf("calling GlobalMemoryStatusEx: %w", err)
	}
	return status.totalPhysical, nil
}
