package paygo_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
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
		"payment":  4,
		"event":    5,
		"webhook":  6,
	},
	"services": {
		"account": 0, "transfer": 0, "apikey": 0, "webhook": 0, "dispatch": 0, "ledgercheck": 0, "payment": 0,
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
		violations = append(violations, packageLayerViolations(pkg)...)
	}
	return violations
}

func packageLayerViolations(pkg sourcePackage) []string {
	var violations []string
	for _, imported := range pkg.imports {
		if breaksLayerOrder(pkg.dir, imported) {
			violations = append(violations, pkg.dir+" must not import "+imported)
		}
	}
	return violations
}

func breaksLayerOrder(dir, imported string) bool {
	from, known := layerRanks[layerOf(dir)]
	to, knownImport := layerRanks[layerOf(imported)]
	return known && knownImport && layerOf(imported) != layerOf(dir) && to >= from
}

func contextViolations(pkgs []sourcePackage, order map[string]map[string]int) []string {
	var violations []string
	for _, pkg := range pkgs {
		violations = append(violations, packageContextViolations(pkg, order[layerOf(pkg.dir)])...)
	}
	return violations
}

func packageContextViolations(pkg sourcePackage, contexts map[string]int) []string {
	if contexts == nil {
		return nil
	}
	from, ok := contexts[contextOf(pkg.dir)]
	if !ok {
		return []string{pkg.dir + " has no position in the DSM order"}
	}
	var violations []string
	for _, imported := range pkg.imports {
		if breaksContextOrder(pkg.dir, imported, from, contexts) {
			violations = append(violations, pkg.dir+" must not import "+imported)
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

const (
	maxFunctionLines  = 30
	maxParameters     = 3
	maxNewParameters  = 5
	maxNesting        = 2
	maxComplexity     = 10
	maxServiceMethods = 7
	maxStructFields   = 10
)

var genericNames = []string{
	"utils", "helpers", "common", "shared", "misc", "base", "core", "lib", "tools", "general",
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
	fset   *token.FileSet
	syntax *ast.File
}

func (f sourceFile) isTest() bool {
	return strings.HasSuffix(f.path, "_test.go")
}

func (f sourceFile) dir() string {
	return filepath.ToSlash(filepath.Dir(f.path))
}

func (f sourceFile) imports() []string {
	paths := make([]string, 0, len(f.syntax.Imports))
	for _, spec := range f.syntax.Imports {
		paths = append(paths, strings.Trim(spec.Path.Value, `"`))
	}
	return paths
}

func TestSource_HasNoComments(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{name: "line comment", src: "package x\n// note\n", want: []string{"x.go:2 has a comment"}},
		{name: "build tag", src: "//go:build integration\n\npackage x\n"},
		{name: "block comment", src: "package x\n/* note */\n", want: []string{"x.go:2 has a comment"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertViolations(t, commentViolations(parseSources(t, "x.go", tt.src)), tt.want)
		})
	}
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, commentViolations(loadFiles(t)), nil)
	})
}

func TestPackages_HaveNoGenericNames(t *testing.T) {
	got := genericNameViolations(parseSources(t, "domain/utils/money.go", "package utils\n"))
	assertViolations(t, got, []string{"domain/utils/money.go has a generic name"})
	got = genericNameViolations(parseSources(t, "domain/money/helpers.go", "package money\n"))
	assertViolations(t, got, []string{"domain/money/helpers.go has a generic name"})
	assertViolations(t, genericNameViolations(parseSources(t, "domain/money/money.go", "package money\n")), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, genericNameViolations(loadFiles(t)), nil)
	})
}

func TestFunctions_RespectSizeLimits(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{name: "30 lines", src: functionWithLines(30)},
		{name: "31 lines", src: functionWithLines(31), want: []string{"x.go:F lines 31"}},
		{name: "31 lines in a test file", path: "x_test.go", src: functionWithLines(31)},
		{name: "context plus 3 params", src: "package x\nfunc F(ctx context.Context, a, b, c int) {}\n"},
		{name: "4 params", src: "package x\nfunc F(a, b, c, d int) {}\n", want: []string{"x.go:F params 4"}},
		{name: "4 params in a test file", path: "x_test.go", src: "package x\nfunc F(a, b, c, d int) {}\n"},
		{name: "constructor with 5 params", src: "package x\nfunc NewX(a, b, c, d, e int) {}\n"},
		{name: "constructor with 6 params", src: "package x\nfunc NewX(a, b, c, d, e, f int) {}\n", want: []string{"x.go:NewX params 6"}},
		{name: "nesting 2", src: functionWithNesting(2)},
		{name: "nesting 3", src: functionWithNesting(3), want: []string{"x.go:F nesting 3"}},
		{name: "nesting 3 in a test file", path: "x_test.go", src: functionWithNesting(3), want: []string{"x_test.go:F nesting 3"}},
		{name: "9 ifs", src: functionWithIfs(9)},
		{name: "10 ifs", src: functionWithIfs(10), want: []string{"x.go:F complexity 11"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if path == "" {
				path = "x.go"
			}
			assertViolations(t, functionViolations(parseSources(t, path, tt.src)), tt.want)
		})
	}
	t.Run("boolean operators add complexity", func(t *testing.T) {
		src := "package x\nfunc F(a, b, c bool) {\n\tif a && b && c {\n\t}\n}\n"
		declaration := parseSources(t, "x.go", src)[0].syntax.Decls[0].(*ast.FuncDecl)
		if got := complexityOf(declaration.Body); got != 4 {
			t.Errorf("complexity = %d, want 4", got)
		}
	})
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, functionViolations(loadFiles(t)), nil)
	})
}

func TestServices_RespectSizeLimits(t *testing.T) {
	var methods strings.Builder
	methods.WriteString("package account\ntype Service struct{}\n")
	for i := range 8 {
		fmt.Fprintf(&methods, "func (s *Service) M%d() {}\n", i)
	}
	got := serviceViolations(parseSources(t, "services/account/account_service.go", methods.String()))
	assertViolations(t, got, []string{"services/account Service methods 8"})
	constructor := "package account\nfunc New(a, b, c, d, e, f int) {}\n"
	got = serviceViolations(parseSources(t, "services/account/account_service.go", constructor))
	assertViolations(t, got, []string{"services/account New params 6"})
	outside := parseSources(t, "domain/account/account.go", methods.String())
	assertViolations(t, serviceViolations(outside), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, serviceViolations(loadFiles(t)), nil)
	})
}

func TestTypes_HaveAtMostTenFields(t *testing.T) {
	src := "package x\ntype T struct {\n\ta, b, c, d, e, f, g, h, i, j, k int\n}\n"
	assertViolations(t, fieldViolations(parseSources(t, "x.go", src)), []string{"x.go:T fields 11"})
	assertViolations(t, fieldViolations(parseSources(t, "x_test.go", src)), nil)
	small := "package x\ntype T struct {\n\ta, b, c, d, e, f, g, h, i, j int\n}\n"
	assertViolations(t, fieldViolations(parseSources(t, "x.go", small)), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, fieldViolations(loadFiles(t)), nil)
	})
}

func TestDomain_ImportsOnlyAllowedStandardLibrary(t *testing.T) {
	forbidden := "package money\nimport \"net/http\"\n"
	got := domainImportViolations(parseSources(t, "domain/money/money.go", forbidden))
	assertViolations(t, got, []string{"domain/money/money.go must not import net/http"})
	allowed := "package account\nimport (\n\"errors\"\n\"github.com/UrielJaloto/paygo/domain/money\"\n)\n"
	assertViolations(t, domainImportViolations(parseSources(t, "domain/account/account.go", allowed)), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, domainImportViolations(loadFiles(t)), nil)
	})
}

func TestThirdPartyImports_StayInTheirPackages(t *testing.T) {
	gin := "package account\nimport \"github.com/gin-gonic/gin\"\n"
	got := thirdPartyViolations(parseSources(t, "services/account/account_service.go", gin))
	assertViolations(t, got, []string{"services/account/account_service.go must not import github.com/gin-gonic/gin"})
	assertViolations(t, thirdPartyViolations(parseSources(t, "presentation/rest/router.go", gin)), nil)
	assertViolations(t, thirdPartyViolations(parseSources(t, "cmd/paygo-api/main.go", gin)), nil)
	t.Run("real tree has no violations", func(t *testing.T) {
		assertViolations(t, thirdPartyViolations(loadFiles(t)), nil)
	})
}

func commentViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		for _, group := range file.syntax.Comments {
			violations = append(violations, groupViolations(file, group)...)
		}
	}
	return violations
}

func groupViolations(file sourceFile, group *ast.CommentGroup) []string {
	var violations []string
	for _, comment := range group.List {
		if !strings.HasPrefix(comment.Text, "//go:build") {
			line := file.fset.Position(comment.Pos()).Line
			violations = append(violations, fmt.Sprintf("%s:%d has a comment", file.path, line))
		}
	}
	return violations
}

func genericNameViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(file.path, ".go"), "_test")
		segments := strings.Split(trimmed, "/")
		if slices.ContainsFunc(segments, isGenericName) {
			violations = append(violations, file.path+" has a generic name")
		}
	}
	return violations
}

func isGenericName(name string) bool {
	return slices.Contains(genericNames, name)
}

func functionViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		violations = append(violations, fileFunctionViolations(file)...)
	}
	return violations
}

func fileFunctionViolations(file sourceFile) []string {
	var violations []string
	for _, declaration := range file.syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Body != nil {
			violations = append(violations, measureFunction(file, function)...)
		}
	}
	return violations
}

func measureFunction(file sourceFile, function *ast.FuncDecl) []string {
	report := func(rule string, value int) string {
		return fmt.Sprintf("%s:%s %s %d", file.path, function.Name.Name, rule, value)
	}
	var violations []string
	if lines := bodyLines(file, function); lines > maxFunctionLines && !file.isTest() {
		violations = append(violations, report("lines", lines))
	}
	if params := parameterCount(function); params > parameterLimit(function) && !file.isTest() {
		violations = append(violations, report("params", params))
	}
	if nesting := nestingOf(function.Body, 0); nesting > maxNesting {
		violations = append(violations, report("nesting", nesting))
	}
	if complexity := complexityOf(function.Body); complexity > maxComplexity {
		violations = append(violations, report("complexity", complexity))
	}
	return violations
}

func bodyLines(file sourceFile, function *ast.FuncDecl) int {
	start := file.fset.Position(function.Body.Lbrace).Line
	end := file.fset.Position(function.Body.Rbrace).Line
	return end - start - 1
}

func parameterLimit(function *ast.FuncDecl) int {
	if strings.HasPrefix(function.Name.Name, "New") {
		return maxNewParameters
	}
	return maxParameters
}

func parameterCount(function *ast.FuncDecl) int {
	count := 0
	fields := function.Type.Params.List
	for i, field := range fields {
		if i == 0 && isContextType(field.Type) {
			count += len(field.Names) - 1
			continue
		}
		count += max(len(field.Names), 1)
	}
	return count
}

func isContextType(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Context"
}

func nestingOf(node ast.Node, depth int) int {
	deepest := depth
	ast.Inspect(node, func(child ast.Node) bool {
		if child == node || !opensNesting(child) {
			return true
		}
		deepest = max(deepest, nestingOf(child, depth+1))
		return false
	})
	return deepest
}

func opensNesting(node ast.Node) bool {
	switch node.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func complexityOf(body ast.Node) int {
	complexity := 1
	ast.Inspect(body, func(node ast.Node) bool {
		if addsBranch(node) {
			complexity++
		}
		return true
	})
	return complexity
}

func addsBranch(node ast.Node) bool {
	switch typed := node.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		return true
	case *ast.CaseClause:
		return typed.List != nil
	case *ast.CommClause:
		return typed.Comm != nil
	case *ast.BinaryExpr:
		return typed.Op == token.LAND || typed.Op == token.LOR
	}
	return false
}

func serviceViolations(files []sourceFile) []string {
	methods := map[string]int{}
	var violations []string
	for _, file := range files {
		if layerOf(file.dir()) != "services" || file.isTest() {
			continue
		}
		countServiceMethods(file, methods)
		violations = append(violations, constructorViolations(file)...)
	}
	for _, dir := range slices.Sorted(maps.Keys(methods)) {
		if methods[dir] > maxServiceMethods {
			violations = append(violations, fmt.Sprintf("%s Service methods %d", dir, methods[dir]))
		}
	}
	return violations
}

func countServiceMethods(file sourceFile, methods map[string]int) {
	for _, declaration := range file.syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.IsExported() && isServiceReceiver(function) {
			methods[file.dir()]++
		}
	}
}

func isServiceReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	name, ok := receiver.(*ast.Ident)
	return ok && name.Name == "Service"
}

func constructorViolations(file sourceFile) []string {
	var violations []string
	for _, declaration := range file.syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Name.Name != "New" {
			continue
		}
		if params := parameterCount(function); params > maxNewParameters {
			violations = append(violations, fmt.Sprintf("%s New params %d", file.dir(), params))
		}
	}
	return violations
}

func fieldViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		if file.isTest() {
			continue
		}
		ast.Inspect(file.syntax, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if ok {
				violations = append(violations, structViolations(file, spec)...)
			}
			return true
		})
	}
	return violations
}

func structViolations(file sourceFile, spec *ast.TypeSpec) []string {
	structure, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	count := 0
	for _, field := range structure.Fields.List {
		count += max(len(field.Names), 1)
	}
	if count <= maxStructFields {
		return nil
	}
	return []string{fmt.Sprintf("%s:%s fields %d", file.path, spec.Name.Name, count)}
}

func domainImportViolations(files []sourceFile) []string {
	var violations []string
	for _, file := range files {
		if layerOf(file.dir()) == "domain" && !file.isTest() {
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
			return livesInHome(file.dir(), imported)
		})...)
	}
	return violations
}

func livesInHome(dir, imported string) bool {
	for prefix, homes := range thirdPartyHomes {
		if strings.HasPrefix(imported, prefix) {
			return slices.ContainsFunc(homes, func(home string) bool {
				return dir == home || strings.HasPrefix(dir, home+"/")
			})
		}
	}
	return true
}

func loadFiles(t *testing.T) []sourceFile {
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

func parseSources(t *testing.T, path, src string) []sourceFile {
	t.Helper()
	file, err := parseFile(path, src)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return []sourceFile{file}
}

func parseFile(path string, src any) (sourceFile, error) {
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return sourceFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return sourceFile{path: path, fset: fset, syntax: syntax}, nil
}

func functionWithLines(lines int) string {
	return "package x\nfunc F() {\n" + strings.Repeat("\t_ = 0\n", lines) + "}\n"
}

func functionWithNesting(depth int) string {
	return "package x\nfunc F(a bool) {\n" + strings.Repeat("if a {\n", depth) + strings.Repeat("}\n", depth) + "}\n"
}

func functionWithIfs(count int) string {
	return "package x\nfunc F(a bool) {\n" + strings.Repeat("if a {\n}\n", count) + "}\n"
}
