// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package execreceiver // import "github.com/graphaelli/execreceiver"

import (
	"os"
	"os/exec"
)

// cancelFunc returns a cancel function that sends SIGINT to the process.
func cancelFunc(cmd *exec.Cmd) func() error {
	return func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
}
