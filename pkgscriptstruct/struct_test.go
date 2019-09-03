// Copyright 2018 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkgscriptstruct_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/andrewchambers/pkgscript/resolve"
	"github.com/andrewchambers/pkgscript/pkgscript"
	"github.com/andrewchambers/pkgscript/pkgscriptstruct"
	"github.com/andrewchambers/pkgscript/pkgscripttest"
)

func init() {
	// The tests make extensive use of these not-yet-standard features.
	resolve.AllowLambda = true
	resolve.AllowNestedDef = true
	resolve.AllowFloat = true
	resolve.AllowSet = true
}

func Test(t *testing.T) {
	testdata := pkgscripttest.DataFile("pkgscriptstruct", ".")
	thread := &pkgscript.Thread{Load: load}
	pkgscripttest.SetReporter(thread, t)
	filename := filepath.Join(testdata, "testdata/struct.star")
	predeclared := pkgscript.StringDict{
		"struct": pkgscript.NewBuiltin("struct", pkgscriptstruct.Make),
		"gensym": pkgscript.NewBuiltin("gensym", gensym),
	}
	if _, err := pkgscript.ExecFile(thread, filename, nil, predeclared); err != nil {
		if err, ok := err.(*pkgscript.EvalError); ok {
			t.Fatal(err.Backtrace())
		}
		t.Fatal(err)
	}
}

// load implements the 'load' operation as used in the evaluator tests.
func load(thread *pkgscript.Thread, module string) (pkgscript.StringDict, error) {
	if module == "assert.star" {
		return pkgscripttest.LoadAssertModule()
	}
	return nil, fmt.Errorf("load not implemented")
}

// gensym is a built-in function that generates a unique symbol.
func gensym(thread *pkgscript.Thread, _ *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	var name string
	if err := pkgscript.UnpackArgs("gensym", args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	return &symbol{name: name}, nil
}

// A symbol is a distinct value that acts as a constructor of "branded"
// struct instances, like a class symbol in Python or a "provider" in Bazel.
type symbol struct{ name string }

var _ pkgscript.Callable = (*symbol)(nil)

func (sym *symbol) Name() string          { return sym.name }
func (sym *symbol) String() string        { return sym.name }
func (sym *symbol) Type() string          { return "symbol" }
func (sym *symbol) Freeze()               {} // immutable
func (sym *symbol) Truth() pkgscript.Bool  { return pkgscript.True }
func (sym *symbol) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", sym.Type()) }

func (sym *symbol) CallInternal(thread *pkgscript.Thread, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", sym)
	}
	return pkgscriptstruct.FromKeywords(sym, kwargs), nil
}
