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

	// MaxConcurrent is the maximum number of concurrent command executions
	// allowed in scheduled mode. If a tick fires and the limit is reached,
	// that execution is skipped and a warning is logged. Default: 1.
	// Only used in scheduled mode.
	MaxConcurrent int `mapstructure:"max_concurrent"`

	// IncludeStderr controls whether stderr is captured. Default: true.
	IncludeStderr bool `mapstructure:"include_stderr"`

	// MaxBufferSize is the maximum buffer size in bytes for a single line
	// of output. Default: 1048576 (1MB).
	MaxBufferSize int `mapstructure:"max_buffer_size"`

	// Environment is a map of environment variables to set for the command.
	// When InheritEnvironment is false (default), only these variables are set.
	// When InheritEnvironment is true, these are added to the current process environment.
	Environment map[string]string `mapstructure:"environment"`

	// WorkingDirectory sets the working directory for the command.
	// Default: inherit from collector process.
	WorkingDirectory string `mapstructure:"working_directory"`

	// InheritEnvironment inherits the collector's environment when true.
	// Default: false (start with a clean environment).
	InheritEnvironment bool `mapstructure:"inherit_environment"`

	// MaxOutputSize is the maximum total bytes buffered per scheduled
	// execution in readLines. When exceeded, reading stops and the
	// output is truncated. Default: 10485760 (10MB). 0 means no limit.
	// Only used in scheduled mode.
	MaxOutputSize int `mapstructure:"max_output_size"`

	// RestartDelay is the delay before restarting a streaming command
	// that has exited. Default: 1s. Only used in streaming mode.
	RestartDelay time.Duration `mapstructure:"restart_delay"`

	// MaxRestartDelay is the maximum backoff delay for streaming command
	// restarts. The restart delay doubles on each consecutive failure,
	// capped at this value. Default: 5m. Only used in streaming mode.
	MaxRestartDelay time.Duration `mapstructure:"max_restart_delay"`
}

var (
	errEmptyCommand            = errors.New("command must not be empty")
	errInvalidMode             = errors.New("mode must be 'scheduled' or 'streaming'")
	errIntervalTooSmall        = errors.New("interval must be at least 1s")
	errMaxBufferSizeTooSmall   = errors.New("max_buffer_size must be at least 1024 bytes")
	errMaxConcurrentTooSmall   = errors.New("max_concurrent must be at least 1")
	errMaxOutputSizeTooSmall   = errors.New("max_output_size must be >= max_buffer_size when set")
	errRestartDelayNegative    = errors.New("restart_delay must not be negative")
	errMaxRestartDelayTooSmall = errors.New("max_restart_delay must be >= restart_delay")
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

	if cfg.Mode == ModeScheduled && cfg.MaxConcurrent < 1 {
		errs = multierr.Append(errs, errMaxConcurrentTooSmall)
	}

	if cfg.MaxBufferSize < 1024 {
		errs = multierr.Append(errs, errMaxBufferSizeTooSmall)
	}

	if cfg.MaxOutputSize > 0 && cfg.MaxOutputSize < cfg.MaxBufferSize {
		errs = multierr.Append(errs, errMaxOutputSizeTooSmall)
	}

	if cfg.RestartDelay < 0 {
		errs = multierr.Append(errs, errRestartDelayNegative)
	}

	if cfg.MaxRestartDelay > 0 && cfg.MaxRestartDelay < cfg.RestartDelay {
		errs = multierr.Append(errs, errMaxRestartDelayTooSmall)
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
