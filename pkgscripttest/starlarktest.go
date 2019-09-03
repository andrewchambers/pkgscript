// Copyright 2017 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkgscripttest defines utilities for testing Starlark programs.
//
// Clients can call LoadAssertModule to load a module that defines
// several functions useful for testing.  See assert.star for its
// definition.
//
// The assert.error function, which reports errors to the current Go
// testing.T, requires that clients call SetTest(thread, t) before use.
package pkgscripttest // import "github.com/andrewchambers/pkgscript/pkgscripttest"

import (
	"fmt"
	"go/build"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/andrewchambers/pkgscript/pkgscript"
	"github.com/andrewchambers/pkgscript/pkgscriptstruct"
)

const localKey = "Reporter"

// A Reporter is a value to which errors may be reported.
// It is satisfied by *testing.T.
type Reporter interface {
	Error(args ...interface{})
}

// SetReporter associates an error reporter (such as a testing.T in
// a Go test) with the Starlark thread so that Starlark programs may
// report errors to it.
func SetReporter(thread *pkgscript.Thread, r Reporter) {
	thread.SetLocal(localKey, r)
}

// GetReporter returns the Starlark thread's error reporter.
// It must be preceded by a call to SetReporter.
func GetReporter(thread *pkgscript.Thread) Reporter {
	r, ok := thread.Local(localKey).(Reporter)
	if !ok {
		panic("internal error: pkgscripttest.SetReporter was not called")
	}
	return r
}

var (
	once      sync.Once
	assert    pkgscript.StringDict
	assertErr error
)

// LoadAssertModule loads the assert module.
// It is concurrency-safe and idempotent.
func LoadAssertModule() (pkgscript.StringDict, error) {
	once.Do(func() {
		predeclared := pkgscript.StringDict{
			"error":   pkgscript.NewBuiltin("error", error_),
			"catch":   pkgscript.NewBuiltin("catch", catch),
			"matches": pkgscript.NewBuiltin("matches", matches),
			"module":  pkgscript.NewBuiltin("module", pkgscriptstruct.MakeModule),
			"_freeze": pkgscript.NewBuiltin("freeze", freeze),
		}
		filename := DataFile("pkgscripttest", "assert.star")
		thread := new(pkgscript.Thread)
		assert, assertErr = pkgscript.ExecFile(thread, filename, nil, predeclared)
	})
	return assert, assertErr
}

// catch(f) evaluates f() and returns its evaluation error message
// if it failed or None if it succeeded.
func catch(thread *pkgscript.Thread, _ *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	var fn pkgscript.Callable
	if err := pkgscript.UnpackArgs("catch", args, kwargs, "fn", &fn); err != nil {
		return nil, err
	}
	if _, err := pkgscript.Call(thread, fn, nil, nil); err != nil {
		return pkgscript.String(err.Error()), nil
	}
	return pkgscript.None, nil
}

// matches(pattern, str) reports whether string str matches the regular expression pattern.
func matches(thread *pkgscript.Thread, _ *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	var pattern, str string
	if err := pkgscript.UnpackArgs("matches", args, kwargs, "pattern", &pattern, "str", &str); err != nil {
		return nil, err
	}
	ok, err := regexp.MatchString(pattern, str)
	if err != nil {
		return nil, fmt.Errorf("matches: %s", err)
	}
	return pkgscript.Bool(ok), nil
}

// error(x) reports an error to the Go test framework.
func error_(thread *pkgscript.Thread, _ *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("error: got %d arguments, want 1", len(args))
	}
	buf := new(strings.Builder)
	stk := thread.CallStack()
	stk.Pop()
	fmt.Fprintf(buf, "%sError: ", stk)
	if s, ok := pkgscript.AsString(args[0]); ok {
		buf.WriteString(s)
	} else {
		buf.WriteString(args[0].String())
	}
	GetReporter(thread).Error(buf.String())
	return pkgscript.None, nil
}

// freeze(x) freezes its operand.
func freeze(thread *pkgscript.Thread, _ *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("freeze does not accept keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("freeze got %d arguments, wants 1", len(args))
	}
	args[0].Freeze()
	return args[0], nil
}

// DataFile returns the effective filename of the specified
// test data resource.  The function abstracts differences between
// 'go build', under which a test runs in its package directory,
// and Blaze, under which a test runs in the root of the tree.
var DataFile = func(pkgdir, filename string) string {
	return filepath.Join(build.Default.GOPATH, "src/github.com/andrewchambers/pkgscript", pkgdir, filename)
}
