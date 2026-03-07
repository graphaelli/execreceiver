// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// testhelper is a cross-platform helper binary used in execreceiver tests.
// It replaces shell-specific commands (sh, sleep, seq, printf, etc.) so that
// tests run identically on Linux, macOS, and Windows.
//
// Usage: testhelper <subcommand> [args...]
//
//	echo <text...>               print args joined by space to stdout, exit 0
//	echo-stderr <text...>        print args joined by space to stderr, exit 0
//	echo-both <stdout> <stderr>  print first arg to stdout, second to stderr, exit 0
//	echo-exit <text> <code>      print text to stdout, exit with code
//	exit <code>                  exit with the given code, no output
//	sleep <ms>                   sleep for N milliseconds, exit 0
//	seq <n>                      print "line1" through "lineN" to stdout, exit 0
//	multiline <a> <b> ...        print each arg on its own line, exit 0
//	env <VAR>                    print the value of the named env var (or empty), exit 0
//	pwd                          print the working directory, exit 0
//	statefile <path>             state-machine: reads/writes an integer counter in <path>
//	                             to produce deterministic output across 4 invocations
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testhelper <subcommand> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "echo":
		fmt.Println(strings.Join(os.Args[2:], " "))

	case "echo-stderr":
		fmt.Fprintln(os.Stderr, strings.Join(os.Args[2:], " "))

	case "echo-both":
		// echo-both <stdout-text> <stderr-text>
		if len(os.Args) >= 4 {
			fmt.Println(os.Args[2])
			fmt.Fprintln(os.Stderr, os.Args[3])
		}

	case "echo-exit":
		// echo-exit <text> <code>
		if len(os.Args) >= 4 {
			fmt.Println(os.Args[2])
			code, _ := strconv.Atoi(os.Args[3])
			os.Exit(code)
		}

	case "exit":
		code, _ := strconv.Atoi(os.Args[2])
		os.Exit(code)

	case "sleep":
		ms, _ := strconv.Atoi(os.Args[2])
		time.Sleep(time.Duration(ms) * time.Millisecond)

	case "seq":
		n, _ := strconv.Atoi(os.Args[2])
		for i := 1; i <= n; i++ {
			fmt.Printf("line%d\n", i)
		}

	case "multiline":
		for _, arg := range os.Args[2:] {
			fmt.Println(arg)
		}

	case "env":
		fmt.Println(os.Getenv(os.Args[2]))

	case "pwd":
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(dir)

	case "statefile":
		// statefile <path>
		// Implements a 4-step state machine used by TestStreamingBackoffResetsAfterSuccessfulRun.
		// State is stored as an integer in the file at <path>.
		//   0 (no file): print "run1-fail", write 1, exit 1
		//   1:           print "run2-success", write 2, sleep 200ms, exit 0
		//   2:           print "run3-after-reset", write 3, exit 1
		//   3+:          print "run4-done", exit 1
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "statefile requires a path argument")
			os.Exit(1)
		}
		path := os.Args[2]
		state := readState(path)
		switch state {
		case 0:
			fmt.Println("run1-fail")
			writeState(path, 1)
			os.Exit(1)
		case 1:
			fmt.Println("run2-success")
			writeState(path, 2)
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		case 2:
			fmt.Println("run3-after-reset")
			writeState(path, 3)
			os.Exit(1)
		default:
			fmt.Println("run4-done")
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "testhelper: unknown subcommand %q\n", os.Args[1])
		os.Exit(1)
	}
}

func readState(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return n
}

func writeState(path string, state int) {
	_ = os.WriteFile(path, []byte(strconv.Itoa(state)), 0o644)
}
