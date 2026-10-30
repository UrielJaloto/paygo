package paygo_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/UrielJaloto/paygo/"

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
		"event":    4,
		"webhook":  5,
	},
	"services": {
		"account": 0, "transfer": 0, "apikey": 0, "webhook": 0, "dispatch": 0,
	},
	"infrastructure": {
		"config": 0, "clock": 0, "identifier": 0, "secret": 0, "randomness": 0, "ratelimit": 0,
		"postgres": 1, "webhookhttp": 1, "poller": 1, "httpserver": 1,
		"logging": 2, "metrics": 2, "tracing": 2,
	},
	"presentation": {
		"rest": 0,
	},
}

type sourcePackage struct {
	dir     string
	imports []string
}

func TestDependencyMatrix_LayersAreLowerTriangular(t *testing.T) {
	tests := []struct {
		name string
		pkgs []sourcePackage
		want []string
	}{
		{name: "empty tree has no violations"},
		{
			name: "services importing domain is allowed",
			pkgs: []sourcePackage{{dir: "services/account", imports: []string{"domain/account"}}},
		},
		{
			name: "domain importing services is rejected",
			pkgs: []sourcePackage{{dir: "domain/account", imports: []string{"services/account"}}},
			want: []string{"domain/account must not import services/account"},
		},
		{
			name: "presentation importing infrastructure is rejected",
			pkgs: []sourcePackage{{dir: "presentation/rest", imports: []string{"infrastructure/config"}}},
			want: []string{"presentation/rest must not import infrastructure/config"},
		},
		{
			name: "infrastructure importing presentation is rejected",
			pkgs: []sourcePackage{{dir: "infrastructure/logging", imports: []string{"presentation/rest"}}},
			want: []string{"infrastructure/logging must not import presentation/rest"},
		},
		{
			name: "cmd may import every layer",
			pkgs: []sourcePackage{{
				dir:     "cmd/paygo-api",
				imports: []string{"domain/money", "services/account", "presentation/rest", "infrastructure/config"},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertViolations(t, layerViolations(tt.pkgs), tt.want)
		})
	}
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, layerViolations(loadPackages(t)), nil)
	})
}

func TestDependencyMatrix_ContextsFollowDeclaredOrder(t *testing.T) {
	tests := []struct {
		name string
		pkgs []sourcePackage
		want []string
	}{
		{
			name: "lower context importing a higher one is rejected",
			pkgs: []sourcePackage{{dir: "domain/money", imports: []string{"domain/account"}}},
			want: []string{"domain/money must not import domain/account"},
		},
		{
			name: "higher context importing a lower one is allowed",
			pkgs: []sourcePackage{{dir: "domain/transfer", imports: []string{"domain/ledger"}}},
		},
		{
			name: "sibling services are rejected",
			pkgs: []sourcePackage{{dir: "services/account", imports: []string{"services/transfer"}}},
			want: []string{"services/account must not import services/transfer"},
		},
		{
			name: "undeclared context is rejected",
			pkgs: []sourcePackage{{dir: "domain/foo"}},
			want: []string{"domain/foo has no position in the DSM order"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertViolations(t, contextViolations(tt.pkgs, contextOrder), tt.want)
		})
	}
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, contextViolations(loadPackages(t), contextOrder), nil)
	})
}

func layerViolations(pkgs []sourcePackage) []string {
	var violations []string
	for _, pkg := range pkgs {
		from, ok := layerRanks[layerOf(pkg.dir)]
		if !ok {
			continue
		}
		for _, imported := range pkg.imports {
			to, ok := layerRanks[layerOf(imported)]
			if ok && layerOf(imported) != layerOf(pkg.dir) && to >= from {
				violations = append(violations, pkg.dir+" must not import "+imported)
			}
		}
	}
	return violations
}

func contextViolations(pkgs []sourcePackage, order map[string]map[string]int) []string {
	var violations []string
	for _, pkg := range pkgs {
		contexts, ok := order[layerOf(pkg.dir)]
		if !ok {
			continue
		}
		from, ok := contexts[contextOf(pkg.dir)]
		if !ok {
			violations = append(violations, pkg.dir+" has no position in the DSM order")
			continue
		}
		for _, imported := range pkg.imports {
			if breaksContextOrder(pkg.dir, imported, from, contexts) {
				violations = append(violations, pkg.dir+" must not import "+imported)
			}
		}
	}
	return violations
}

func breaksContextOrder(dir, imported string, from int, contexts map[string]int) bool {
	if layerOf(imported) != layerOf(dir) || contextOf(imported) == contextOf(dir) {
		return false
	}
	to, ok := contexts[contextOf(imported)]
	return ok && to >= from
}

func layerOf(dir string) string {
	layer, _, _ := strings.Cut(dir, "/")
	return layer
}

func contextOf(dir string) string {
	_, rest, _ := strings.Cut(dir, "/")
	context, _, _ := strings.Cut(rest, "/")
	return context
}

func loadPackages(t *testing.T) []sourcePackage {
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
	pkgs := make([]sourcePackage, 0, len(imports))
	for dir, list := range imports {
		pkgs = append(pkgs, sourcePackage{dir: dir, imports: list})
	}
	return pkgs
}

func skipUnlessSource(path string, entry fs.DirEntry) error {
	name := entry.Name()
	if path != "." && (strings.HasPrefix(name, ".") || name == "testdata" || name == "docs") {
		return filepath.SkipDir
	}
	return nil
}

func collectImports(path string, imports map[string][]string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	list := imports[dir]
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return fmt.Errorf("unquote import in %s: %w", path, err)
		}
		if internal, ok := strings.CutPrefix(value, modulePath); ok {
			list = append(list, internal)
		}
	}
	imports[dir] = list
	return nil
}

func assertViolations(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("violations:\n got %q\nwant %q", got, want)
	}
}
