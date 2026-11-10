package main

import (
	"runtime"
	"testing"
)

func TestReadHardware_ThisMachine_ReportsEveryField(t *testing.T) {
	got, err := readHardware()
	if err != nil {
		t.Fatalf("readHardware: %v", err)
	}
	if got.Runtime != runtime.GOOS+"/"+runtime.GOARCH || got.CPUModel == "" || got.CPUs != runtime.NumCPU() || got.RAMBytes == 0 {
		t.Errorf("hardware = %+v", got)
	}
}
