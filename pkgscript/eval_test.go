// Copyright 2017 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkgscript_test

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/andrewchambers/pkgscript/internal/chunkedfile"
	"github.com/andrewchambers/pkgscript/resolve"
	"github.com/andrewchambers/pkgscript/pkgscript"
	"github.com/andrewchambers/pkgscript/pkgscripttest"
	"github.com/andrewchambers/pkgscript/syntax"
)

// A test may enable non-standard options by containing (e.g.) "option:recursion".
func setOptions(src string) {
	resolve.AllowFloat = option(src, "float")
	resolve.AllowGlobalReassign = option(src, "globalreassign")
	resolve.LoadBindsGlobally = option(src, "loadbindsglobally")
	resolve.AllowLambda = option(src, "lambda")
	resolve.AllowNestedDef = option(src, "nesteddef")
	resolve.AllowRecursion = option(src, "recursion")
	resolve.AllowSet = option(src, "set")
}

func option(chunk, name string) bool {
	return strings.Contains(chunk, "option:"+name)
}

func TestEvalExpr(t *testing.T) {
	// This is mostly redundant with the new *.star tests.
	// TODO(adonovan): move checks into *.star files and
	// reduce this to a mere unit test of pkgscript.Eval.
	thread := new(pkgscript.Thread)
	for _, test := range []struct{ src, want string }{
		{`123`, `123`},
		{`-1`, `-1`},
		{`"a"+"b"`, `"ab"`},
		{`1+2`, `3`},

		// lists
		{`[]`, `[]`},
		{`[1]`, `[1]`},
		{`[1,]`, `[1]`},
		{`[1, 2]`, `[1, 2]`},
		{`[2 * x for x in [1, 2, 3]]`, `[2, 4, 6]`},
		{`[2 * x for x in [1, 2, 3] if x > 1]`, `[4, 6]`},
		{`[(x, y) for x in [1, 2] for y in [3, 4]]`,
			`[(1, 3), (1, 4), (2, 3), (2, 4)]`},
		{`[(x, y) for x in [1, 2] if x == 2 for y in [3, 4]]`,
			`[(2, 3), (2, 4)]`},
		// tuples
		{`()`, `()`},
		{`(1)`, `1`},
		{`(1,)`, `(1,)`},
		{`(1, 2)`, `(1, 2)`},
		{`(1, 2, 3, 4, 5)`, `(1, 2, 3, 4, 5)`},
		{`1, 2`, `(1, 2)`},
		// dicts
		{`{}`, `{}`},
		{`{"a": 1}`, `{"a": 1}`},
		{`{"a": 1,}`, `{"a": 1}`},

		// conditional
		{`1 if 3 > 2 else 0`, `1`},
		{`1 if "foo" else 0`, `1`},
		{`1 if "" else 0`, `0`},

		// indexing
		{`["a", "b"][0]`, `"a"`},
		{`["a", "b"][1]`, `"b"`},
		{`("a", "b")[0]`, `"a"`},
		{`("a", "b")[1]`, `"b"`},
		{`"aΩb"[0]`, `"a"`},
		{`"aΩb"[1]`, `"\xce"`},
		{`"aΩb"[3]`, `"b"`},
		{`{"a": 1}["a"]`, `1`},
		{`{"a": 1}["b"]`, `key "b" not in dict`},
		{`{}[[]]`, `unhashable type: list`},
		{`{"a": 1}[[]]`, `unhashable type: list`},
		{`[x for x in range(3)]`, "[0, 1, 2]"},
	} {
		var got string
		if v, err := pkgscript.Eval(thread, "<expr>", test.src, nil); err != nil {
			got = err.Error()
		} else {
			got = v.String()
		}
		if got != test.want {
			t.Errorf("eval %s = %s, want %s", test.src, got, test.want)
		}
	}
}

func TestExecFile(t *testing.T) {
	defer setOptions("")
	testdata := pkgscripttest.DataFile("pkgscript", ".")
	thread := &pkgscript.Thread{Load: load}
	pkgscripttest.SetReporter(thread, t)
	for _, file := range []string{
		"testdata/assign.star",
		"testdata/bool.star",
		"testdata/builtins.star",
		"testdata/control.star",
		"testdata/dict.star",
		"testdata/float.star",
		"testdata/function.star",
		"testdata/int.star",
		"testdata/list.star",
		"testdata/misc.star",
		"testdata/set.star",
		"testdata/string.star",
		"testdata/tuple.star",
		"testdata/recursion.star",
		"testdata/module.star",
	} {
		filename := filepath.Join(testdata, file)
		for _, chunk := range chunkedfile.Read(filename, t) {
			predeclared := pkgscript.StringDict{
				"hasfields": pkgscript.NewBuiltin("hasfields", newHasFields),
				"fibonacci": fib{},
			}

			setOptions(chunk.Source)
			resolve.AllowLambda = true // used extensively

			_, err := pkgscript.ExecFile(thread, filename, chunk.Source, predeclared)
			switch err := err.(type) {
			case *pkgscript.EvalError:
				found := false
				for i := range err.CallStack {
					posn := err.CallStack.At(i).Pos
					if posn.Filename() == filename {
						chunk.GotError(int(posn.Line), err.Error())
						found = true
						break
					}
				}
				if !found {
					t.Error(err.Backtrace())
				}
			case nil:
				// success
			default:
				t.Errorf("\n%s", err)
			}
			chunk.Done()
		}
	}
}

// A fib is an iterable value representing the infinite Fibonacci sequence.
type fib struct{}

func (t fib) Freeze()                    {}
func (t fib) String() string             { return "fib" }
func (t fib) Type() string               { return "fib" }
func (t fib) Truth() pkgscript.Bool       { return true }
func (t fib) Hash() (uint32, error)      { return 0, fmt.Errorf("fib is unhashable") }
func (t fib) Iterate() pkgscript.Iterator { return &fibIterator{0, 1} }

type fibIterator struct{ x, y int }

func (it *fibIterator) Next(p *pkgscript.Value) bool {
	*p = pkgscript.MakeInt(it.x)
	it.x, it.y = it.y, it.x+it.y
	return true
}
func (it *fibIterator) Done() {}

// load implements the 'load' operation as used in the evaluator tests.
func load(thread *pkgscript.Thread, module string) (pkgscript.StringDict, error) {
	if module == "assert.star" {
		return pkgscripttest.LoadAssertModule()
	}

	// TODO(adonovan): test load() using this execution path.
	filename := filepath.Join(filepath.Dir(thread.CallFrame(0).Pos.Filename()), module)
	return pkgscript.ExecFile(thread, filename, nil, nil)
}

func newHasFields(thread *pkgscript.Thread, b *pkgscript.Builtin, args pkgscript.Tuple, kwargs []pkgscript.Tuple) (pkgscript.Value, error) {
	if len(args)+len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected arguments", b.Name())
	}
	return &hasfields{attrs: make(map[string]pkgscript.Value)}, nil
}

// hasfields is a test-only implementation of HasAttrs.
// It permits any field to be set.
// Clients will likely want to provide their own implementation,
// so we don't have any public implementation.
type hasfields struct {
	attrs  pkgscript.StringDict
	frozen bool
}

var (
	_ pkgscript.HasAttrs  = (*hasfields)(nil)
	_ pkgscript.HasBinary = (*hasfields)(nil)
)

func (hf *hasfields) String() string        { return "hasfields" }
func (hf *hasfields) Type() string          { return "hasfields" }
func (hf *hasfields) Truth() pkgscript.Bool  { return true }
func (hf *hasfields) Hash() (uint32, error) { return 42, nil }

func (hf *hasfields) Freeze() {
	if !hf.frozen {
		hf.frozen = true
		for _, v := range hf.attrs {
			v.Freeze()
		}
	}
}

func (hf *hasfields) Attr(name string) (pkgscript.Value, error) { return hf.attrs[name], nil }

func (hf *hasfields) SetField(name string, val pkgscript.Value) error {
	if hf.frozen {
		return fmt.Errorf("cannot set field on a frozen hasfields")
	}
	if strings.HasPrefix(name, "no") { // for testing
		return pkgscript.NoSuchAttrError(fmt.Sprintf("no .%s field", name))
	}
	hf.attrs[name] = val
	return nil
}

func (hf *hasfields) AttrNames() []string {
	names := make([]string, 0, len(hf.attrs))
	for key := range hf.attrs {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

func (hf *hasfields) Binary(op syntax.Token, y pkgscript.Value, side pkgscript.Side) (pkgscript.Value, error) {
	// This method exists so we can exercise 'list += x'
	// where x is not Iterable but defines list+x.
	if op == syntax.PLUS {
		if _, ok := y.(*pkgscript.List); ok {
			return pkgscript.MakeInt(42), nil // list+hasfields is 42
		}
	}
	return nil, nil
}

func TestParameterPassing(t *testing.T) {
	const filename = "parameters.go"
	const src = `
def a():
	return
def b(a, b):
	return a, b
def c(a, b=42):
	return a, b
def d(*args):
	return args
def e(**kwargs):
	return kwargs
def f(a, b=42, *args, **kwargs):
	return a, b, args, kwargs
def g(a, b=42, *args, c=123, **kwargs):
	return a, b, args, c, kwargs
def h(a, b=42, *, c=123, **kwargs):
	return a, b, c, kwargs
def i(a, b=42, *, c, d=123, e, **kwargs):
	return a, b, c, d, e, kwargs
def j(a, b=42, *args, c, d=123, e, **kwargs):
	return a, b, args, c, d, e, kwargs
`

	thread := new(pkgscript.Thread)
	globals, err := pkgscript.ExecFile(thread, filename, src, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct{ src, want string }{
		// a()
		{`a()`, `None`},
		{`a(1)`, `function a accepts no arguments (1 given)`},

		// b(a, b)
		{`b()`, `function b missing 2 arguments (a, b)`},
		{`b(1)`, `function b missing 1 argument (b)`},
		{`b(a=1)`, `function b missing 1 argument (b)`},
		{`b(b=1)`, `function b missing 1 argument (a)`},
		{`b(1, 2)`, `(1, 2)`},
		{`b`, `<function b>`}, // asserts that b's parameter b was treated as a local variable
		{`b(1, 2, 3)`, `function b accepts 2 positional arguments (3 given)`},
		{`b(1, b=2)`, `(1, 2)`},
		{`b(1, a=2)`, `function b got multiple values for parameter "a"`},
		{`b(1, x=2)`, `function b got an unexpected keyword argument "x"`},
		{`b(a=1, b=2)`, `(1, 2)`},
		{`b(b=1, a=2)`, `(2, 1)`},
		{`b(b=1, a=2, x=1)`, `function b got an unexpected keyword argument "x"`},
		{`b(x=1, b=1, a=2)`, `function b got an unexpected keyword argument "x"`},

		// c(a, b=42)
		{`c()`, `function c missing 1 argument (a)`},
		{`c(1)`, `(1, 42)`},
		{`c(1, 2)`, `(1, 2)`},
		{`c(1, 2, 3)`, `function c accepts at most 2 positional arguments (3 given)`},
		{`c(1, b=2)`, `(1, 2)`},
		{`c(1, a=2)`, `function c got multiple values for parameter "a"`},
		{`c(a=1, b=2)`, `(1, 2)`},
		{`c(b=1, a=2)`, `(2, 1)`},

		// d(*args)
		{`d()`, `()`},
		{`d(1)`, `(1,)`},
		{`d(1, 2)`, `(1, 2)`},
		{`d(1, 2, k=3)`, `function d got an unexpected keyword argument "k"`},
		{`d(args=[])`, `function d got an unexpected keyword argument "args"`},

		// e(**kwargs)
		{`e()`, `{}`},
		{`e(1)`, `function e accepts 0 positional arguments (1 given)`},
		{`e(k=1)`, `{"k": 1}`},
		{`e(kwargs={})`, `{"kwargs": {}}`},

		// f(a, b=42, *args, **kwargs)
		{`f()`, `function f missing 1 argument (a)`},
		{`f(0)`, `(0, 42, (), {})`},
		{`f(0)`, `(0, 42, (), {})`},
		{`f(0, 1)`, `(0, 1, (), {})`},
		{`f(0, 1, 2)`, `(0, 1, (2,), {})`},
		{`f(0, 1, 2, 3)`, `(0, 1, (2, 3), {})`},
		{`f(a=0)`, `(0, 42, (), {})`},
		{`f(0, b=1)`, `(0, 1, (), {})`},
		{`f(0, a=1)`, `function f got multiple values for parameter "a"`},
		{`f(0, b=1, c=2)`, `(0, 1, (), {"c": 2})`},
		{`f(0, 1, x=2, *[3, 4], y=5, **dict(z=6))`, // github.com/google/skylark/issues/135
			`(0, 1, (3, 4), {"x": 2, "y": 5, "z": 6})`},

		// g(a, b=42, *args, c=123, **kwargs)
		{`g()`, `function g missing 1 argument (a)`},
		{`g(0)`, `(0, 42, (), 123, {})`},
		{`g(0, 1)`, `(0, 1, (), 123, {})`},
		{`g(0, 1, 2)`, `(0, 1, (2,), 123, {})`},
		{`g(0, 1, 2, 3)`, `(0, 1, (2, 3), 123, {})`},
		{`g(a=0)`, `(0, 42, (), 123, {})`},
		{`g(0, b=1)`, `(0, 1, (), 123, {})`},
		{`g(0, a=1)`, `function g got multiple values for parameter "a"`},
		{`g(0, b=1, c=2, d=3)`, `(0, 1, (), 2, {"d": 3})`},
		{`g(0, 1, x=2, *[3, 4], y=5, **dict(z=6))`,
			`(0, 1, (3, 4), 123, {"x": 2, "y": 5, "z": 6})`},

		// h(a, b=42, *, c=123, **kwargs)
		{`h()`, `function h missing 1 argument (a)`},
		{`h(0)`, `(0, 42, 123, {})`},
		{`h(0, 1)`, `(0, 1, 123, {})`},
		{`h(0, 1, 2)`, `function h accepts at most 2 positional arguments (3 given)`},
		{`h(a=0)`, `(0, 42, 123, {})`},
		{`h(0, b=1)`, `(0, 1, 123, {})`},
		{`h(0, a=1)`, `function h got multiple values for parameter "a"`},
		{`h(0, b=1, c=2)`, `(0, 1, 2, {})`},
		{`h(0, b=1, d=2)`, `(0, 1, 123, {"d": 2})`},
		{`h(0, b=1, c=2, d=3)`, `(0, 1, 2, {"d": 3})`},
		{`h(0, b=1, c=2, d=3)`, `(0, 1, 2, {"d": 3})`},

		// i(a, b=42, *, c, d=123, e, **kwargs)
		{`i()`, `function i missing 3 arguments (a, c, e)`},
		{`i(0)`, `function i missing 2 arguments (c, e)`},
		{`i(0, 1)`, `function i missing 2 arguments (c, e)`},
		{`i(0, 1, 2)`, `function i accepts at most 2 positional arguments (3 given)`},
		{`i(0, 1, e=2)`, `function i missing 1 argument (c)`},
		{`i(0, 1, 2, 3)`, `function i accepts at most 2 positional arguments (4 given)`},
		{`i(a=0)`, `function i missing 2 arguments (c, e)`},
		{`i(0, b=1)`, `function i missing 2 arguments (c, e)`},
		{`i(0, a=1)`, `function i got multiple values for parameter "a"`},
		{`i(0, b=1, c=2)`, `function i missing 1 argument (e)`},
		{`i(0, b=1, d=2)`, `function i missing 2 arguments (c, e)`},
		{`i(0, b=1, c=2, d=3)`, `function i missing 1 argument (e)`},
		{`i(0, b=1, c=2, d=3, e=4)`, `(0, 1, 2, 3, 4, {})`},
		{`i(0, 1, b=1, c=2, d=3, e=4)`, `function i got multiple values for parameter "b"`},

		// j(a, b=42, *args, c, d=123, e, **kwargs)
		{`j()`, `function j missing 3 arguments (a, c, e)`},
		{`j(0)`, `function j missing 2 arguments (c, e)`},
		{`j(0, 1)`, `function j missing 2 arguments (c, e)`},
		{`j(0, 1, 2)`, `function j missing 2 arguments (c, e)`},
		{`j(0, 1, e=2)`, `function j missing 1 argument (c)`},
		{`j(0, 1, 2, 3)`, `function j missing 2 arguments (c, e)`},
		{`j(a=0)`, `function j missing 2 arguments (c, e)`},
		{`j(0, b=1)`, `function j missing 2 arguments (c, e)`},
		{`j(0, a=1)`, `function j got multiple values for parameter "a"`},
		{`j(0, b=1, c=2)`, `function j missing 1 argument (e)`},
		{`j(0, b=1, d=2)`, `function j missing 2 arguments (c, e)`},
		{`j(0, b=1, c=2, d=3)`, `function j missing 1 argument (e)`},
		{`j(0, b=1, c=2, d=3, e=4)`, `(0, 1, (), 2, 3, 4, {})`},
		{`j(0, 1, b=1, c=2, d=3, e=4)`, `function j got multiple values for parameter "b"`},
		{`j(0, 1, 2, c=3, e=4)`, `(0, 1, (2,), 3, 123, 4, {})`},
	} {
		var got string
		if v, err := pkgscript.Eval(thread, "<expr>", test.src, globals); err != nil {
			got = err.Error()
		} else {
			got = v.String()
		}
		if got != test.want {
			t.Errorf("eval %s = %s, want %s", test.src, got, test.want)
		}
	}
}

// TestPrint ensures that the Starlark print function calls
// Thread.Print, if provided.
func TestPrint(t *testing.T) {
	const src = `
print("hello")
def f(): print("hello", "world", sep=", ")
f()
`
	buf := new(bytes.Buffer)
	print := func(thread *pkgscript.Thread, msg string) {
		caller := thread.CallFrame(1)
		fmt.Fprintf(buf, "%s: %s: %s\n", caller.Pos, caller.Name, msg)
	}
	thread := &pkgscript.Thread{Print: print}
	if _, err := pkgscript.ExecFile(thread, "foo.star", src, nil); err != nil {
		t.Fatal(err)
	}
	want := "foo.star:2:6: <toplevel>: hello\n" +
		"foo.star:3:15: f: hello, world\n"
	if got := buf.String(); got != want {
		t.Errorf("output was %s, want %s", got, want)
	}
}

func reportEvalError(tb testing.TB, err error) {
	if err, ok := err.(*pkgscript.EvalError); ok {
		tb.Fatal(err.Backtrace())
	}
	tb.Fatal(err)
}

// TestInt exercises the Int.Int64 and Int.Uint64 methods.
// If we can move their logic into math/big, delete this test.
func TestInt(t *testing.T) {
	one := pkgscript.MakeInt(1)

	for _, test := range []struct {
		i          pkgscript.Int
		wantInt64  string
		wantUint64 string
	}{
		{pkgscript.MakeInt64(math.MinInt64).Sub(one), "error", "error"},
		{pkgscript.MakeInt64(math.MinInt64), "-9223372036854775808", "error"},
		{pkgscript.MakeInt64(-1), "-1", "error"},
		{pkgscript.MakeInt64(0), "0", "0"},
		{pkgscript.MakeInt64(1), "1", "1"},
		{pkgscript.MakeInt64(math.MaxInt64), "9223372036854775807", "9223372036854775807"},
		{pkgscript.MakeUint64(math.MaxUint64), "error", "18446744073709551615"},
		{pkgscript.MakeUint64(math.MaxUint64).Add(one), "error", "error"},
	} {
		gotInt64, gotUint64 := "error", "error"
		if i, ok := test.i.Int64(); ok {
			gotInt64 = fmt.Sprint(i)
		}
		if u, ok := test.i.Uint64(); ok {
			gotUint64 = fmt.Sprint(u)
		}
		if gotInt64 != test.wantInt64 {
			t.Errorf("(%s).Int64() = %s, want %s", test.i, gotInt64, test.wantInt64)
		}
		if gotUint64 != test.wantUint64 {
			t.Errorf("(%s).Uint64() = %s, want %s", test.i, gotUint64, test.wantUint64)
		}
	}
}

func TestBacktrace(t *testing.T) {
	getBacktrace := func(err error) string {
		switch err := err.(type) {
		case *pkgscript.EvalError:
			return err.Backtrace()
		case nil:
			t.Fatalf("ExecFile succeeded unexpectedly")
		default:
			t.Fatalf("ExecFile failed with %v, wanted *EvalError", err)
		}
		panic("unreachable")
	}

	// This test ensures continuity of the stack of active Starlark
	// functions, including propagation through built-ins such as 'min'.
	const src = `
def f(x): return 1//x
def g(x): f(x)
def h(): return min([1, 2, 0], key=g)
def i(): return h()
i()
`
	thread := new(pkgscript.Thread)
	_, err := pkgscript.ExecFile(thread, "crash.star", src, nil)
	// Compiled code currently has no column information.
	const want = `Traceback (most recent call last):
  crash.star:6:2: in <toplevel>
  crash.star:5:18: in i
  crash.star:4:20: in h
  <builtin>: in min
  crash.star:3:12: in g
  crash.star:2:19: in f
Error: floored division by zero`
	if got := getBacktrace(err); got != want {
		t.Errorf("error was %s, want %s", got, want)
	}

	// Additionally, ensure that errors originating in
	// Starlark and/or Go each have an accurate frame.
	//
	// This program fails in Starlark (f) if x==0,
	// or in Go (string.join) if x is non-zero.
	const src2 = `
def f(): ''.join([1//i])
f()
`
	for i, want := range []string{
		0: `Traceback (most recent call last):
  crash.star:3:2: in <toplevel>
  crash.star:2:20: in f
Error: floored division by zero`,
		1: `Traceback (most recent call last):
  crash.star:3:2: in <toplevel>
  crash.star:2:17: in f
  <builtin>: in join
Error: join: in list, want string, got int`,
	} {
		globals := pkgscript.StringDict{"i": pkgscript.MakeInt(i)}
		_, err := pkgscript.ExecFile(thread, "crash.star", src2, globals)
		if got := getBacktrace(err); got != want {
			t.Errorf("error was %s, want %s", got, want)
		}
	}
}

// TestRepeatedExec parses and resolves a file syntax tree once then
// executes it repeatedly with different values of its predeclared variables.
func TestRepeatedExec(t *testing.T) {
	predeclared := pkgscript.StringDict{"x": pkgscript.None}
	_, prog, err := pkgscript.SourceProgram("repeat.star", "y = 2 * x", predeclared.Has)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		x, want pkgscript.Value
	}{
		{x: pkgscript.MakeInt(42), want: pkgscript.MakeInt(84)},
		{x: pkgscript.String("mur"), want: pkgscript.String("murmur")},
		{x: pkgscript.Tuple{pkgscript.None}, want: pkgscript.Tuple{pkgscript.None, pkgscript.None}},
	} {
		predeclared["x"] = test.x // update the values in dictionary
		thread := new(pkgscript.Thread)
		if globals, err := prog.Init(thread, predeclared); err != nil {
			t.Errorf("x=%v: %v", test.x, err) // exec error
		} else if eq, err := pkgscript.Equal(globals["y"], test.want); err != nil {
			t.Errorf("x=%v: %v", test.x, err) // comparison error
		} else if !eq {
			t.Errorf("x=%v: got y=%v, want %v", test.x, globals["y"], test.want)
		}
	}
}

// TestEmptyFilePosition ensures that even Programs
// from empty files have a valid position.
func TestEmptyPosition(t *testing.T) {
	var predeclared pkgscript.StringDict
	for _, content := range []string{"", "empty = False"} {
		_, prog, err := pkgscript.SourceProgram("hello.star", content, predeclared.Has)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := prog.Filename(), "hello.star"; got != want {
			t.Errorf("Program.Filename() = %q, want %q", got, want)
		}
	}
}

// TestUnpackUserDefined tests that user-defined
// implementations of pkgscript.Value may be unpacked.
func TestUnpackUserDefined(t *testing.T) {
	// success
	want := new(hasfields)
	var x *hasfields
	if err := pkgscript.UnpackArgs("unpack", pkgscript.Tuple{want}, nil, "x", &x); err != nil {
		t.Errorf("UnpackArgs failed: %v", err)
	}
	if x != want {
		t.Errorf("for x, got %v, want %v", x, want)
	}

	// failure
	err := pkgscript.UnpackArgs("unpack", pkgscript.Tuple{pkgscript.MakeInt(42)}, nil, "x", &x)
	if want := "unpack: for parameter x: got int, want hasfields"; fmt.Sprint(err) != want {
		t.Errorf("unpack args error = %q, want %q", err, want)
	}
}

func TestDocstring(t *testing.T) {
	globals, _ := pkgscript.ExecFile(&pkgscript.Thread{}, "doc.star", `
def somefunc():
	"somefunc doc"
	return 0
`, nil)

	if globals["somefunc"].(*pkgscript.Function).Doc() != "somefunc doc" {
		t.Fatal("docstring not found")
	}
}

func TestFrameLocals(t *testing.T) {
	// trace prints a nice stack trace including argument
	// values of calls to Starlark functions.
	trace := func(thread *pkgscript.Thread) string {
		buf := new(bytes.Buffer)
		for i := 0; i < thread.CallStackDepth(); i++ {
			fr := thread.DebugFrame(i)
			fmt.Fprintf(buf, "%s(", fr.Callable().Name())
			if fn, ok := fr.Callable().(*pkgscript.Function); ok {
				for i := 0; i < fn.NumParams(); i++ {
					if i > 0 {
						buf.WriteString(", ")
					}
					name, _ := fn.Param(i)
					fmt.Fprintf(buf, "%s=%s", name, fr.Local(i))
				}
			} else {
				buf.WriteString("...") // a built-in function
			}
			buf.WriteString(")\n")
		}
		return buf.String()
	}

	var got string
	builtin := func(thread *pkgscript.Thread, _ *pkgscript.Builtin, _ pkgscript.Tuple, _ []pkgscript.Tuple) (pkgscript.Value, error) {
		got = trace(thread)
		return pkgscript.None, nil
	}
	predeclared := pkgscript.StringDict{
		"builtin": pkgscript.NewBuiltin("builtin", builtin),
	}
	_, err := pkgscript.ExecFile(&pkgscript.Thread{}, "foo.star", `
def f(x, y): builtin()
def g(z): f(z, z*z)
g(7)
`, predeclared)
	if err != nil {
		t.Errorf("ExecFile failed: %v", err)
	}

	var want = `
builtin(...)
f(x=7, y=49)
g(z=7)
<toplevel>()
`[1:]
	if got != want {
		t.Errorf("got <<%s>>, want <<%s>>", got, want)
	}
}

type badType string

func (b *badType) String() string        { return "badType" }
func (b *badType) Type() string          { return "badType:" + string(*b) } // panics if b==nil
func (b *badType) Truth() pkgscript.Bool  { return true }
func (b *badType) Hash() (uint32, error) { return 0, nil }
func (b *badType) Freeze()               {}

var _ pkgscript.Value = new(badType)

// TestUnpackErrorBadType verifies that the Unpack functions fail
// gracefully when a parameter's default value's Type method panics.
func TestUnpackErrorBadType(t *testing.T) {
	for _, test := range []struct {
		x    *badType
		want string
	}{
		{new(badType), "got NoneType, want badType"},       // Starlark type name
		{nil, "got NoneType, want *pkgscript_test.badType"}, // Go type name
	} {
		err := pkgscript.UnpackArgs("f", pkgscript.Tuple{pkgscript.None}, nil, "x", &test.x)
		if err == nil {
			t.Errorf("UnpackArgs succeeded unexpectedly")
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("UnpackArgs error %q does not contain %q", err, test.want)
		}
	}
}
