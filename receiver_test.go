// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func newTestReceiver(t *testing.T, cfg *Config) (*execReceiver, *consumertest.LogsSink) {
	t.Helper()
	sink := new(consumertest.LogsSink)
	r, err := newExecReceiver(receivertest.NewNopSettings(typ), cfg, sink)
	require.NoError(t, err)
	return r, sink
}

func TestScheduledBasic(t *testing.T) {
	cfg := &Config{
		Command:       []string{"echo", "hello"},
		Mode:          ModeScheduled,
		Interval:      time.Hour, // won't tick during test
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	logs := sink.AllLogs()
	require.NotEmpty(t, logs)

	lr := logs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "hello", lr.Body().Str())
	assert.Equal(t, plog.SeverityNumberInfo, lr.SeverityNumber())
	assert.Equal(t, "INFO", lr.SeverityText())

	cmd, ok := lr.Attributes().Get("exec.command")
	require.True(t, ok)
	assert.Equal(t, "echo hello", cmd.Str())

	stream, ok := lr.Attributes().Get("exec.stream")
	require.True(t, ok)
	assert.Equal(t, "stdout", stream.Str())

	pid, ok := lr.Attributes().Get("exec.pid")
	require.True(t, ok)
	assert.Greater(t, pid.Int(), int64(0))

	// Resource attributes
	host, ok := logs[0].ResourceLogs().At(0).Resource().Attributes().Get("host.name")
	require.True(t, ok)
	assert.NotEmpty(t, host.Str())
}

func TestScheduledMultiLine(t *testing.T) {
	cfg := &Config{
		Command:       []string{"printf", "a\nb\nc"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 3
	}, 5*time.Second, 50*time.Millisecond)

	var bodies []string
	for _, ld := range sink.AllLogs() {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				for k := 0; k < sl.LogRecords().Len(); k++ {
					bodies = append(bodies, sl.LogRecords().At(k).Body().Str())
				}
			}
		}
	}
	assert.Equal(t, []string{"a", "b", "c"}, bodies)
}

func TestScheduledStderr(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "echo error_output >&2"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "error_output", lr.Body().Str())
	assert.Equal(t, plog.SeverityNumberWarn, lr.SeverityNumber())
	assert.Equal(t, "WARN", lr.SeverityText())

	stream, _ := lr.Attributes().Get("exec.stream")
	assert.Equal(t, "stderr", stream.Str())
}

func TestScheduledStderrDisabled(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "echo stdout_only && echo stderr_only >&2"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: false,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	// Only stdout should be captured
	for _, ld := range sink.AllLogs() {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				for k := 0; k < sl.LogRecords().Len(); k++ {
					stream, _ := sl.LogRecords().At(k).Attributes().Get("exec.stream")
					assert.Equal(t, "stdout", stream.Str())
				}
			}
		}
	}
}

func TestScheduledCommandError(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "exit 1"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Command fails with exit 1, no output expected
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 0, sink.LogRecordCount())
}

func TestScheduledTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal handling differs on windows")
	}
	cfg := &Config{
		Command:       []string{"sleep", "60"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		ExecTimeout:   500 * time.Millisecond,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, _ := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))

	// Ensure it shuts down cleanly even after timeout
	time.Sleep(time.Second)
	require.NoError(t, r.Shutdown(context.Background()))
}

func TestStreamingBasic(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "for i in 1 2 3; do echo line$i; done"},
		Mode:          ModeStreaming,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Hour, // don't restart during test
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 3
	}, 5*time.Second, 50*time.Millisecond)

	var bodies []string
	for _, ld := range sink.AllLogs() {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				for k := 0; k < sl.LogRecords().Len(); k++ {
					bodies = append(bodies, sl.LogRecords().At(k).Body().Str())
				}
			}
		}
	}
	assert.Contains(t, bodies, "line1")
	assert.Contains(t, bodies, "line2")
	assert.Contains(t, bodies, "line3")
}

func TestStreamingRestart(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "echo restarted; exit 0"},
		Mode:          ModeStreaming,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  100 * time.Millisecond,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Should see multiple "restarted" lines as command restarts
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 3
	}, 5*time.Second, 50*time.Millisecond)
}

func TestShutdownBeforeStart(t *testing.T) {
	cfg := &Config{
		Command:       []string{"echo", "hello"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, _ := newTestReceiver(t, cfg)
	require.NoError(t, r.Shutdown(context.Background()))
}

func TestEnvironment(t *testing.T) {
	cfg := &Config{
		Command:       []string{"sh", "-c", "echo $TEST_VAR_EXEC"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
		Environment:   map[string]string{"TEST_VAR_EXEC": "test_value_123"},
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "test_value_123", lr.Body().Str())
}

func TestCleanEnvironmentByDefault(t *testing.T) {
	// Default is inherit_environment: false, so HOME should not be set.
	cfg := &Config{
		Command:       []string{"sh", "-c", "echo ${HOME:-empty}"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "empty", lr.Body().Str())
}

func TestInheritEnvironment(t *testing.T) {
	// With inherit_environment: true, HOME should be inherited from the collector.
	cfg := &Config{
		Command:            []string{"sh", "-c", "echo ${HOME:-empty}"},
		Mode:               ModeScheduled,
		Interval:           time.Hour,
		IncludeStderr:      true,
		MaxBufferSize:      1024 * 1024,
		RestartDelay:       time.Second,
		InheritEnvironment: true,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.NotEqual(t, "empty", lr.Body().Str(), "HOME should be inherited from the collector environment")
}

func TestWorkingDirectory(t *testing.T) {
	cfg := &Config{
		Command:          []string{"pwd"},
		Mode:             ModeScheduled,
		Interval:         time.Hour,
		IncludeStderr:    true,
		MaxBufferSize:    1024 * 1024,
		RestartDelay:     time.Second,
		WorkingDirectory: "/tmp",
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	// /tmp may resolve to /private/tmp on macOS
	body := lr.Body().Str()
	assert.True(t, body == "/tmp" || body == "/private/tmp", "unexpected working directory: %s", body)
}

func TestScheduledInterval(t *testing.T) {
	cfg := &Config{
		Command:       []string{"echo", "tick"},
		Mode:          ModeScheduled,
		Interval:      200 * time.Millisecond,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Should see multiple executions over time
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 3
	}, 5*time.Second, 50*time.Millisecond)
}
