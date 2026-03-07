# Exec Receiver

| Status        |           |
| ------------- |-----------|
| Stability     | [alpha]: logs |
| Distributions | |
| Issues        | [![Open issues](https://img.shields.io/github/issues-search/graphaelli/execreceiver?query=is%3Aissue%20is%3Aopen&label=open&color=orange&logo=opentelemetry)](https://github.com/graphaelli/execreceiver/issues) |

[alpha]: https://github.com/open-telemetry/opentelemetry-collector/blob/main/docs/component-stability.md#alpha

An OpenTelemetry Collector receiver that runs external commands and captures their stdout/stderr output as log records.

## Security Considerations

This receiver executes arbitrary commands with the same privileges as the collector process. Keep the following in mind:

- **Restrict access to the collector configuration.** Anyone who can modify the config can execute commands on the host.
- **Environment is clean by default.** Commands start with an empty environment unless `inherit_environment: true` is set. Avoid inheriting the collector's environment when it may contain secrets (API keys, tokens, credentials).
- **Be mindful of command output.** Output flows into the telemetry pipeline and may contain sensitive data (PII, credentials, internal paths). Use processors to filter or redact as needed.
- **Avoid passing secrets as command arguments.** The full command string is recorded in the `exec.command` log attribute on every record.
- **Set `exec_timeout` in scheduled mode** to prevent runaway processes from accumulating.

## Modes

### Scheduled (default)

Runs the command on a configurable interval.
Each execution produces a batch of log records from the command's output. If the command is still running when the next interval fires and the `max_concurrent` limit has been reached, that tick is skipped and a warning is logged.

### Streaming

Runs the command continuously and reads output line-by-line as it is produced.
If the command exits, it is restarted after a configurable delay with exponential backoff.
The backoff starts at `restart_delay`, doubles on each consecutive failure, and caps at `max_restart_delay`.
It resets when the command runs successfully for longer than `restart_delay`.

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `command` | `[]string` | *(required)* | Command and arguments to execute. First element is the program. |
| `mode` | `string` | `scheduled` | Execution mode: `scheduled` or `streaming`. |
| `interval` | `duration` | `60s` | Time between scheduled executions. Only used in scheduled mode. |
| `exec_timeout` | `duration` | `0` (none) | Maximum duration for a scheduled execution. Process is killed if exceeded. Only used in scheduled mode. |
| `max_concurrent` | `int` | `1` | Maximum number of concurrent command executions in scheduled mode. If the limit is reached when a tick fires, that execution is skipped and a warning is logged. Only used in scheduled mode. |
| `include_stderr` | `bool` | `true` | Whether to capture stderr output alongside stdout. |
| `max_buffer_size` | `int` | `1048576` (1MB) | Maximum buffer size in bytes for reading a single line of output. |
| `max_output_size` | `int` | `10485760` (10MB) | Maximum total bytes buffered per scheduled execution. Output is truncated when exceeded. `0` means no limit. Must be >= `max_buffer_size` when set. Only used in scheduled mode. |
| `environment` | `map[string]string` | `{}` | Environment variables to set for the command. |
| `working_directory` | `string` | *(inherit)* | Working directory for the command. |
| `inherit_environment` | `bool` | `false` | If true, inherits the collector's environment as a base. When false (default), starts with a clean environment. `environment` entries are always added. |
| `restart_delay` | `duration` | `1s` | Initial delay before restarting a streaming command that has exited. Doubles on each consecutive failure (exponential backoff), capped at `max_restart_delay`. Resets after the command runs longer than `restart_delay`. Only used in streaming mode. |
| `max_restart_delay` | `duration` | `5m` | Maximum backoff delay for streaming command restarts. Must be >= `restart_delay`. Only used in streaming mode. |

### Example: Scheduled

```yaml
receivers:
  exec:
    command: ["df", "-h"]
    mode: scheduled
    interval: 30s
```

On Windows:

```yaml
receivers:
  exec:
    command: ["powershell", "-Command", "Invoke-WebRequest -Uri http://localhost:8080/health -UseBasicParsing | Select-Object -ExpandProperty Content"]
    mode: scheduled
    interval: 30s
    exec_timeout: 10s
```

### Example: Streaming

```yaml
receivers:
  exec:
    command: ["tail", "-f", "/var/log/syslog"]
    mode: streaming
    restart_delay: 5s
    include_stderr: false
```

On Windows:

```yaml
receivers:
  exec:
    command: ["powershell", "-Command", "Get-Content -Path C:\\logs\\app.log -Wait -Tail 0"]
    mode: streaming
    restart_delay: 5s
    include_stderr: false
```

### Example: Multiple Receivers

```yaml
receivers:
  exec/disk:
    command: ["df", "-h"]
    mode: scheduled
    interval: 60s
  exec/uptime:
    command: ["uptime"]
    mode: scheduled
    interval: 300s
  exec/journal:
    command: ["journalctl", "-f", "-o", "json"]
    mode: streaming

service:
  pipelines:
    logs:
      receivers: [exec/disk, exec/uptime, exec/journal]
      processors: [batch]
      exporters: [debug]
```

See the [examples/](examples/) directory for complete collector configurations.

## Log Record Schema

Each line of output produces a log record with the following structure:

| Field | Value |
|-------|-------|
| **Body** | Raw output line text |
| **Timestamp** | Time the line was read |
| **ObservedTimestamp** | Time the log record was created |
| **SeverityNumber** | `INFO` (stdout) or `WARN` (stderr) |
| **SeverityText** | `INFO` or `WARN` |

### Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `exec.command` | string | The full command string (space-joined) |
| `exec.pid` | int64 | Process ID of the executed command |
| `exec.stream` | string | `stdout` or `stderr` |

### Resource Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `host.name` | string | Hostname of the collector host |

## Self-Observability

The receiver exposes internal metrics for monitoring its own operation:

| Metric | Type | Description |
|--------|------|-------------|
| `otelcol_exec_receiver_errors` | Counter | Execution errors (start failures, non-zero exits) |
| `otelcol_exec_receiver_executions` | Counter | Total command executions started |
| `otelcol_exec_receiver_executions_skipped` | Counter | Scheduled executions skipped due to concurrency limit |
| `otelcol_exec_receiver_execution_duration` | Histogram | Duration of command executions (seconds) |
| `otelcol_exec_receiver_log_records` | Counter | Total log records produced from command output |
| `otelcol_exec_receiver_restarts` | Counter | Streaming mode command restarts |

Standard receiver metrics (`otelcol_receiver_accepted_log_records`, `otelcol_receiver_refused_log_records`)
are also available via the built-in `ObsReport` instrumentation.

## Process Management

- **Graceful shutdown**: On shutdown, the receiver sends `SIGINT` to running processes, then `SIGKILL` after 5 seconds if the process hasn't exited.
- **Scheduled mode**: Each execution runs independently. If `exec_timeout` is set, the process is killed if it exceeds the timeout.
- **Streaming mode**: The command runs continuously. If it exits, the receiver waits before restarting with exponential backoff (starting at `restart_delay`, doubling on each consecutive failure, capped at `max_restart_delay`). The backoff resets when the command runs longer than `restart_delay`. The restart counter metric tracks how many times a streaming command has been restarted.
- **Environment**: By default, commands start with a clean environment. Use `environment` to set variables, or `inherit_environment: true` to inherit the collector's environment as a base.

### Audit Logging

The receiver emits structured INFO-level log messages for each command execution to provide an audit trail. These logs are written to the collector's own log output (not the telemetry pipeline) and include the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | The full command string (space-joined) |
| `pid` | int | Process ID of the executed command |
| `exit_code` | int | Exit code (0 for success, >0 for failure, -1 if unavailable) |
| `duration` | duration | Wall-clock duration of the execution |
| `mode` | string | `scheduled` or `streaming` |
| `receiver_id` | string | The receiver's component ID (e.g. `exec/myname`) |

In **scheduled mode**, a `"Command exited"` message is logged after each execution completes.

In **streaming mode**, a `"Command started"` message is logged when the process starts (with `command`, `pid`, `mode`, and `receiver_id` fields), and a `"Command exited"` message is logged when the process exits (with all fields including `exit_code` and `duration`).

## Building a Custom Collector

Use the [OpenTelemetry Collector Builder (ocb)](https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/cmd/builder) to include this receiver in a custom collector distribution:

```yaml
# builder-config.yaml
dist:
  name: custom-otelcol
  output_path: ./dist

receivers:
  - gomod: github.com/graphaelli/execreceiver v0.1.0

exporters:
  - gomod: go.opentelemetry.io/collector/exporter/debugexporter v0.147.0

processors:
  - gomod: go.opentelemetry.io/collector/processor/batchprocessor v0.147.0
```

Then build:

```sh
ocb --config builder-config.yaml
```

## Benchmarks

Run benchmarks with:

```sh
go test -bench=. -benchtime=3s ./...
```

Example results (Apple M4 Pro):

```
BenchmarkLogRecordCreation/1-lines      994836     1188 ns/op     848 B/op    21 allocs/op
BenchmarkLogRecordCreation/10-lines     519051     2372 ns/op    4768 B/op    97 allocs/op
BenchmarkLogRecordCreation/100-lines     88672    13651 ns/op   43408 B/op   820 allocs/op
BenchmarkLogRecordCreation/1000-lines     8958   136036 ns/op  425973 B/op  8023 allocs/op
BenchmarkScheduledExecution/1-lines       331  3718713 ns/op
BenchmarkScheduledExecution/1000-lines    334  3587836 ns/op
BenchmarkReceiverLifecycle              38466    27741 ns/op    8470 B/op   112 allocs/op
```
