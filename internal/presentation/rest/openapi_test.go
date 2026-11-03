package rest_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

const openAPIPath = "../../../docs/openapi.yaml"

var ginParameter = regexp.MustCompile(`:(\w+)`)

type openAPIParameter struct {
	Name string `json:"name"`
	In   string `json:"in"`
}

type openAPIOperation struct {
	Parameters []openAPIParameter `json:"parameters"`
}

type openAPIDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]openAPIOperation `json:"paths"`
}

func loadOpenAPI(t *testing.T) openAPIDocument {
	t.Helper()
	source, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}
	raw, err := yaml.YAMLToJSON(source)
	if err != nil {
		t.Fatalf("converting the contract: %v", err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decoding the contract: %v", err)
	}
	return document
}

func documentedRoutes(document openAPIDocument) []string {
	routes := []string{}
	for path, operations := range document.Paths {
		for method := range operations {
			routes = append(routes, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(routes)
	return routes
}

func servedRoutes() []string {
	routes := []string{}
	for _, route := range newRouter(&spyPanics{}, true).Routes() {
		routes = append(routes, route.Method+" "+ginParameter.ReplaceAllString(route.Path, "{$1}"))
	}
	slices.Sort(routes)
	return routes
}

func declaresIdempotencyKey(operation openAPIOperation) bool {
	return slices.Contains(operation.Parameters, openAPIParameter{Name: "Idempotency-Key", In: "header"})
}

func TestOpenAPI_File_DecodesWithTheExpectedHeader(t *testing.T) {
	document := loadOpenAPI(t)
	if !strings.HasPrefix(document.OpenAPI, "3.1") || document.Info.Title != "paygo" || document.Info.Version != "1.0.0" {
		t.Errorf("header = %q %q %q", document.OpenAPI, document.Info.Title, document.Info.Version)
	}
}

func TestOpenAPI_Paths_MatchTheRouter(t *testing.T) {
	documented, served := documentedRoutes(loadOpenAPI(t)), servedRoutes()
	if !slices.Equal(documented, served) {
		t.Errorf("documented routes differ from served routes\ndocumented: %v\nserved:     %v", documented, served)
	}
}

func TestOpenAPI_MoneyMovingPosts_DeclareIdempotencyKey(t *testing.T) {
	document := loadOpenAPI(t)
	paths := []string{
		"/v1/transfers",
		"/v1/transfers/{id}/reversal",
		"/v1/charges",
		"/v1/payouts",
		"/admin/v1/deposits",
		"/admin/v1/withdrawals",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			operation, found := document.Paths[path][strings.ToLower(http.MethodPost)]
			if !found {
				t.Fatal("POST is not documented")
			}
			if !declaresIdempotencyKey(operation) {
				t.Error("Idempotency-Key header is not declared")
			}
		})
	}
}
