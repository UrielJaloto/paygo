package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	reportDirectoryMode = 0o755
	reportFileMode      = 0o644
)

type versions struct {
	Go       string `json:"go"`
	Postgres string `json:"postgres,omitempty"`
}

type report struct {
	workloadSettings
	measurements
	Hardware hardware `json:"hardware"`
	Versions versions `json:"versions"`
	Correct  bool     `json:"correct"`
}

type workloadSettings struct {
	Scenario  string `json:"scenario"`
	Accounts  int    `json:"accounts"`
	Transfers int    `json:"transfers"`
	Clients   int    `json:"clients"`
	Seed      uint64 `json:"seed"`
}

type measurements struct {
	TransfersPerSecond  float64   `json:"transfers_per_second"`
	Latency             latencies `json:"latency"`
	ErrorRate           float64   `json:"error_rate"`
	WALBytesPerTransfer float64   `json:"wal_bytes_per_transfer"`
}

func encodeReport(measured report) ([]byte, error) {
	encoded, err := json.MarshalIndent(measured, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding report: %w", err)
	}
	return append(encoded, '\n'), nil
}

func writeReport(output io.Writer, measured report) error {
	encoded, err := encodeReport(measured)
	if err != nil {
		return err
	}
	if _, err := output.Write(encoded); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

func saveReport(path string, measured report) error {
	encoded, err := encodeReport(measured)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), reportDirectoryMode); err != nil {
		return fmt.Errorf("creating report directory: %w", err)
	}
	if err := os.WriteFile(path, encoded, reportFileMode); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

func formatSummary(path string, measured report) string {
	return fmt.Sprintf("%s: %.0f transfers/s, p99 %s, errors %.4f, correct %t\n", path, measured.TransfersPerSecond, measured.Latency.Percentile99, measured.ErrorRate, measured.Correct)
}
