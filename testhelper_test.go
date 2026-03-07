// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	initTestHelper    sync.Once
	testHelperBin     string
	testHelperBuildErr error
)

// helperCmd returns a command slice that invokes the cross-platform test helper
// binary with the given subcommand and arguments. The helper binary is compiled
// once (lazily) the first time this function is called.
//
// Example: helperCmd(t, "echo", "hello") → []string{"/tmp/.../testhelper", "echo", "hello"}
func helperCmd(tb testing.TB, args ...string) []string {
	tb.Helper()
	initTestHelper.Do(func() {
		dir, err := os.MkdirTemp("", "execreceiver-testhelper-*")
		if err != nil {
			testHelperBuildErr = fmt.Errorf("creating temp dir: %w", err)
			return
		}

		bin := filepath.Join(dir, "testhelper")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}

		cmd := exec.Command("go", "build", "-o", bin, "./internal/testhelper")
		out, err := cmd.CombinedOutput()
		if err != nil {
			testHelperBuildErr = fmt.Errorf("building testhelper: %w\n%s", err, out)
			return
		}
		testHelperBin = bin
	})

	if testHelperBuildErr != nil {
		tb.Fatalf("testhelper setup failed: %v", testHelperBuildErr)
	}
	return append([]string{testHelperBin}, args...)
}
