package compile_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/andrewchambers/pkgscript/pkgscript"
)

// TestSerialization verifies that a serialized program can be loaded,
// deserialized, and executed.
func TestSerialization(t *testing.T) {
	predeclared := pkgscript.StringDict{
		"x": pkgscript.String("mur"),
		"n": pkgscript.MakeInt(2),
	}
	const src = `
def mul(a, b):
    return a * b

y = mul(x, n)
`
	_, oldProg, err := pkgscript.SourceProgram("mul.star", src, predeclared.Has)
	if err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	if err := oldProg.Write(buf); err != nil {
		t.Fatalf("oldProg.WriteTo: %v", err)
	}

	newProg, err := pkgscript.CompiledProgram(buf)
	if err != nil {
		t.Fatalf("CompiledProgram: %v", err)
	}

	thread := new(pkgscript.Thread)
	globals, err := newProg.Init(thread, predeclared)
	if err != nil {
		t.Fatalf("newProg.Init: %v", err)
	}
	if got, want := globals["y"], pkgscript.String("murmur"); got != want {
		t.Errorf("Value of global was %s, want %s", got, want)
		t.Logf("globals: %v", globals)
	}

	// Verify stack frame.
	predeclared["n"] = pkgscript.None
	_, err = newProg.Init(thread, predeclared)
	evalErr, ok := err.(*pkgscript.EvalError)
	if !ok {
		t.Fatalf("newProg.Init call returned err %v, want *EvalError", err)
	}
	const want = `Traceback (most recent call last):
  mul.star:5:8: in <toplevel>
  mul.star:3:14: in mul
Error: unknown binary op: string * NoneType`
	if got := evalErr.Backtrace(); got != want {
		t.Fatalf("got <<%s>>, want <<%s>>", got, want)
	}
}

func TestGarbage(t *testing.T) {
	const garbage = "This is not a compiled Starlark program."
	_, err := pkgscript.CompiledProgram(strings.NewReader(garbage))
	if err == nil {
		t.Fatalf("CompiledProgram did not report an error when decoding garbage")
	}
	if !strings.Contains(err.Error(), "not a compiled module") {
		t.Fatalf("CompiledProgram reported the wrong error when decoding garbage: %v", err)
	}
}
