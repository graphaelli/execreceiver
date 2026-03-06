// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package execreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestNewFactory(t *testing.T) {
	f := NewFactory()
	assert.Equal(t, "exec", f.Type().String())
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	assert.Equal(t, ModeScheduled, cfg.Mode)
	assert.Equal(t, 60*time.Second, cfg.Interval)
	assert.True(t, cfg.IncludeStderr)
	assert.Equal(t, 1024*1024, cfg.MaxBufferSize)
	assert.Equal(t, time.Second, cfg.RestartDelay)
}

func TestCreateLogsReceiver(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Command = []string{"echo", "hello"}

	r, err := createLogsReceiver(
		context.Background(),
		receivertest.NewNopSettings(typ),
		cfg,
		consumertest.NewNop(),
	)
	require.NoError(t, err)
	assert.NotNil(t, r)
}
