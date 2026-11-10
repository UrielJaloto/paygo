package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleReport() report {
	return report{
		workloadSettings: workloadSettings{Scenario: scenarioUniform, Transfers: 10},
		measurements:     measurements{TransfersPerSecond: 1234.4, Latency: latencies{Percentile99: 3 * time.Millisecond}},
		Correct:          true,
	}
}

func TestWriteReport_PrintsTheJSONReport(t *testing.T) {
	var output bytes.Buffer
	if err := writeReport(&output, sampleReport()); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	var decoded report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded != sampleReport() {
		t.Errorf("decoded = %+v, err = %v", decoded, err)
	}
}

func TestSaveReport_CreatesTheDirectoryAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs", "uniform.json")
	if err := saveReport(path, sampleReport()); err != nil {
		t.Fatalf("saveReport: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}
	var decoded report
	if err := json.Unmarshal(content, &decoded); err != nil || decoded != sampleReport() {
		t.Errorf("decoded = %+v, err = %v", decoded, err)
	}
}

func TestSaveReport_UnwritableDirectory_ReturnsError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, reportFileMode); err != nil {
		t.Fatalf("creating blocker: %v", err)
	}
	if err := saveReport(filepath.Join(blocker, "report.json"), sampleReport()); err == nil {
		t.Error("err = nil, want a directory error")
	}
}

func TestFormatSummary(t *testing.T) {
	want := "out.json: 1234 transfers/s, p99 3ms, errors 0.0000, correct true\n"
	if got := formatSummary("out.json", sampleReport()); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestParseMinor_Invalid_ReturnsErrInvalidAmount(t *testing.T) {
	for _, amount := range []string{"10", "10.5", "1x.00"} {
		if _, err := parseMinor(amount); !errors.Is(err, errInvalidAmount) {
			t.Errorf("parseMinor(%q) err = %v", amount, err)
		}
	}
}
