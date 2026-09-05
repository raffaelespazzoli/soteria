# Story 16.4: ScyllaDB External (BYO) Wiring

Status: ready-for-dev

## Story

As a platform engineer,
I want the Helm chart to support connecting to an externally managed ScyllaDB cluster,
so that I can bring my own ScyllaDB deployment and still use Soteria without the managed ScyllaDB resources.

## Acceptance Criteria

**AC1: No ScyllaDB resources rendered in external mode**
Given `scylladb.mode=external`
When I run `helm template`
Then no ScyllaCluster, ScyllaDB ConfigMap, ScyllaDB Certificate, ServiceExport, or Cilium-annotated service is rendered

**AC2: Controller args use external contact points**
Given `scylladb.mode=external` and `external.contactPoints` is set
When I render the controller Deployment
Then the container args include `--scylladb-contact-points` set to the configured contact points, `--scylladb-keyspace`, `--scylladb-local-dc`, and `--scylladb-dc-replication`

**AC3: TLS volumes mounted when external TLS is enabled**
Given `scylladb.mode=external` and `external.tls.enabled=true`
When I render the controller Deployment
Then the Secret referenced by `external.tls.secretName` is mounted as a volume and the container args include `--scylladb-tls-cert`, `--scylladb-tls-key`, `--scylladb-tls-ca`, and `--scylladb-tls-server-name`

**AC4: No TLS args when external TLS is disabled**
Given `scylladb.mode=external` and `external.tls.enabled=false`
When I render the controller Deployment
Then no `--scylladb-tls-*` args are present and no TLS Secret volume is mounted

**AC5: Switching modes produces correct diff**
Given a rendered chart with `scylladb.mode=managed`
When I re-render with `scylladb.mode=external`
Then the ScyllaDB resources disappear and the controller Deployment args switch to external connection parameters

## Tasks / Subtasks

- [ ] Task 1: Add external mode args to controller Deployment (AC: 2, 5)
  - [ ] 1.1: In `templates/controller/deployment.yaml`, add `{{- else if eq .Values.scylladb.mode "external" }}` block
  - [ ] 1.2: Wire `--scylladb-contact-points={{ .Values.scylladb.external.contactPoints }}`
  - [ ] 1.3: Wire `--scylladb-keyspace={{ .Values.scylladb.keyspace }}`
  - [ ] 1.4: Wire `--scylladb-local-dc={{ .Values.scylladb.localDC }}` (if non-empty)
  - [ ] 1.5: Wire `--scylladb-dc-replication={{ .Values.scylladb.dcReplication }}` (if non-empty)
- [ ] Task 2: Add conditional TLS args (AC: 3, 4)
  - [ ] 2.1: Add `{{- if .Values.scylladb.external.tls.enabled }}` guard for all `--scylladb-tls-*` args
  - [ ] 2.2: Wire `--scylladb-tls-cert=/etc/soteria/scylladb-tls/tls.crt`
  - [ ] 2.3: Wire `--scylladb-tls-key=/etc/soteria/scylladb-tls/tls.key`
  - [ ] 2.4: Wire `--scylladb-tls-ca=/etc/soteria/scylladb-tls/ca.crt`
  - [ ] 2.5: Wire `--scylladb-tls-server-name={{ .Values.scylladb.external.tls.serverName }}` (if set)
- [ ] Task 3: Add conditional TLS volume and mount (AC: 3, 4)
  - [ ] 3.1: Add volume `scylladb-external-tls` from Secret `{{ .Values.scylladb.external.tls.secretName }}`, guarded by `external.tls.enabled`
  - [ ] 3.2: Add volumeMount at `/etc/soteria/scylladb-tls`, guarded by `external.tls.enabled`
- [ ] Task 4: Verify no managed resources render (AC: 1)
  - [ ] 4.1: Confirm `templates/scylladb/` templates are already guarded by `scylladb.mode=managed` (from Story 16.3)
- [ ] Task 5: Verify rendering (AC: 1–5)
  - [ ] 5.1: Run `helm template` with `scylladb.mode=external, external.tls.enabled=true` — verify TLS args and volumes
  - [ ] 5.2: Run `helm template` with `scylladb.mode=external, external.tls.enabled=false` — verify no TLS args or volumes
  - [ ] 5.3: Compare managed vs external renders to confirm correct resource diff

## Dev Notes

### Implementation Pattern

The controller flags from `pkg/apiserver/options.go` that must be wired for external mode:

```go
fs.StringVar(&o.ScyllaDBContactPoints, "scylladb-contact-points", "localhost:9042",
    "Comma-separated ScyllaDB contact points")
fs.StringVar(&o.ScyllaDBKeyspace, "scylladb-keyspace", "soteria",
    "ScyllaDB keyspace name")
fs.StringVar(&o.ScyllaDBLocalDC, "scylladb-local-dc", "",
    "Local ScyllaDB datacenter name for DC-aware routing")
fs.StringVar(&o.ScyllaDBDCReplication, "scylladb-dc-replication", "",
    "Auto-create keyspace with NetworkTopologyStrategy. Comma-separated dc:rf pairs")
fs.StringVar(&o.ScyllaDBTLSCert, "scylladb-tls-cert", "", "Path to ScyllaDB TLS client certificate")
fs.StringVar(&o.ScyllaDBTLSKey, "scylladb-tls-key", "", "Path to ScyllaDB TLS client key")
fs.StringVar(&o.ScyllaDBTLSCA, "scylladb-tls-ca", "", "Path to ScyllaDB TLS CA certificate")
fs.StringVar(&o.ScyllaDBTLSServerName, "scylladb-tls-server-name", "",
    "TLS server name for ScyllaDB certificate verification")
```

The Helm template conditional pattern for the Deployment args section:

```yaml
{{- if eq .Values.scylladb.mode "managed" }}
  - --scylladb-contact-points={{ include "soteria.fullname" . }}-scylladb-client.{{ .Release.Namespace }}.svc:9142
  - --scylladb-tls-cert=/etc/soteria/scylladb-client-tls/tls.crt
  # ... managed mTLS args
{{- else if eq .Values.scylladb.mode "external" }}
  - --scylladb-contact-points={{ .Values.scylladb.external.contactPoints }}
  {{- if .Values.scylladb.external.tls.enabled }}
  - --scylladb-tls-cert=/etc/soteria/scylladb-tls/tls.crt
  - --scylladb-tls-key=/etc/soteria/scylladb-tls/tls.key
  - --scylladb-tls-ca=/etc/soteria/scylladb-tls/ca.crt
  {{- if .Values.scylladb.external.tls.serverName }}
  - --scylladb-tls-server-name={{ .Values.scylladb.external.tls.serverName }}
  {{- end }}
  {{- end }}
{{- end }}
```

The existing kustomize overlay `hack/overlays/base/manager-args-patch.yaml` shows the full arg set for managed mode as a reference.

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/templates/controller/deployment.yaml` | UPDATE | Add external mode conditional args and TLS volume/mount blocks |
| `charts/soteria/values.yaml` | UPDATE | Ensure `scylladb.external.tls.serverName` field exists |

### Key Constraints

- External TLS mount path (`/etc/soteria/scylladb-tls`) must differ from managed mTLS path (`/etc/soteria/scylladb-client-tls`) to avoid confusion
- `--scylladb-local-dc` and `--scylladb-dc-replication` should only be rendered if the values are non-empty (they are shared between managed and external modes via `scylladb.localDC` and `scylladb.dcReplication`)
- The external TLS Secret is expected to contain standard `tls.crt`, `tls.key`, and `ca.crt` keys
- Depends on: Story 16.2 (controller templates must exist first)

### Project Structure Notes

- This story modifies the Deployment template created in Story 16.2 — no new template files needed
- The mode switching logic in the Deployment is the single source of truth for ScyllaDB connection configuration

### References

- [Source: `pkg/apiserver/options.go` — all `--scylladb-*` flags with defaults and descriptions]
- [Source: `hack/overlays/base/manager-args-patch.yaml` — managed mode ScyllaDB args including TLS cert paths]
- [Source: `hack/overlays/base/manager-scylladb-patch.yaml` — volume mounts for managed mode apiserver-tls and scylladb-client-tls]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
