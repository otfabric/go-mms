// SPDX-License-Identifier: MIT

// Package apisurface lists the exported API of a Go package from source.
// Coverage guards in the e2e and interop suites use it to require a test
// (or a documented exemption) for every public method.
package apisurface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Methods returns the exported methods declared on the named types in the
// non-test Go files of dir, as "Type.Method".
func Methods(dir string, types ...string) ([]string, error) {
	want := make(map[string]bool, len(types))
	for _, t := range types {
		want[t] = true
	}
	var out []string
	err := eachFuncDecl(dir, func(fd *ast.FuncDecl) {
		if fd.Recv == nil || len(fd.Recv.List) == 0 || !fd.Name.IsExported() {
			return
		}
		recv := receiverType(fd.Recv.List[0].Type)
		if want[recv] {
			out = append(out, recv+"."+fd.Name.Name)
		}
	})
	sort.Strings(out)
	return out, err
}

// Funcs returns the exported package-level functions of dir whose names
// start with prefix.
func Funcs(dir, prefix string) ([]string, error) {
	var out []string
	err := eachFuncDecl(dir, func(fd *ast.FuncDecl) {
		if fd.Recv == nil && fd.Name.IsExported() && strings.HasPrefix(fd.Name.Name, prefix) {
			out = append(out, fd.Name.Name)
		}
	})
	sort.Strings(out)
	return out, err
}

// TestFuncs returns the names of the Test functions declared in the
// _test.go files of dir, for any build tags.
func TestFuncs(dir string) (map[string]bool, error) {
	out := make(map[string]bool)
	err := walk(dir, true, func(f *ast.File) {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") {
				out[fd.Name.Name] = true
			}
		}
	})
	return out, err
}

// TestCalls maps each Test function declared in the _test.go files of dir
// to the names of the functions and methods called directly in its body
// (including closures), for any build tags. Calls through helpers do not
// count.
func TestCalls(dir string) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool)
	err := walk(dir, true, func(f *ast.File) {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil || !strings.HasPrefix(fd.Name.Name, "Test") {
				continue
			}
			calls := make(map[string]bool)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					switch fn := call.Fun.(type) {
					case *ast.Ident:
						calls[fn.Name] = true
					case *ast.SelectorExpr:
						calls[fn.Sel.Name] = true
					}
				}
				return true
			})
			out[fd.Name.Name] = calls
		}
	})
	return out, err
}

func eachFuncDecl(dir string, fn func(*ast.FuncDecl)) error {
	return walk(dir, false, func(f *ast.File) {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn(fd)
			}
		}
	})
}

func walk(dir string, tests bool, fn func(*ast.File)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") != tests {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		fn(f)
	}
	return nil
}

func receiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverType(t.X)
	}
	return ""
}
