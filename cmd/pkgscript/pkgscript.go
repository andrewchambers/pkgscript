// Copyright 2017 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The pkgscript command interprets a Starlark file.
// With no arguments, it starts a read-eval-print loop (REPL).
package main // import "github.com/andrewchambers/pkgscript/cmd/pkgscript"

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/andrewchambers/pkgscript/internal/compile"
	"github.com/andrewchambers/pkgscript/repl"
	"github.com/andrewchambers/pkgscript/resolve"
	"github.com/andrewchambers/pkgscript/pkgscript"
)

// flags
var (
	cpuprofile = flag.String("cpuprofile", "", "gather Go CPU profile in this file")
	memprofile = flag.String("memprofile", "", "gather Go memory profile in this file")
	profile    = flag.String("profile", "", "gather Starlark time profile in this file")
	showenv    = flag.Bool("showenv", false, "on success, print final global environment")
	execprog   = flag.String("c", "", "execute program `prog`")
)

func init() {
	flag.BoolVar(&compile.Disassemble, "disassemble", compile.Disassemble, "show disassembly during compilation of each function")

	// non-standard dialect flags
	flag.BoolVar(&resolve.AllowFloat, "float", resolve.AllowFloat, "allow floating-point numbers")
	flag.BoolVar(&resolve.AllowSet, "set", resolve.AllowSet, "allow set data type")
	flag.BoolVar(&resolve.AllowLambda, "lambda", resolve.AllowLambda, "allow lambda expressions")
	flag.BoolVar(&resolve.AllowNestedDef, "nesteddef", resolve.AllowNestedDef, "allow nested def statements")
	flag.BoolVar(&resolve.AllowRecursion, "recursion", resolve.AllowRecursion, "allow while statements and recursive functions")
	flag.BoolVar(&resolve.AllowGlobalReassign, "globalreassign", resolve.AllowGlobalReassign, "allow reassignment of globals, and if/for/while statements at top level")
}

func main() {
	os.Exit(doMain())
}

func doMain() int {
	log.SetPrefix("pkgscript: ")
	log.SetFlags(0)
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		check(err)
		err = pprof.StartCPUProfile(f)
		check(err)
		defer func() {
			pprof.StopCPUProfile()
			err := f.Close()
			check(err)
		}()
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		check(err)
		defer func() {
			runtime.GC()
			err := pprof.Lookup("heap").WriteTo(f, 0)
			check(err)
			err = f.Close()
			check(err)
		}()
	}

	if *profile != "" {
		f, err := os.Create(*profile)
		check(err)
		err = pkgscript.StartProfile(f)
		check(err)
		defer func() {
			err := pkgscript.StopProfile()
			check(err)
		}()
	}

	thread := &pkgscript.Thread{Load: repl.MakeLoad()}
	globals := make(pkgscript.StringDict)

	switch {
	case flag.NArg() == 1 || *execprog != "":
		var (
			filename string
			src      interface{}
			err      error
		)
		if *execprog != "" {
			// Execute provided program.
			filename = "cmdline"
			src = *execprog
		} else {
			// Execute specified file.
			filename = flag.Arg(0)
		}
		thread.Name = "exec " + filename
		globals, err = pkgscript.ExecFile(thread, filename, src, nil)
		if err != nil {
			repl.PrintError(err)
			return 1
		}
	case flag.NArg() == 0:
		fmt.Println("Welcome to Starlark (github.com/andrewchambers/pkgscript)")
		thread.Name = "REPL"
		repl.REPL(thread, globals)
		return 0
	default:
		log.Print("want at most one Starlark file name")
		return 1
	}

	// Print the global environment.
	if *showenv {
		for _, name := range globals.Keys() {
			if !strings.HasPrefix(name, "_") {
				fmt.Fprintf(os.Stderr, "%s = %s\n", name, globals[name])
			}
		}
	}

	return 0
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
