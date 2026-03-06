// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func BenchmarkLogRecordCreation(b *testing.B) {
	for _, numLines := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("%d-lines", numLines), func(b *testing.B) {
			r, _ := newBenchReceiver(b)
			lines := make([]outputLine, numLines)
			now := time.Now()
			for i := range lines {
				lines[i] = outputLine{
					text:      fmt.Sprintf("benchmark log line %d with some realistic content", i),
					stream:    "stdout",
					timestamp: now,
				}
			}

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = r.buildLogData(lines, 12345)
			}
		})
	}
}

func BenchmarkScheduledExecution(b *testing.B) {
	for _, numLines := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("%d-lines", numLines), func(b *testing.B) {
			r, _ := newBenchReceiver(b)
			r.cfg.Command = []string{"seq", fmt.Sprintf("%d", numLines)}

			require.NoError(b, r.startTelemetry())
			b.Cleanup(func() { r.telemetry.Shutdown() })

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				r.executeOnce(context.Background())
			}
		})
	}
}

func BenchmarkReceiverLifecycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		r, _ := newBenchReceiver(b)
		r.cfg.Command = []string{"echo", "hello"}
		r.cfg.Interval = time.Hour
		require.NoError(b, r.Start(context.Background(), nil))
		require.NoError(b, r.Shutdown(context.Background()))
	}
}

func newBenchReceiver(b *testing.B) (*execReceiver, *consumertest.LogsSink) {
	b.Helper()
	sink := new(consumertest.LogsSink)
	cfg := &Config{
		Command:       []string{"echo", "hello"},
		Mode:          ModeScheduled,
		Interval:      time.Hour,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024,
		RestartDelay:  time.Second,
	}
	r, err := newExecReceiver(receivertest.NewNopSettings(typ), cfg, sink)
	require.NoError(b, err)
	return r, sink
}
