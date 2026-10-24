# Shared local telemetry

`mono telemetry` manages one shared development observability backend per selected
Kubernetes cluster. It works from any directory, without Git or `services.yaml`.
It uses `grafana/otel-lgtm:0.29.0`, with Grafana, Loki, Tempo, Prometheus and the
OpenTelemetry Collector. Applications send logs, metrics and traces over OTLP;
they do not need to expose Prometheus scrape endpoints.

## Setup and connection

Start your local cluster (for example Rancher Desktop with Kubernetes enabled),
install `kubectl`, and ensure a storage provisioner and StorageClass are available.
Mono diagnoses these prerequisites; it does not install them or create a cluster.

```sh
mono telemetry doctor
# Or select an existing context explicitly:
mono telemetry doctor --context rancher-desktop

mono telemetry doctor --check
mono telemetry status
mono telemetry connect
```

Doctor checks prerequisites and installs or repairs namespace `mono-telemetry`,
Deployment/Service `lgtm`, and independent PVC `lgtm`. `up` uses the same setup path.
`doctor --check` and `status` only inspect. Missing, stopped or unhealthy resources
return nonzero. Existing conflicting resources without mono ownership labels are
not adopted. An existing namespace can be used without changing its labels.

Context selection is explicit `--context`, then saved selection, then the current
recognized local context, then a sole reachable recognized local context. Discovery
recognizes `docker-desktop`, `rancher-desktop`, `minikube`, `microk8s`, `colima`,
`kind-*` and `k3d-*`. Other existing contexts work with explicit `--context`.
Ambiguity requires an explicit choice. Unavailable explicit/saved contexts fail
without falling back. Mono never switches kubeconfig's current context.

Keep `connect` running in a terminal while using the backend. It binds six host
ports on `127.0.0.1`, waits for forwarding and backend health, and prints the ready
endpoints. Ctrl-C or SIGTERM stops and reaps its forwarding child. A pod/cluster
interruption can end forwarding; rerun `connect`. No daemon, PID file, ingress or
automatic reconnect is installed. Occupied ports fail instead of silently changing
the endpoints. Grafana keeps the bundled development defaults, including admin/admin
credentials and anonymous Admin access; the Service is reachable inside its cluster.

| Endpoint | Default host URL/address |
| --- | --- |
| Grafana | `http://127.0.0.1:3000` |
| Loki | `http://127.0.0.1:3100` |
| Tempo queries | `http://127.0.0.1:3200` |
| OTLP gRPC | `127.0.0.1:4317` |
| OTLP HTTP | `http://127.0.0.1:4318` |
| Prometheus | `http://127.0.0.1:9090` |

## Configure applications

In another terminal, load settings into the application's shell:

```sh
eval "$(mono telemetry env --service-name orders-api --project storefront)"
# Start your application with its OpenTelemetry SDK/exporter configured.
```

`env` works offline and prints only POSIX-shell exports. It sets
`OTEL_EXPORTER_OTLP_ENDPOINT` to the collector's HTTP base URL,
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`, and `GRAFANA_URL`, `LOKI_URL`,
`TEMPO_URL`, `PROMETHEUS_URL`. Optional `--service-name` sets `OTEL_SERVICE_NAME`;
`--project` sets
`OTEL_RESOURCE_ATTRIBUTES=service.namespace=<project>,deployment.environment.name=local`.
The command shell-quotes values and rejects comma, equal and control characters
in project identifiers. It does not derive identity from the working directory.
If your application already sets resource attributes, combine them intentionally.

All projects use the same backend. Explicit service/namespace identity makes their
data distinguishable. Applications still need an SDK or instrumentation that emits
telemetry; mono does not instrument them, scrape host processes, or collect pod logs.

Override host ports identically in both commands:

```sh
mono telemetry connect --grafana-port 13000 --otlp-http-port 14318
# In the application terminal:
eval "$(mono telemetry env --grafana-port 13000 --otlp-http-port 14318 --service-name worker)"
```

Available flags are `--grafana-port`, `--loki-port`, `--tempo-port`,
`--otlp-grpc-port`, `--otlp-http-port`, `--prometheus-port`. Values must be distinct
integers from 1 through 65535. Overrides belong to that invocation and are not saved.

SDKs using OTLP HTTP send to `/v1/logs`, `/v1/metrics`, and `/v1/traces` on the
collector's HTTP port. Configure per-signal URLs with these paths if your SDK needs
them; a general OTLP endpoint is the base URL. For OTLP gRPC, configure your SDK for
`127.0.0.1:4317` and insecure transport (or the explicitly overridden host port).
`env` defaults to HTTP/protobuf; gRPC requires your SDK's protocol/endpoint settings.

Direct backend APIs are also forwarded: Loki accepts native pushes at
`/loki/api/v1/push`; Prometheus accepts Snappy/protobuf remote write at
`/api/v1/write`. Query Loki using `/loki/api/v1/query_range`, Prometheus using
`/api/v1/query` or `/api/v1/query_range`, and Tempo using `/api/traces/<trace-id>`.
Grafana's bundled data sources already point to the in-cluster backends.

## Storage and lifecycle

```sh
mono telemetry down  # Scale to zero; keep the claim, service and configuration.
mono telemetry up    # Reuse the claim and restore the backend.
```

The PVC mounts the entire `/data` directory, including backend WAL/blocks and
Grafana state. Data and dashboards survive project resets, ordinary pod replacement,
and down/up. The default claim is `10Gi` on the cluster's sole default StorageClass.
Choose initial settings with `doctor/up --storage-class NAME --storage-size 20Gi`.
Positive integer `Mi` and `Gi` sizes are accepted. An existing managed claim's actual
settings are adopted when flags are omitted, even if local config was lost. A Bound
claim does not require a current default StorageClass. Mono refuses to resize or
change the class of an existing claim, and never deletes it after failures.

Persistence does not cover cluster recreation, manual namespace/PVC deletion,
disk failure, or uncommitted data after abrupt crashes. Upstream backend retention
still applies; finite disk capacity is not unlimited history. There is no purge or
backup/restore command.

Setup saves only the version, selected context, class and size in
`<user-config-dir>/mono/telemetry.json` (on Linux, `$XDG_CONFIG_HOME` or
`~/.config`). The directory is private and the file is mode `0600`, replaced
atomically. It contains no secrets or PIDs. Setup saves this selection after
prerequisite/ownership checks and before applying resources, so partial installs
can be retried. Selecting another cluster leaves the previous stack intact.
`--context` on status/check/connect/down is temporary and does not change the saved
selection. Corrupt or unsupported config must be repaired or moved before retrying.

## Troubleshooting and verification

Errors identify the tool, context, API, inspection, ownership, storage, apply,
readiness, stop, or connection stage. Start the selected cluster and check
`kubectl --context NAME get --raw=/readyz` for API errors. Storage errors need a
working provisioner and a named or sole default class. A Pending claim with
WaitForFirstConsumer is expected until its workload is scheduled; mono applies the
workload before waiting. Startup has a five-minute readiness budget, with PVC,
pod/deployment, events and bounded log diagnostics on failure. Retry `doctor` after
fixing the reported issue; storage remains in place. `down` can take up to the pod's
60-second termination grace period while the backends flush data.

For port conflicts, stop the process using the port or pass the same override to
`connect` and `env`. If a previous connect ended during pod replacement, rerun it.
For missing data, verify SDK instrumentation, exporter protocol/path, explicit service
identity, query time ranges, backend retention, and the selected context.

Normal `make check` uses isolated fake kubectl and helper processes. The explicit
`make test-telemetry-integration` target requires kubectl, kind, a running Docker
daemon and image network access. Kind can use Rancher Desktop's Docker engine. It
creates and destroys only a uniquely named disposable cluster with private
kubeconfig/config directories; it never targets your existing Rancher cluster.
The test sends host OTLP logs/metrics/traces and native Loki/remote-write samples,
checks Grafana provisioning/dashboard persistence, replaces a pod, performs down/up,
queries historical data, compares PVC UIDs, checks sharing across two empty project
directories, and verifies port release after interrupting connect. An explicitly
requested integration run fails with instructions if prerequisites are missing.
