// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver // import "github.com/graphaelli/execreceiver"

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.uber.org/zap"

	"github.com/graphaelli/execreceiver/internal/metadata"
)

type outputLine struct {
	text      string
	stream    string // "stdout" or "stderr"
	timestamp time.Time
}

type execReceiver struct {
	cfg       *Config
	settings  receiver.Settings
	logger    *zap.Logger
	consumer  consumer.Logs
	obsrecv   *receiverhelper.ObsReport
	telemetry *metadata.TelemetryBuilder

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newExecReceiver(params receiver.Settings, cfg *Config, consumer consumer.Logs) (*execReceiver, error) {
	obsrecv, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             params.ID,
		Transport:              "exec",
		ReceiverCreateSettings: params,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ObsReport: %w", err)
	}

	return &execReceiver{
		cfg:      cfg,
		settings: params,
		logger:   params.Logger,
		consumer: consumer,
		obsrecv:  obsrecv,
	}, nil
}

func (r *execReceiver) startTelemetry() error {
	var err error
	r.telemetry, err = metadata.NewTelemetryBuilder(r.settings.TelemetrySettings)
	if err != nil {
		return fmt.Errorf("creating telemetry builder: %w", err)
	}
	return nil
}

// Start implements receiver.Logs.
func (r *execReceiver) Start(_ context.Context, _ component.Host) error {
	if err := r.startTelemetry(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	r.wg.Add(1)
	switch r.cfg.Mode {
	case ModeScheduled:
		go r.runScheduled(ctx)
	case ModeStreaming:
		go r.runStreaming(ctx)
	}

	return nil
}

// Shutdown implements receiver.Logs.
func (r *execReceiver) Shutdown(_ context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
	if r.telemetry != nil {
		r.telemetry.Shutdown()
	}
	return nil
}

// runScheduled runs the command on a configurable interval.
func (r *execReceiver) runScheduled(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	// Execute immediately on start.
	r.executeOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.executeOnce(ctx)
		}
	}
}

// executeOnce runs the command once and pushes all output as a single log batch.
func (r *execReceiver) executeOnce(ctx context.Context) {
	execCtx := ctx
	if r.cfg.ExecTimeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, r.cfg.ExecTimeout)
		defer cancel()
	}

	start := time.Now()
	r.telemetry.ExecReceiverExecutions.Add(ctx, 1)

	cmd := r.buildCommand(execCtx)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		r.logger.Error("Failed to create stdout pipe", zap.Error(err))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
		return
	}

	var stderrPipe io.ReadCloser
	if r.cfg.IncludeStderr {
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			r.logger.Error("Failed to create stderr pipe", zap.Error(err))
			r.telemetry.ExecReceiverErrors.Add(ctx, 1)
			return
		}
	}

	if err := cmd.Start(); err != nil {
		r.logger.Error("Failed to start command", zap.Error(err))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
		return
	}

	pid := cmd.Process.Pid

	var (
		mu    sync.Mutex
		lines []outputLine
	)

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		l := r.readLines(stdoutPipe, "stdout")
		mu.Lock()
		lines = append(lines, l...)
		mu.Unlock()
	}()

	if stderrPipe != nil {
		readWg.Add(1)
		go func() {
			defer readWg.Done()
			l := r.readLines(stderrPipe, "stderr")
			mu.Lock()
			lines = append(lines, l...)
			mu.Unlock()
		}()
	}

	readWg.Wait()
	cmdErr := cmd.Wait()

	duration := time.Since(start)
	r.telemetry.ExecReceiverExecutionDuration.Record(ctx, duration.Seconds())

	r.logger.Info("Command exited",
		zap.String("command", strings.Join(r.cfg.Command, " ")),
		zap.Int("pid", pid),
		zap.Int("exit_code", exitCode(cmdErr)),
		zap.Duration("duration", duration),
		zap.String("mode", string(r.cfg.Mode)),
		zap.String("receiver_id", r.settings.ID.String()),
	)

	if cmdErr != nil {
		r.logger.Warn("Command exited with error", zap.Error(cmdErr), zap.Int("pid", pid))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
	}

	if len(lines) > 0 {
		ld := r.buildLogData(lines, pid)
		r.telemetry.ExecReceiverLogRecords.Add(ctx, int64(len(lines)))
		obsCtx := r.obsrecv.StartLogsOp(ctx)
		consErr := r.consumer.ConsumeLogs(ctx, ld)
		r.obsrecv.EndLogsOp(obsCtx, metadata.Type.String(), len(lines), consErr)
	}
}

// runStreaming runs the command continuously, restarting on exit.
func (r *execReceiver) runStreaming(ctx context.Context) {
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		r.telemetry.ExecReceiverExecutions.Add(ctx, 1)
		r.streamCommand(ctx)

		// Check if we should stop before restarting.
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.cfg.RestartDelay):
			r.logger.Info("Restarting streaming command")
			r.telemetry.ExecReceiverRestarts.Add(ctx, 1)
		}
	}
}

// streamCommand runs the command and streams output line-by-line to the consumer.
func (r *execReceiver) streamCommand(ctx context.Context) {
	start := time.Now()
	cmd := r.buildCommand(ctx)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		r.logger.Error("Failed to create stdout pipe", zap.Error(err))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
		return
	}

	var stderrPipe io.ReadCloser
	if r.cfg.IncludeStderr {
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			r.logger.Error("Failed to create stderr pipe", zap.Error(err))
			r.telemetry.ExecReceiverErrors.Add(ctx, 1)
			return
		}
	}

	if err := cmd.Start(); err != nil {
		r.logger.Error("Failed to start streaming command", zap.Error(err))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
		return
	}

	pid := cmd.Process.Pid
	r.logger.Info("Command started",
		zap.String("command", strings.Join(r.cfg.Command, " ")),
		zap.Int("pid", pid),
		zap.String("mode", string(r.cfg.Mode)),
		zap.String("receiver_id", r.settings.ID.String()),
	)

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		r.streamLines(ctx, stdoutPipe, "stdout", pid)
	}()

	if stderrPipe != nil {
		readWg.Add(1)
		go func() {
			defer readWg.Done()
			r.streamLines(ctx, stderrPipe, "stderr", pid)
		}()
	}

	readWg.Wait()
	cmdErr := cmd.Wait()

	duration := time.Since(start)
	r.logger.Info("Command exited",
		zap.String("command", strings.Join(r.cfg.Command, " ")),
		zap.Int("pid", pid),
		zap.Int("exit_code", exitCode(cmdErr)),
		zap.Duration("duration", duration),
		zap.String("mode", string(r.cfg.Mode)),
		zap.String("receiver_id", r.settings.ID.String()),
	)

	if cmdErr != nil && ctx.Err() == nil {
		r.logger.Warn("Streaming command exited", zap.Error(cmdErr), zap.Int("pid", pid))
		r.telemetry.ExecReceiverErrors.Add(ctx, 1)
	}
}

// streamLines reads lines from a reader and pushes each one to the consumer.
func (r *execReceiver) streamLines(ctx context.Context, reader io.Reader, stream string, pid int) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, r.cfg.MaxBufferSize), r.cfg.MaxBufferSize)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := outputLine{
			text:      scanner.Text(),
			stream:    stream,
			timestamp: time.Now(),
		}

		ld := r.buildLogData([]outputLine{line}, pid)
		r.telemetry.ExecReceiverLogRecords.Add(ctx, 1)
		obsCtx := r.obsrecv.StartLogsOp(ctx)
		consErr := r.consumer.ConsumeLogs(ctx, ld)
		r.obsrecv.EndLogsOp(obsCtx, metadata.Type.String(), 1, consErr)
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		r.logger.Warn("Error reading command output",
			zap.String("stream", stream), zap.Error(err))
	}
}

// readLines reads all lines from a reader and returns them.
func (r *execReceiver) readLines(reader io.Reader, stream string) []outputLine {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, r.cfg.MaxBufferSize), r.cfg.MaxBufferSize)

	var lines []outputLine
	for scanner.Scan() {
		lines = append(lines, outputLine{
			text:      scanner.Text(),
			stream:    stream,
			timestamp: time.Now(),
		})
	}

	if err := scanner.Err(); err != nil {
		r.logger.Warn("Error reading command output",
			zap.String("stream", stream), zap.Error(err))
	}

	return lines
}

// buildCommand creates an exec.Cmd from the receiver configuration.
func (r *execReceiver) buildCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.cfg.Command[0], r.cfg.Command[1:]...)

	// Graceful shutdown: send SIGINT first, then SIGKILL after 5s.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	if r.cfg.WorkingDirectory != "" {
		cmd.Dir = r.cfg.WorkingDirectory
	}

	if r.cfg.InheritEnvironment {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = []string{}
	}

	for k, v := range r.cfg.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return cmd
}

// exitCode extracts the exit code from an error returned by cmd.Wait.
// It returns 0 if err is nil, or the exit code from exec.ExitError.
// For other error types it returns -1.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// buildLogData constructs plog.Logs from output lines.
func (r *execReceiver) buildLogData(lines []outputLine, pid int) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()

	hostname, _ := os.Hostname()
	rl.Resource().Attributes().PutStr("host.name", hostname)

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(metadata.ScopeName)
	sl.Scope().SetVersion(r.settings.BuildInfo.Version)

	now := time.Now()
	cmdStr := strings.Join(r.cfg.Command, " ")

	for _, line := range lines {
		lr := sl.LogRecords().AppendEmpty()

		lr.SetTimestamp(pcommon.NewTimestampFromTime(line.timestamp))
		lr.SetObservedTimestamp(pcommon.NewTimestampFromTime(now))

		lr.Body().SetStr(line.text)

		if line.stream == "stderr" {
			lr.SetSeverityNumber(plog.SeverityNumberWarn)
			lr.SetSeverityText("WARN")
		} else {
			lr.SetSeverityNumber(plog.SeverityNumberInfo)
			lr.SetSeverityText("INFO")
		}

		lr.Attributes().PutStr("exec.command", cmdStr)
		lr.Attributes().PutInt("exec.pid", int64(pid))
		lr.Attributes().PutStr("exec.stream", line.stream)
	}

	return ld
}
