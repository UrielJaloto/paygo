package main

import (
	"errors"
	"testing"
)

func TestParseCPUModel(t *testing.T) {
	tests := []struct {
		name           string
		cpuInformation string
		want           string
		wantErr        error
	}{
		{name: "x86", cpuInformation: "processor\t: 0\nmodel name\t: Intel(R) Core(TM) i7-13700F\n", want: "Intel(R) Core(TM) i7-13700F"},
		{name: "arm board", cpuInformation: "processor\t: 0\nModel\t\t: Raspberry Pi 5 Model B\n", want: "Raspberry Pi 5 Model B"},
		{name: "missing", cpuInformation: "processor\t: 0\nBogoMIPS\t: 50.00\n", wantErr: errCPUModelNotFound},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseCPUModel(testCase.cpuInformation)
			if !errors.Is(err, testCase.wantErr) || got != testCase.want {
				t.Errorf("parseCPUModel = %q, %v; want %q, %v", got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}
