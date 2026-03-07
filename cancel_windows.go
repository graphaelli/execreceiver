// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package execreceiver // import "github.com/graphaelli/execreceiver"

import "os/exec"

// cancelFunc returns a cancel function that kills the process.
// Windows does not support SIGINT for arbitrary processes; Kill is the
// portable way to terminate a child process.
func cancelFunc(cmd *exec.Cmd) func() error {
	return func() error {
		return cmd.Process.Kill()
	}
}
