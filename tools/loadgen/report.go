package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

var (
	cpuModelPattern = regexp.MustCompile(`(?m)^model name\s*:\s*(.+)$`)
	memTotalPattern = regexp.MustCompile(`(?m)^MemTotal:\s*(\d+) kB$`)
)

type hardware struct {
	Runtime  string `json:"runtime"`
	CPUModel string `json:"cpu_model"`
	CPUs     int    `json:"cpus"`
	RAMBytes int64  `json:"ram_bytes"`
}

type versions struct {
	Go       string `json:"go"`
	Postgres string `json:"postgres,omitempty"`
}

type report struct {
	Scenario         string    `json:"scenario"`
	Accounts         int       `json:"accounts"`
	Transfers        int       `json:"transfers"`
	Clients          int       `json:"clients"`
	Seed             uint64    `json:"seed"`
	Hardware         hardware  `json:"hardware"`
	Versions         versions  `json:"versions"`
	TransfersPerSec  float64   `json:"transfers_per_second"`
	Latency          latencies `json:"latency"`
	ErrorRate        float64   `json:"error_rate"`
	WALBytesTransfer float64   `json:"wal_bytes_per_transfer"`
	Correct          bool      `json:"correct"`
}

func readHardware() hardware {
	cpuinfo, _ := os.ReadFile("/proc/cpuinfo")
	meminfo, _ := os.ReadFile("/proc/meminfo")
	return hardware{
		Runtime:  runtime.GOOS + "/" + runtime.GOARCH,
		CPUModel: cpuModel(string(cpuinfo)),
		CPUs:     runtime.NumCPU(),
		RAMBytes: ramBytes(string(meminfo)),
	}
}

func cpuModel(cpuinfo string) string {
	if match := cpuModelPattern.FindStringSubmatch(cpuinfo); match != nil {
		return match[1]
	}
	return os.Getenv("PROCESSOR_IDENTIFIER")
}

func ramBytes(meminfo string) int64 {
	match := memTotalPattern.FindStringSubmatch(meminfo)
	if match == nil {
		return 0
	}
	kilobytes, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0
	}
	return kilobytes * 1024
}

func resultPath(directory string, measured report, date time.Time) string {
	name := fmt.Sprintf("%s-%s.json", date.Format(time.DateOnly), measured.Scenario)
	return filepath.Join(directory, name)
}

func writeReport(path string, measured report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating results directory: %w", err)
	}
	encoded, err := json.MarshalIndent(measured, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding report: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}
