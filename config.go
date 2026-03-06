// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver // import "github.com/graphaelli/execreceiver"

import (
	"errors"
	"fmt"
	"os"
	"time"

	"go.uber.org/multierr"
)

// Mode defines how the exec receiver runs commands.
type Mode string

const (
	// ModeScheduled runs the command on a configurable interval.
	ModeScheduled Mode = "scheduled"
	// ModeStreaming runs the command continuously, restarting on exit.
	ModeStreaming Mode = "streaming"
)

// Config defines configuration for the exec receiver.
type Config struct {
	// Command is the command and arguments to execute.
	// The first element is the program, remaining elements are arguments.
	Command []string `mapstructure:"command"`

	// Mode controls how the command is executed.
	// "scheduled": run on interval (default). "streaming": run continuously.
	Mode Mode `mapstructure:"mode"`

	// Interval is the time between scheduled executions. Default: 60s.
	// Only used in scheduled mode.
	Interval time.Duration `mapstructure:"interval"`

	// ExecTimeout is the maximum duration a scheduled execution may run.
	// If exceeded, the process is killed. Default: 0 (no timeout).
	// Only used in scheduled mode.
	ExecTimeout time.Duration `mapstructure:"exec_timeout"`

	// IncludeStderr controls whether stderr is captured. Default: true.
	IncludeStderr bool `mapstructure:"include_stderr"`

	// MaxBufferSize is the maximum buffer size in bytes for a single line
	// of output. Default: 1048576 (1MB).
	MaxBufferSize int `mapstructure:"max_buffer_size"`

	// Environment is a map of environment variables to set for the command.
	// Added to the current process environment unless ClearEnvironment is true.
	Environment map[string]string `mapstructure:"environment"`

	// WorkingDirectory sets the working directory for the command.
	// Default: inherit from collector process.
	WorkingDirectory string `mapstructure:"working_directory"`

	// ClearEnvironment starts with an empty environment when true.
	// Default: false.
	ClearEnvironment bool `mapstructure:"clear_environment"`

	// RestartDelay is the delay before restarting a streaming command
	// that has exited. Default: 1s. Only used in streaming mode.
	RestartDelay time.Duration `mapstructure:"restart_delay"`
}

var (
	errEmptyCommand          = errors.New("command must not be empty")
	errInvalidMode           = errors.New("mode must be 'scheduled' or 'streaming'")
	errIntervalTooSmall      = errors.New("interval must be at least 1s")
	errMaxBufferSizeTooSmall = errors.New("max_buffer_size must be at least 1024 bytes")
	errRestartDelayNegative  = errors.New("restart_delay must not be negative")
)

// Validate checks the configuration for errors.
func (cfg *Config) Validate() error {
	var errs error

	if len(cfg.Command) == 0 {
		errs = multierr.Append(errs, errEmptyCommand)
	}

	if cfg.Mode != ModeScheduled && cfg.Mode != ModeStreaming {
		errs = multierr.Append(errs, errInvalidMode)
	}

	if cfg.Mode == ModeScheduled && cfg.Interval < time.Second {
		errs = multierr.Append(errs, errIntervalTooSmall)
	}

	if cfg.MaxBufferSize < 1024 {
		errs = multierr.Append(errs, errMaxBufferSizeTooSmall)
	}

	if cfg.RestartDelay < 0 {
		errs = multierr.Append(errs, errRestartDelayNegative)
	}

	if cfg.WorkingDirectory != "" {
		info, err := os.Stat(cfg.WorkingDirectory)
		if err != nil {
			errs = multierr.Append(errs, fmt.Errorf("working_directory: %w", err))
		} else if !info.IsDir() {
			errs = multierr.Append(errs, fmt.Errorf("working_directory %q is not a directory", cfg.WorkingDirectory))
		}
	}

	return errs
}
