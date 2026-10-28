// SPDX-License-Identifier: MIT

package apisurface

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const srcClient = `package sample

type Client struct{}

type Server[T any] struct{ v T }

type other struct{}

func (c *Client) Read() error         { return nil }
func (c Client) Write() error         { return nil }
func (c *Client) private()            {}
func (s *Server[T]) Serve() T         { return s.v }
func (s Server[T]) Close() error      { return nil }
func (o *other) Exported()            {}

func NewClient() *Client              { return nil }
func NewServer() *Server[int]         { return nil }
func Dial() error                     { return nil }
func newHelper()                      {}
`

// srcWeird holds a receiver with several type parameters (an
// *ast.IndexListExpr), which receiverType does not resolve, and a function
// declaration without a body.
const srcWeird = `package sample

type pair[K, V any] struct{}

func (p pair[K, V]) Keys() {}

func NewWithoutBody()
`

const srcTests = `//go:build integration

package sample

import "testing"

func helper(t *testing.T) { t.Helper() }

func TestRead(t *testing.T) {
	c := NewClient()
	_ = c.Read()
	func() { Dial() }()
	helper(t)
}

func TestEmpty(t *testing.T) {}

func BenchmarkRead(b *testing.B) { _ = NewClient().Read() }

type suite struct{}

func (suite) TestMethod(t *testing.T) { Dial() }

var x = func() int { return 1 }()
`

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sampleDir(t *testing.T) string {
	return writeFiles(t, map[string]string{
		"client.go":        srcClient,
		"weird.go":         srcWeird,
		"client_test.go":   srcTests,
		"README.md":        "not go",
		"sub/ignored.go":   "package sub\n\nfunc (c *Client) Ignored() {}\n",
		"sub/ignored2.txt": "x",
	})
}

func TestListMethods(t *testing.T) {
	dir := sampleDir(t)
	got, err := Methods(dir, "Client", "Server")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Client.Read", "Client.Write", "Server.Close", "Server.Serve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Methods = %v, want %v", got, want)
	}

	got, err = Methods(dir, "Client")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Client.Read", "Client.Write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Methods(Client) = %v, want %v", got, want)
	}

	got, err = Methods(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("Methods() = %v, %v; want empty", got, err)
	}
}

func TestListFuncs(t *testing.T) {
	dir := sampleDir(t)
	got, err := Funcs(dir, "New")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"NewClient", "NewServer", "NewWithoutBody"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Funcs(New) = %v, want %v", got, want)
	}
	got, err = Funcs(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Dial", "NewClient", "NewServer", "NewWithoutBody"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Funcs(\"\") = %v, want %v", got, want)
	}
}

func TestListTestFuncs(t *testing.T) {
	got, err := TestFuncs(sampleDir(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"TestRead": true, "TestEmpty": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TestFuncs = %v, want %v", got, want)
	}
}

func TestListTestCalls(t *testing.T) {
	got, err := TestCalls(sampleDir(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]bool{
		"TestRead": {
			"NewClient": true,
			"Read":      true,
			"Dial":      true,
			"helper":    true,
		},
		"TestEmpty": {},
	}
	// The immediately-invoked closure is a call of a FuncLit; it is
	// neither an identifier nor a selector, so it adds no name.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TestCalls = %v, want %v", got, want)
	}
}

func TestEmptyDir(t *testing.T) {
	dir := t.TempDir()
	m, err := Methods(dir, "X")
	if err != nil || len(m) != 0 {
		t.Fatalf("Methods = %v, %v", m, err)
	}
	f, err := Funcs(dir, "")
	if err != nil || len(f) != 0 {
		t.Fatalf("Funcs = %v, %v", f, err)
	}
	tf, err := TestFuncs(dir)
	if err != nil || len(tf) != 0 {
		t.Fatalf("TestFuncs = %v, %v", tf, err)
	}
	tc, err := TestCalls(dir)
	if err != nil || len(tc) != 0 {
		t.Fatalf("TestCalls = %v, %v", tc, err)
	}
}

func TestMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Methods(dir, "X"); err == nil {
		t.Error("Methods: expected error")
	}
	if _, err := Funcs(dir, ""); err == nil {
		t.Error("Funcs: expected error")
	}
	if _, err := TestFuncs(dir); err == nil {
		t.Error("TestFuncs: expected error")
	}
	if _, err := TestCalls(dir); err == nil {
		t.Error("TestCalls: expected error")
	}
}

func TestParseError(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bad.go":      "package bad\nfunc {",
		"bad_test.go": "package bad\nfunc Test( {",
	})
	if _, err := Methods(dir, "X"); err == nil {
		t.Error("Methods: expected parse error")
	}
	if _, err := Funcs(dir, ""); err == nil {
		t.Error("Funcs: expected parse error")
	}
	if _, err := TestFuncs(dir); err == nil {
		t.Error("TestFuncs: expected parse error")
	}
	if _, err := TestCalls(dir); err == nil {
		t.Error("TestCalls: expected parse error")
	}
}

func TestOwnPackage(t *testing.T) {
	// Sanity check against real source: this package's own API.
	got, err := Funcs(".", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Funcs", "Methods", "TestCalls", "TestFuncs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Funcs(.) = %v, want %v", got, want)
	}
	tests, err := TestFuncs(".")
	if err != nil {
		t.Fatal(err)
	}
	if !tests["TestOwnPackage"] {
		t.Fatal("TestFuncs(.) does not list TestOwnPackage")
	}
}
