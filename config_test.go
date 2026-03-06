// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid scheduled",
			cfg: &Config{
				Command:       []string{"echo", "hello"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
			},
		},
		{
			name: "valid streaming",
			cfg: &Config{
				Command:       []string{"tail", "-f", "/dev/null"},
				Mode:          ModeStreaming,
				Interval:      60 * time.Second,
				MaxBufferSize: 1024 * 1024,
				RestartDelay:  time.Second,
			},
		},
		{
			name: "empty command",
			cfg: &Config{
				Command:       []string{},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
			},
			wantErr: "command must not be empty",
		},
		{
			name: "invalid mode",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          "invalid",
				Interval:      60 * time.Second,
				MaxBufferSize: 1024 * 1024,
			},
			wantErr: "mode must be 'scheduled' or 'streaming'",
		},
		{
			name: "interval too small",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      100 * time.Millisecond,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
			},
			wantErr: "interval must be at least 1s",
		},
		{
			name: "streaming mode ignores interval check",
			cfg: &Config{
				Command:       []string{"tail", "-f", "/dev/null"},
				Mode:          ModeStreaming,
				Interval:      0,
				MaxBufferSize: 1024 * 1024,
			},
		},
		{
			name: "max_concurrent too small",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 0,
				MaxBufferSize: 1024 * 1024,
			},
			wantErr: "max_concurrent must be at least 1",
		},
		{
			name: "max_concurrent ignored in streaming mode",
			cfg: &Config{
				Command:       []string{"tail", "-f", "/dev/null"},
				Mode:          ModeStreaming,
				Interval:      60 * time.Second,
				MaxConcurrent: 0,
				MaxBufferSize: 1024 * 1024,
			},
		},
		{
			name: "buffer too small",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 512,
			},
			wantErr: "max_buffer_size must be at least 1024 bytes",
		},
		{
			name: "max_output_size valid when equal to max_buffer_size",
			cfg: &Config{
				Command:       []string{"echo", "hello"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
				MaxOutputSize: 1024 * 1024,
			},
		},
		{
			name: "max_output_size valid when zero (unlimited)",
			cfg: &Config{
				Command:       []string{"echo", "hello"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
				MaxOutputSize: 0,
			},
		},
		{
			name: "max_output_size too small",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
				MaxOutputSize: 1024,
			},
			wantErr: "max_output_size must be >= max_buffer_size when set",
		},
		{
			name: "negative restart delay",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxConcurrent: 1,
				MaxBufferSize: 1024 * 1024,
				RestartDelay:  -1 * time.Second,
			},
			wantErr: "restart_delay must not be negative",
		},
		{
			name: "max_restart_delay less than restart_delay",
			cfg: &Config{
				Command:         []string{"tail", "-f", "/dev/null"},
				Mode:            ModeStreaming,
				Interval:        60 * time.Second,
				MaxBufferSize:   1024 * 1024,
				RestartDelay:    10 * time.Second,
				MaxRestartDelay: 5 * time.Second,
			},
			wantErr: "max_restart_delay must be >= restart_delay",
		},
		{
			name: "valid max_restart_delay",
			cfg: &Config{
				Command:         []string{"tail", "-f", "/dev/null"},
				Mode:            ModeStreaming,
				Interval:        60 * time.Second,
				MaxBufferSize:   1024 * 1024,
				RestartDelay:    time.Second,
				MaxRestartDelay: 5 * time.Minute,
			},
		},
		{
			name: "max_restart_delay zero uses no cap",
			cfg: &Config{
				Command:         []string{"tail", "-f", "/dev/null"},
				Mode:            ModeStreaming,
				Interval:        60 * time.Second,
				MaxBufferSize:   1024 * 1024,
				RestartDelay:    time.Second,
				MaxRestartDelay: 0,
			},
		},
		{
			name: "non-existent working directory",
			cfg: &Config{
				Command:          []string{"echo"},
				Mode:             ModeScheduled,
				Interval:         60 * time.Second,
				MaxConcurrent:    1,
				MaxBufferSize:    1024 * 1024,
				WorkingDirectory: "/nonexistent/path/that/does/not/exist",
			},
			wantErr: "working_directory",
		},
		{
			name: "multiple errors",
			cfg: &Config{
				Command:       []string{},
				Mode:          "bad",
				MaxBufferSize: 1,
				RestartDelay:  -1,
			},
			wantErr: "command must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigLoadFromYAML(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)

	tests := []struct {
		id       component.ID
		expected *Config
	}{
		{
			id: component.NewID(component.MustNewType("exec")),
			expected: &Config{
				Command:         []string{"echo", "hello"},
				Mode:            ModeScheduled,
				Interval:        10 * time.Second,
				MaxConcurrent:   1,
				IncludeStderr:   true,
				MaxBufferSize:   1024 * 1024,
				MaxOutputSize:   10 * 1024 * 1024,
				RestartDelay:    time.Second,
				MaxRestartDelay: 5 * time.Minute,
			},
		},
		{
			id: component.NewIDWithName(component.MustNewType("exec"), "streaming"),
			expected: &Config{
				Command:            []string{"tail", "-f", "/dev/null"},
				Mode:               ModeStreaming,
				Interval:           60 * time.Second,
				MaxConcurrent:      1,
				RestartDelay:       2 * time.Second,
				MaxRestartDelay:    10 * time.Second,
				IncludeStderr:      false,
				MaxBufferSize:      2097152,
				MaxOutputSize:      10 * 1024 * 1024,
				Environment:        map[string]string{"FOO": "bar"},
				WorkingDirectory:   "/tmp",
				InheritEnvironment: false,
			},
		},
		{
			id: component.NewIDWithName(component.MustNewType("exec"), "full"),
			expected: &Config{
				Command:            []string{"sh", "-c", "echo test"},
				Mode:               ModeScheduled,
				Interval:           30 * time.Second,
				ExecTimeout:        10 * time.Second,
				MaxConcurrent:      1,
				IncludeStderr:      true,
				MaxBufferSize:      524288,
				MaxOutputSize:      5242880,
				Environment:        map[string]string{"ENV1": "value1", "ENV2": "value2"},
				WorkingDirectory:   "/tmp",
				InheritEnvironment: true,
				RestartDelay:       5 * time.Second,
				MaxRestartDelay:    30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.id.String(), func(t *testing.T) {
			factory := NewFactory()
			cfg := factory.CreateDefaultConfig()

			sub, err := cm.Sub(tt.id.String())
			require.NoError(t, err)
			require.NoError(t, sub.Unmarshal(cfg))

			assert.Equal(t, tt.expected, cfg)
		})
	}
}
