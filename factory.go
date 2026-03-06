// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver // import "github.com/graphaelli/execreceiver"

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/graphaelli/execreceiver/internal/metadata"
)

// NewFactory creates a factory for the exec receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithLogs(createLogsReceiver, metadata.LogsStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Mode:          ModeScheduled,
		Interval:      60 * time.Second,
		IncludeStderr: true,
		MaxBufferSize: 1024 * 1024, // 1MB
		RestartDelay:  time.Second,
	}
}

func createLogsReceiver(
	_ context.Context,
	params receiver.Settings,
	cfg component.Config,
	consumer consumer.Logs,
) (receiver.Logs, error) {
	return newExecReceiver(params, cfg.(*Config), consumer)
}
