package main

import (
	"errors"
	"testing"
)

func lookupFrom(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}
}

func TestParseOptions_Defaults(t *testing.T) {
	got, err := parseOptions(nil, lookupFrom(map[string]string{adminTokenVariable: "token"}))
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	want := options{scenario: scenarioUniform, accounts: defaultAccounts, transfers: defaultTransfers, clients: defaultClients, seed: defaultSeed, apiURL: defaultAPIURL, databaseURL: defaultDatabaseURL, adminToken: "token"}
	if got != want {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}

func TestParseOptions_Out_IsKept(t *testing.T) {
	got, err := parseOptions([]string{"-out", "report.json"}, lookupFrom(map[string]string{adminTokenVariable: "token"}))
	if err != nil || got.out != "report.json" {
		t.Errorf("out = %q, err = %v", got.out, err)
	}
}

func TestParseOptions_Failures(t *testing.T) {
	tests := []struct {
		name        string
		arguments   []string
		environment map[string]string
		want        error
	}{
		{name: "missing token", environment: map[string]string{}, want: errMissingAdminToken},
		{name: "empty token", environment: map[string]string{adminTokenVariable: ""}, want: errMissingAdminToken},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseOptions(testCase.arguments, lookupFrom(testCase.environment)); !errors.Is(err, testCase.want) {
				t.Errorf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestParseOptions_UnknownFlag_ReturnsError(t *testing.T) {
	if _, err := parseOptions([]string{"-results", "x"}, lookupFrom(map[string]string{adminTokenVariable: "token"})); err == nil {
		t.Error("err = nil, want a flag error")
	}
}
