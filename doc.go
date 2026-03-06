// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate go tool go.opentelemetry.io/collector/cmd/mdatagen metadata.yaml

// Package execreceiver implements an OpenTelemetry Collector receiver that
// executes external commands and captures their stdout/stderr output as log records.
package execreceiver // import "github.com/graphaelli/execreceiver"
