// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
		Command:       helperCmd(t, "echo", "hello"),
		Mode:          ModeScheduled,
		Interval:      time.Hour, // won't tick during test
		MaxConcurrent: 1,
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
	assert.True(t, strings.HasSuffix(cmd.Str(), "echo hello"),
		"exec.command should end with 'echo hello', got: %s", cmd.Str())

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
		Command:       helperCmd(t, "multiline", "a", "b", "c"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
		Command:       helperCmd(t, "echo-stderr", "error_output"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
		Command:       helperCmd(t, "echo-both", "stdout_only", "stderr_only"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
		Command:       helperCmd(t, "exit", "1"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
	cfg := &Config{
		Command:       helperCmd(t, "sleep", "60000"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		ExecTimeout:   500 * time.Millisecond,
		MaxConcurrent: 1,
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
		Command:         helperCmd(t, "seq", "3"),
		Mode:            ModeStreaming,
		IncludeStderr:   true,
		MaxBufferSize:   1024 * 1024,
		RestartDelay:    time.Hour, // don't restart during test
		MaxRestartDelay: time.Hour,
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
		Command:         helperCmd(t, "echo", "restarted"),
		Mode:            ModeStreaming,
		IncludeStderr:   true,
		MaxBufferSize:   1024 * 1024,
		RestartDelay:    100 * time.Millisecond,
		MaxRestartDelay: 100 * time.Millisecond, // cap at restart_delay to keep restarts fast
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
		Command:       helperCmd(t, "echo", "hello"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, _ := newTestReceiver(t, cfg)
	require.NoError(t, r.Shutdown(context.Background()))
}

func TestEnvironment(t *testing.T) {
	cfg := &Config{
		Command:       helperCmd(t, "env", "TEST_VAR_EXEC"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
	// Default is inherit_environment: false, so PATH should not be set.
	cfg := &Config{
		Command:       helperCmd(t, "env", "PATH"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
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
	assert.Equal(t, "", lr.Body().Str())
}

func TestInheritEnvironment(t *testing.T) {
	// With inherit_environment: true, PATH should be inherited from the collector.
	cfg := &Config{
		Command:            helperCmd(t, "env", "PATH"),
		Mode:               ModeScheduled,
		Interval:           time.Hour,
		MaxConcurrent:      1,
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
	assert.NotEmpty(t, lr.Body().Str(), "PATH should be inherited from the collector environment")
}

func TestWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Command:          helperCmd(t, "pwd"),
		Mode:             ModeScheduled,
		Interval:         time.Hour,
		MaxConcurrent:    1,
		IncludeStderr:    true,
		MaxBufferSize:    1024 * 1024,
		RestartDelay:     time.Second,
		WorkingDirectory: dir,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	lr := sink.AllLogs()[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	body := lr.Body().Str()

	// Resolve symlinks to handle e.g. macOS /tmp → /private/tmp
	evalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		evalDir = dir
	}
	assert.True(t, body == dir || body == evalDir, "unexpected working directory: %s", body)
}

func TestScheduledMaxOutputSize(t *testing.T) {
	// Generate output that exceeds the max_output_size limit.
	// Each line from "seq 20" is "lineN\n" (6–8 bytes). With max_output_size=50,
	// reading stops once totalBytes exceeds 50, leaving well under 20 lines.
	cfg := &Config{
		Command:       helperCmd(t, "seq", "20"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024,
		MaxOutputSize: 50,
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	// Should have fewer than 20 lines due to truncation
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

	assert.Greater(t, len(bodies), 0, "should have at least one line")
	assert.Less(t, len(bodies), 20, "output should be truncated before all 20 lines")
}

func TestScheduledMaxOutputSizeUnlimited(t *testing.T) {
	// When max_output_size is 0, all output should be captured.
	cfg := &Config{
		Command:       helperCmd(t, "seq", "5"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024,
		MaxOutputSize: 0, // unlimited
		RestartDelay:  time.Second,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 5
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
	assert.Equal(t, 5, len(bodies), "all 5 lines should be captured when unlimited")
}

func TestScheduledSkipOnConcurrencyLimit(t *testing.T) {
	// Use a slow command (sleep 10s) with a short interval and max_concurrent=1.
	// The first execution (triggered immediately on Start) will hold the
	// semaphore for a long time. Subsequent ticks should be skipped.
	cfg := &Config{
		Command:       helperCmd(t, "sleep", "10000"),
		Mode:          ModeScheduled,
		Interval:      100 * time.Millisecond,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, _ := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Wait for a few ticks to fire while the slow command is still running.
	// Ticks should be skipped because the semaphore is already held.
	time.Sleep(500 * time.Millisecond)

	// The semaphore should be full (1 slot occupied by sleep).
	// Verify that we cannot acquire the semaphore (it's full).
	select {
	case r.sem <- struct{}{}:
		// We acquired it, which means the slot was free -- unexpected
		<-r.sem
		t.Fatal("expected semaphore to be full, but acquired a slot")
	default:
		// Expected: semaphore is full
	}
}

func TestScheduledInterval(t *testing.T) {
	cfg := &Config{
		Command:       helperCmd(t, "echo", "tick"),
		Mode:          ModeScheduled,
		Interval:      200 * time.Millisecond,
		MaxConcurrent: 1,
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

func TestStreamingBackoffIncreasesOnRapidFailures(t *testing.T) {
	// Use a command that exits immediately to trigger rapid failures.
	// With restart_delay=50ms and max_restart_delay=200ms, the delays should be:
	//   50ms, 100ms, 200ms (capped), 200ms, ...
	// After 3 restarts (50+100+200=350ms minimum), we check timing.
	cfg := &Config{
		Command:         helperCmd(t, "echo-exit", "backoff", "1"),
		Mode:            ModeStreaming,
		IncludeStderr:   true,
		MaxBufferSize:   1024 * 1024,
		RestartDelay:    50 * time.Millisecond,
		MaxRestartDelay: 200 * time.Millisecond,
	}
	r, sink := newTestReceiver(t, cfg)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	start := time.Now()

	// Wait for at least 4 executions (initial + 3 restarts).
	// With backoff: 0 + 50ms + 100ms + 200ms = 350ms minimum before 4th execution starts.
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 4
	}, 5*time.Second, 10*time.Millisecond)

	elapsed := time.Since(start)

	// The total delay for 3 restarts should be at least 350ms (50+100+200).
	// Without backoff it would be 150ms (3*50ms).
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(300),
		"backoff should cause restarts to take longer than without backoff")
}

func TestStreamingBackoffResetsAfterSuccessfulRun(t *testing.T) {
	// This test uses a command that:
	// 1. First run: exits immediately (triggers backoff increase to 100ms)
	// 2. Second run: sleeps for 200ms (longer than restart_delay=50ms), then exits
	//    -> backoff should reset to 50ms
	// 3. Third run: exits immediately again
	// 4. Fourth run: exits immediately again
	//
	// If backoff reset works, runs 3+4 happen with short delays (50ms, 100ms).
	// If backoff did NOT reset, run 3 would wait 200ms (doubled from 100ms).
	//
	// State is tracked via a file managed by the testhelper "statefile" subcommand.
	stateFile := filepath.Join(t.TempDir(), "state")
	cfg := &Config{
		Command:         helperCmd(t, "statefile", stateFile),
		Mode:            ModeStreaming,
		IncludeStderr:   true,
		MaxBufferSize:   1024 * 1024,
		RestartDelay:    50 * time.Millisecond,
		MaxRestartDelay: 5 * time.Second,
	}

	r, sink := newTestReceiver(t, cfg)

	start := time.Now()
	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Wait for all 4 runs to complete.
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() >= 4
	}, 10*time.Second, 10*time.Millisecond)

	elapsed := time.Since(start)

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

	assert.Contains(t, bodies, "run1-fail")
	assert.Contains(t, bodies, "run2-success")
	assert.Contains(t, bodies, "run3-after-reset")
	assert.Contains(t, bodies, "run4-done")

	// With backoff reset, total should be well under 2s.
	// Without reset, the accumulated backoff would be much larger.
	assert.Less(t, elapsed.Milliseconds(), int64(2000),
		"backoff should have reset after the successful long-running command")
}

func TestAuditLogScheduled(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)

	cfg := &Config{
		Command:       helperCmd(t, "echo", "hello"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	sink := new(consumertest.LogsSink)
	settings := receivertest.NewNopSettings(typ)
	settings.Logger = zap.New(core)
	r, err := newExecReceiver(settings, cfg, sink)
	require.NoError(t, err)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Wait for the command to execute and produce output.
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 5*time.Second, 50*time.Millisecond)

	// Find the "Command exited" audit log.
	var exitLogs []observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "Command exited" {
			exitLogs = append(exitLogs, entry)
		}
	}
	require.NotEmpty(t, exitLogs, "expected at least one 'Command exited' audit log")

	entry := exitLogs[0]
	assert.Equal(t, zapcore.InfoLevel, entry.Level)

	fields := fieldMap(entry.ContextMap())
	assert.True(t, strings.HasSuffix(fields["command"].(string), "echo hello"),
		"command should end with 'echo hello', got: %s", fields["command"])
	assert.Equal(t, int64(0), fields["exit_code"])
	assert.Equal(t, "scheduled", fields["mode"])
	assert.Contains(t, fields, "pid")
	assert.Contains(t, fields, "duration")
	assert.Contains(t, fields, "receiver_id")
}

func TestAuditLogScheduledNonZeroExit(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)

	cfg := &Config{
		Command:       helperCmd(t, "exit", "2"),
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		MaxConcurrent: 1,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	sink := new(consumertest.LogsSink)
	settings := receivertest.NewNopSettings(typ)
	settings.Logger = zap.New(core)
	r, err := newExecReceiver(settings, cfg, sink)
	require.NoError(t, err)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Wait for the audit log to appear.
	require.Eventually(t, func() bool {
		for _, entry := range observed.All() {
			if entry.Message == "Command exited" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)

	var exitLog observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "Command exited" {
			exitLog = entry
			break
		}
	}

	fields := fieldMap(exitLog.ContextMap())
	assert.Equal(t, int64(2), fields["exit_code"])
	assert.True(t, strings.HasSuffix(fields["command"].(string), "exit 2"),
		"command should end with 'exit 2', got: %s", fields["command"])
}

func TestAuditLogStreaming(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)

	cfg := &Config{
		Command:       helperCmd(t, "echo", "streaming_output"),
		Mode:          ModeStreaming,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Hour, // don't restart during test
	}
	sink := new(consumertest.LogsSink)
	settings := receivertest.NewNopSettings(typ)
	settings.Logger = zap.New(core)
	r, err := newExecReceiver(settings, cfg, sink)
	require.NoError(t, err)

	require.NoError(t, r.Start(context.Background(), nil))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })

	// Wait for both audit logs to appear.
	require.Eventually(t, func() bool {
		var hasStarted, hasExited bool
		for _, entry := range observed.All() {
			if entry.Message == "Command started" {
				hasStarted = true
			}
			if entry.Message == "Command exited" {
				hasExited = true
			}
		}
		return hasStarted && hasExited
	}, 5*time.Second, 50*time.Millisecond)

	// Verify "Command started" log fields.
	var startLog observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "Command started" {
			startLog = entry
			break
		}
	}
	assert.Equal(t, zapcore.InfoLevel, startLog.Level)
	startFields := fieldMap(startLog.ContextMap())
	assert.Contains(t, startFields, "pid")
	assert.Equal(t, "streaming", startFields["mode"])
	assert.Contains(t, startFields, "command")
	assert.Contains(t, startFields, "receiver_id")

	// Verify "Command exited" log fields.
	var exitLog observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "Command exited" {
			exitLog = entry
			break
		}
	}
	assert.Equal(t, zapcore.InfoLevel, exitLog.Level)
	exitFields := fieldMap(exitLog.ContextMap())
	assert.Equal(t, int64(0), exitFields["exit_code"])
	assert.Equal(t, "streaming", exitFields["mode"])
	assert.Contains(t, exitFields, "pid")
	assert.Contains(t, exitFields, "duration")
	assert.Contains(t, exitFields, "receiver_id")
}

// fieldMap converts observer context map to a simple map for assertions.
func fieldMap(m map[string]interface{}) map[string]interface{} {
	return m
}
