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
			name: "buffer too small",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxBufferSize: 512,
			},
			wantErr: "max_buffer_size must be at least 1024 bytes",
		},
		{
			name: "negative restart delay",
			cfg: &Config{
				Command:       []string{"echo"},
				Mode:          ModeScheduled,
				Interval:      60 * time.Second,
				MaxBufferSize: 1024 * 1024,
				RestartDelay:  -1 * time.Second,
			},
			wantErr: "restart_delay must not be negative",
		},
		{
			name: "non-existent working directory",
			cfg: &Config{
				Command:          []string{"echo"},
				Mode:             ModeScheduled,
				Interval:         60 * time.Second,
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
				Command:       []string{"echo", "hello"},
				Mode:          ModeScheduled,
				Interval:      10 * time.Second,
				IncludeStderr: true,
				MaxBufferSize: 1024 * 1024,
				RestartDelay:  time.Second,
			},
		},
		{
			id: component.NewIDWithName(component.MustNewType("exec"), "streaming"),
			expected: &Config{
				Command:          []string{"tail", "-f", "/dev/null"},
				Mode:             ModeStreaming,
				Interval:         60 * time.Second,
				RestartDelay:     2 * time.Second,
				IncludeStderr:    false,
				MaxBufferSize:    2097152,
				Environment:      map[string]string{"FOO": "bar"},
				WorkingDirectory: "/tmp",
				InheritEnvironment: false,
			},
		},
		{
			id: component.NewIDWithName(component.MustNewType("exec"), "full"),
			expected: &Config{
				Command:          []string{"sh", "-c", "echo test"},
				Mode:             ModeScheduled,
				Interval:         30 * time.Second,
				ExecTimeout:      10 * time.Second,
				IncludeStderr:    true,
				MaxBufferSize:    524288,
				Environment:      map[string]string{"ENV1": "value1", "ENV2": "value2"},
				WorkingDirectory: "/tmp",
				InheritEnvironment: true,
				RestartDelay:     5 * time.Second,
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
