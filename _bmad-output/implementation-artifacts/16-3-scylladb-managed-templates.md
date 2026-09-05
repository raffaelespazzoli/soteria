# Story 16.3: ScyllaDB Managed Templates

Status: done

## Story

As a platform engineer,
I want Helm templates for a managed ScyllaDB deployment that are only rendered when `scylladb.mode=managed`,
so that ScyllaDB is automatically provisioned as part of the Soteria installation.

## Acceptance Criteria

**AC1: ScyllaCluster CR renders when managed mode is selected**
Given `scylladb.mode=managed`
When I run `helm template`
Then a ScyllaCluster CR is rendered with datacenter from values, rack configuration, members, storage, externalSeeds, developerMode flag, and resources

**AC2: ScyllaCluster broadcastOptions vary by networking mode**
Given `scylladb.mode=managed`
When `networking.mode=submariner`
Then the ScyllaCluster `broadcastOptions` are configured for Submariner service discovery
When `networking.mode=cilium`
Then the ScyllaCluster `broadcastOptions` are configured for Cilium global services

**AC3: mTLS ConfigMap is rendered**
Given `scylladb.mode=managed`
When I render the chart
Then a ConfigMap containing `scylla.yaml` with mTLS configuration is present

**AC4: cert-manager Certificate for ScyllaDB**
Given `scylladb.mode=managed`
When I render the chart
Then a cert-manager Certificate for ScyllaDB serving cert is rendered referencing `tls.issuerRef`

**AC5: Submariner ServiceExport is conditional**
Given `scylladb.mode=managed` and `networking.mode=submariner`
When I render the chart
Then a ServiceExport resource is rendered for the ScyllaDB headless service
When `networking.mode=cilium`
Then no ServiceExport resource is rendered

**AC6: Cilium global service annotation is conditional**
Given `scylladb.mode=managed` and `networking.mode=cilium`
When I render the chart
Then the ScyllaDB headless service has annotation `service.cilium.io/global: "true"`
When `networking.mode=submariner`
Then the annotation is absent

**AC7: No ScyllaDB resources in external mode**
Given `scylladb.mode=external`
When I render the chart
Then no ScyllaCluster, ScyllaDB ConfigMap, ScyllaDB Certificate, ServiceExport, or Cilium-annotated service is rendered

## Tasks / Subtasks

- [x] Task 1: Create ScyllaCluster CR template (AC: 1, 2, 7)
  - [x] 1.1: Create `templates/scylladb/scyllacluster.yaml` guarded by `{{- if eq .Values.scylladb.mode "managed" }}`
  - [x] 1.2: Wire datacenter name from `scylladb.localDC`, rack config (name, members, storage, resources) from `scylladb.managed.*`
  - [x] 1.3: Wire `externalSeeds` from `scylladb.managed.externalSeeds` (list)
  - [x] 1.4: Wire `developerMode` from `scylladb.managed.developerMode`
  - [x] 1.5: Add conditional `broadcastOptions` block — for Submariner: `nodes.type: ServiceClusterIP`, `clients.type: ServiceClusterIP`; for Cilium: `nodes.type: PodIP`, `clients.type: PodIP`
  - [x] 1.6: Wire `scyllaConfig: scylla-config` and rack-level `volumes`/`volumeMounts` for mTLS certs (based on `hack/multisite/overlays/base/scylladb-tls-patch.yaml`)
- [x] Task 2: Create mTLS ConfigMap template (AC: 3, 7)
  - [x] 2.1: Create `templates/scylladb/configmap-mtls.yaml` guarded by managed mode, containing `scylla.yaml` with `server_encryption_options` and `client_encryption_options` (mirror `hack/multisite/overlays/base/scylladb-tls-config.yaml`)
- [x] Task 3: Create ScyllaDB Certificate template (AC: 4, 7)
  - [x] 3.1: Create `templates/scylladb/certificate.yaml` guarded by managed mode, referencing `tls.issuerRef`, with server+client auth usages and DNS names for the ScyllaDB headless service (mirror `config/certmanager/scylladb-serving-cert.yaml`)
- [x] Task 4: Create ServiceExport template (AC: 5, 7)
  - [x] 4.1: Create `templates/scylladb/serviceexport.yaml` guarded by `scylladb.mode=managed AND networking.mode=submariner`
  - [x] 4.2: Export the ScyllaDB client headless service (mirror `hack/overlays/base/serviceexport.yaml`)
- [x] Task 5: Add Cilium global service annotation (AC: 6, 7)
  - [x] 5.1: In `scyllacluster.yaml`, add conditional annotation `service.cilium.io/global: "true"` when `networking.mode=cilium`
- [x] Task 6: Verify rendering (AC: 1–7)
  - [x] 6.1: Run `helm template` with `scylladb.mode=managed, networking.mode=submariner` — verify ScyllaCluster + ServiceExport present
  - [x] 6.2: Run `helm template` with `scylladb.mode=managed, networking.mode=cilium` — verify Cilium annotation, no ServiceExport
  - [x] 6.3: Run `helm template` with `scylladb.mode=external` — verify no ScyllaDB resources

## Dev Notes

### Implementation Pattern

The ScyllaCluster CR should follow the pattern from `hack/multisite/overlays/base/scyllacluster.yaml`:

```yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: soteria-scylladb
spec:
  version: "2026.1.3"
  developerMode: true
  exposeOptions:
    broadcastOptions:
      nodes:
        type: PodIP       # Cilium: PodIP; Submariner: ServiceClusterIP
      clients:
        type: PodIP
  externalSeeds:
    - REPLACED_BY_OVERLAY
  datacenter:
    name: REPLACED_BY_OVERLAY
    racks:
      - name: rack1
        members: 1
        storage:
          capacity: 5Gi
          storageClassName: rook-ceph-block-xfs
```

The mTLS ConfigMap from `hack/multisite/overlays/base/scylladb-tls-config.yaml`:

```yaml
data:
  scylla.yaml: |
    server_encryption_options:
      internode_encryption: all
      certificate: /etc/scylla/certmanager-tls/tls.crt
      keyfile: /etc/scylla/certmanager-tls/tls.key
      truststore: /etc/scylla/certmanager-ca/ca-bundle.crt
      require_client_auth: true
    client_encryption_options:
      enabled: true
      certificate: /etc/scylla/certmanager-tls/tls.crt
      keyfile: /etc/scylla/certmanager-tls/tls.key
      truststore: /etc/scylla/combined-ca/ca-bundle.crt
      require_client_auth: true
```

The TLS patch from `hack/multisite/overlays/base/scylladb-tls-patch.yaml` adds rack-level volumes for `certmanager-serving`, `certmanager-ca`, and `combined-ca` secrets, with volume mounts at `/etc/scylla/certmanager-tls`, `/etc/scylla/certmanager-ca`, `/etc/scylla/combined-ca`.

The Submariner ServiceExport from `hack/overlays/base/serviceexport.yaml`:

```yaml
apiVersion: multicluster.x-k8s.io/v1alpha1
kind: ServiceExport
metadata:
  name: soteria-scylladb-client
  namespace: soteria
```

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/templates/scylladb/scyllacluster.yaml` | NEW | ScyllaCluster CR with conditional broadcastOptions, conditional on managed mode |
| `charts/soteria/templates/scylladb/configmap-mtls.yaml` | NEW | ConfigMap with `scylla.yaml` mTLS config, conditional on managed mode |
| `charts/soteria/templates/scylladb/certificate.yaml` | NEW | cert-manager Certificate for ScyllaDB serving cert, conditional on managed mode |
| `charts/soteria/templates/scylladb/serviceexport.yaml` | NEW | Submariner ServiceExport for ScyllaDB headless service, conditional on managed + submariner |

### Key Constraints

- All ScyllaDB templates must be guarded by `{{- if eq .Values.scylladb.mode "managed" }}`
- `broadcastOptions` differ between networking modes: Cilium uses `PodIP`, Submariner uses `ServiceClusterIP`
- The `scyllaConfig` field on the rack references the ConfigMap by name (`scylla-config`)
- scylla-operator (v1.21) may not propagate rack-level volumes/volumeMounts to the StatefulSet — note this as a known issue for the install script to handle
- Depends on: Story 16.1 (chart skeleton and values.yaml)

### Project Structure Notes

- All ScyllaDB templates go under `charts/soteria/templates/scylladb/` subdirectory
- Networking-mode conditional logic uses `{{- if eq .Values.networking.mode "submariner" }}` and `{{- if eq .Values.networking.mode "cilium" }}`

### References

- [Source: `hack/multisite/overlays/base/scyllacluster.yaml` — base ScyllaCluster CR for multi-DC deployment]
- [Source: `hack/multisite/overlays/base/scylladb-tls-config.yaml` — mTLS ConfigMap with scylla.yaml encryption options]
- [Source: `hack/multisite/overlays/base/scylladb-tls-patch.yaml` — rack-level TLS volumes and volume mounts]
- [Source: `hack/overlays/base/serviceexport.yaml` — Submariner ServiceExport for ScyllaDB client service]
- [Source: `hack/overlays/base/scylladb-tls-config.yaml` — alternative mTLS config for etl6/etl7 deployment]
- [Source: `hack/overlays/base/scylladb-tls-patch.yaml` — alternative TLS patch with combined-ca ConfigMap]
- [Source: `config/certmanager/scylladb-serving-cert.yaml` — ScyllaDB serving Certificate with server+client auth usages]
- [Source: `hack/multisite/setup-scylladb.sh` — full ScyllaDB cross-DC deployment script with cert-manager CA setup]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
None — all tasks completed without errors.

### Completion Notes List
- ✅ Task 1: Created ScyllaCluster CR template with conditional broadcastOptions (Submariner: ServiceClusterIP, Cilium: PodIP), datacenter/rack config wired from values, TLS volumes/volumeMounts, scyllaConfig reference, and `required` validation on localDC.
- ✅ Task 2: Created mTLS ConfigMap template mirroring `hack/multisite/overlays/base/scylladb-tls-config.yaml` with server_encryption_options and client_encryption_options.
- ✅ Task 3: Created cert-manager Certificate template with templated dnsNames using fullname helper, issuerRef from `tls.issuerRef`, and server+client auth usages.
- ✅ Task 4: Created ServiceExport template with dual guard (`managed` AND `submariner`).
- ✅ Task 5: Added conditional Cilium annotation `service.cilium.io/global: "true"` on ScyllaCluster CR metadata when `networking.mode=cilium`.
- ✅ Task 6: All three `helm template` scenarios verified: managed+submariner, managed+cilium, external mode.
- ⚠️ Known: scylla-operator (v1.21) may not propagate rack-level volumes/volumeMounts to StatefulSet — install script handles this post-deploy.

### File List
- `charts/soteria/templates/scylladb/scyllacluster.yaml` (NEW)
- `charts/soteria/templates/scylladb/configmap-mtls.yaml` (NEW)
- `charts/soteria/templates/scylladb/certificate.yaml` (NEW)
- `charts/soteria/templates/scylladb/serviceexport.yaml` (NEW)
- `_bmad-output/implementation-artifacts/16-3-scylladb-managed-templates.md` (MODIFIED)
