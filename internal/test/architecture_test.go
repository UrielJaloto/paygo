package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/UrielJaloto/paygo/internal/"

var layerRanks = map[string]int{
	"domain":         0,
	"services":       1,
	"presentation":   2,
	"infrastructure": 2,
	"cmd":            3,
}

var contextOrder = map[string]map[string]int{
	"domain": {
		"money": 0, "page": 0, "apikey": 0,
		"account":  1,
		"ledger":   2,
		"transfer": 3,
		"payment":  4,
		"event":    5,
		"webhook":  6,
	},
	"services": {
		"account": 0, "transfer": 0, "apikey": 0, "webhook": 0, "dispatch": 0, "ledgercheck": 0, "payment": 0,
	},
	"infrastructure": {
		"config": 0, "clock": 0, "identifier": 0, "secret": 0, "randomness": 0, "ratelimit": 0,
		"postgres": 1, "webhookhttp": 1, "poller": 1, "httpserver": 1, "provider": 1,
		"logging": 2, "metrics": 2, "tracing": 2,
	},
	"presentation": {
		"rest": 0,
	},
}

type sourcePackage struct {
	directory     string
	imports []string
}

func TestDependencyMatrix_LayersAreLowerTriangular(t *testing.T) {
	tests := []struct {
		name string
		packages []sourcePackage
		want []string
	}{
		{name: "empty tree has no violations"},
		{
			name: "services importing domain is allowed",
			packages: []sourcePackage{{directory: "services/account", imports: []string{"domain/account"}}},
		},
		{
			name: "domain importing services is rejected",
			packages: []sourcePackage{{directory: "domain/account", imports: []string{"services/account"}}},
			want: []string{"domain/account must not import services/account"},
		},
		{
			name: "presentation importing infrastructure is rejected",
			packages: []sourcePackage{{directory: "presentation/rest", imports: []string{"infrastructure/config"}}},
			want: []string{"presentation/rest must not import infrastructure/config"},
		},
		{
			name: "infrastructure importing presentation is rejected",
			packages: []sourcePackage{{directory: "infrastructure/logging", imports: []string{"presentation/rest"}}},
			want: []string{"infrastructure/logging must not import presentation/rest"},
		},
		{
			name: "cmd may import every layer",
			packages: []sourcePackage{{
				directory:     "cmd/paygo-api",
				imports: []string{"domain/money", "services/account", "presentation/rest", "infrastructure/config"},
			}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assertViolations(t, layerViolations(testCase.packages), testCase.want)
		})
	}
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, layerViolations(readPackages(t)), nil)
	})
}

func TestDependencyMatrix_ContextsFollowDeclaredOrder(t *testing.T) {
	tests := []struct {
		name string
		packages []sourcePackage
		want []string
	}{
		{
			name: "lower context importing a higher one is rejected",
			packages: []sourcePackage{{directory: "domain/money", imports: []string{"domain/account"}}},
			want: []string{"domain/money must not import domain/account"},
		},
		{
			name: "higher context importing a lower one is allowed",
			packages: []sourcePackage{{directory: "domain/transfer", imports: []string{"domain/ledger"}}},
		},
		{
			name: "sibling services are rejected",
			packages: []sourcePackage{{directory: "services/account", imports: []string{"services/transfer"}}},
			want: []string{"services/account must not import services/transfer"},
		},
		{
			name: "undeclared context is rejected",
			packages: []sourcePackage{{directory: "domain/foo"}},
			want: []string{"domain/foo has no position in the DSM order"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assertViolations(t, contextViolations(testCase.packages, contextOrder), testCase.want)
		})
	}
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, contextViolations(readPackages(t), contextOrder), nil)
	})
}

func layerViolations(packages []sourcePackage) []string {
	var violations []string
	for _, pkg := range packages {
		violations = append(violations, packageLayerViolations(pkg)...)
	}
	return violations
}

func packageLayerViolations(pkg sourcePackage) []string {
	var violations []string
	for _, imported := range pkg.imports {
		if breaksLayerOrder(pkg.directory, imported) {
			violations = append(violations, pkg.directory+" must not import "+imported)
		}
	}
	return violations
}

func breaksLayerOrder(directory, imported string) bool {
	from, known := layerRanks[layerOf(directory)]
	to, knownImport := layerRanks[layerOf(imported)]
	return known && knownImport && layerOf(imported) != layerOf(directory) && to >= from
}

func contextViolations(packages []sourcePackage, order map[string]map[string]int) []string {
	var violations []string
	for _, pkg := range packages {
		violations = append(violations, packageContextViolations(pkg, order[layerOf(pkg.directory)])...)
	}
	return violations
}

func packageContextViolations(pkg sourcePackage, contexts map[string]int) []string {
	if contexts == nil {
		return nil
	}
	from, ok := contexts[contextOf(pkg.directory)]
	if !ok {
		return []string{pkg.directory + " has no position in the DSM order"}
	}
	var violations []string
	for _, imported := range pkg.imports {
		if breaksContextOrder(pkg.directory, imported, from, contexts) {
			violations = append(violations, pkg.directory+" must not import "+imported)
		}
	}
	return violations
}

func breaksContextOrder(directory, imported string, from int, contexts map[string]int) bool {
	if layerOf(imported) != layerOf(directory) || contextOf(imported) == contextOf(directory) {
		return false
	}
	to, ok := contexts[contextOf(imported)]
	return ok && to >= from
}

func layerOf(directory string) string {
	layer, _, _ := strings.Cut(directory, "/")
	return layer
}

func contextOf(directory string) string {
	_, rest, _ := strings.Cut(directory, "/")
	context, _, _ := strings.Cut(rest, "/")
	return context
}

func readPackages(t *testing.T) []sourcePackage {
	t.Helper()
	imports := map[string][]string{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipUnlessSource(path, entry)
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return collectImports(path, imports)
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	packages := make([]sourcePackage, 0, len(imports))
	for directory, list := range imports {
		packages = append(packages, sourcePackage{directory: directory, imports: list})
	}
	return packages
}

func skipUnlessSource(path string, entry fs.DirEntry) error {
	name := entry.Name()
	if path != "." && (strings.HasPrefix(name, ".") || name == "test") {
		return filepath.SkipDir
	}
	return nil
}

func collectImports(path string, imports map[string][]string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	directory := filepath.ToSlash(filepath.Dir(path))
	list := imports[directory]
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return fmt.Errorf("unquote import in %s: %w", path, err)
		}
		if internal, ok := strings.CutPrefix(value, modulePath); ok {
			list = append(list, internal)
		}
	}
	imports[directory] = list
	return nil
}

func assertViolations(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("violations:\n got %q\nwant %q", got, want)
	}
}

var allowedDomainImports = []string{
	"errors", "fmt", "strings", "strconv", "time", "crypto/sha256", "encoding/hex",
	"math", "regexp", "unicode", "net/url", "encoding/base64", "slices",
}

var thirdPartyHomes = map[string][]string{
	"github.com/gin-gonic/gin": {"presentation/rest", "cmd"},
	"github.com/jackc/pgx":     {"infrastructure/postgres"},
	"github.com/prometheus":    {"infrastructure/metrics", "cmd"},
	"go.opentelemetry.io":      {"infrastructure/tracing", "cmd"},
}

type sourceFile struct {
	path   string
	syntax *ast.File
}

func (f sourceFile) isTest() bool {
	return strings.HasSuffix(f.path, "_test.go")
}

func (f sourceFile) directory() string {
	return filepath.ToSlash(filepath.Dir(f.path))
}

func (f sourceFile) imports() []string {
	paths := make([]string, 0, len(f.syntax.Imports))
	for _, spec := range f.syntax.Imports {
		paths = append(paths, strings.Trim(spec.Path.Value, `"`))
	}
	return paths
}

func TestDomain_ImportsOnlyAllowedStandardLibrary(t *testing.T) {
	forbidden := "package money\nimport \"net/http\"\n"
	got := domainImportViolations(parseSources(t, "domain/money/money.go", forbidden))
	assertViolations(t, got, []string{"domain/money/money.go must not import net/http"})
	allowed := "package account\nimport (\n\"errors\"\n\"github.com/UrielJaloto/paygo/internal/domain/money\"\n)\n"
	assertViolations(t, domainImportViolations(parseSources(t, "domain/account/account.go", allowed)), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, domainImportViolations(readFiles(t)), nil)
	})
}

func TestThirdPartyImports_StayInTheirPackages(t *testing.T) {
	gin := "package account\nimport \"github.com/gin-gonic/gin\"\n"
	got := thirdPartyViolations(parseSources(t, "services/account/account_service.go", gin))
	assertViolations(t, got, []string{"services/account/account_service.go must not import github.com/gin-gonic/gin"})
	assertViolations(t, thirdPartyViolations(parseSources(t, "presentation/rest/router.go", gin)), nil)
	assertViolations(t, thirdPartyViolations(parseSources(t, "cmd/paygo-api/main.go", gin)), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, thirdPartyViolations(readFiles(t)), nil)
	})
}

func domainImportViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		if layerOf(file.directory()) == "domain" && !file.isTest() {
			violations = append(violations, forbiddenImports(file, isAllowedInDomain)...)
		}
	}
	return violations
}

func forbiddenImports(file sourceFile, allowed func(string) bool) []string {
	var violations []string
	for _, imported := range file.imports() {
		if !allowed(imported) {
			violations = append(violations, file.path+" must not import "+imported)
		}
	}
	return violations
}

func isAllowedInDomain(imported string) bool {
	return strings.HasPrefix(imported, modulePath) || slices.Contains(allowedDomainImports, imported)
}

func thirdPartyViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		violations = append(violations, forbiddenImports(file, func(imported string) bool {
			return livesInHome(file.directory(), imported)
		})...)
	}
	return violations
}

func livesInHome(directory, imported string) bool {
	for prefix, homes := range thirdPartyHomes {
		if strings.HasPrefix(imported, prefix) {
			return slices.ContainsFunc(homes, func(home string) bool {
				return directory == home || strings.HasPrefix(directory, home+"/")
			})
		}
	}
	return true
}

func readFiles(t *testing.T) []sourceFile {
	t.Helper()
	var files []sourceFile
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipUnlessSource(path, entry)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parseFile(filepath.ToSlash(path), nil)
		files = append(files, file)
		return err
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	return files
}

func parseSources(t *testing.T, path, source string) []sourceFile {
	t.Helper()
	file, err := parseFile(path, source)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return []sourceFile{file}
}

func parseFile(path string, source any) (sourceFile, error) {
	syntax, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
	if err != nil {
		return sourceFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return sourceFile{path: path, syntax: syntax}, nil
}

func TestMain(m *testing.M) {
	if err := os.Chdir(".."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
