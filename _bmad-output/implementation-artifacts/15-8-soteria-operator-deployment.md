# Story 15.8: Soteria Operator Deployment (Moved from 14.6)

Status: done

## Story

As a platform engineer,
I want Soteria deployed on both Minikube KVM2 clusters with the real Ceph VolumeReplicationClass,
so that the operator manages DR plans against real storage replication.

## Acceptance Criteria

**AC1: Soteria operator deployment**
Given ScyllaDB and KubeVirt are running on both Minikube clusters
When the Soteria deployment script is executed
Then Soteria operator (API server + controller) is deployed on both clusters
And console plugin is excluded (Minikube has no OCP Console)

**AC2: Site-name configuration**
Given the Soteria deployment
When the controller manager starts
Then east cluster runs with `--site-name=east` and `--scylladb-local-dc=east`
And west cluster runs with `--site-name=west` and `--scylladb-local-dc=west`

**AC3: VolumeReplicationClass reference**
Given the Rook-Ceph VolumeReplicationClass from Story 14.2
When DRPlans are created
Then they reference `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`

**AC4: APIService availability smoke test**
Given Soteria is deployed
When checking APIService status
Then `v1alpha1.soteria.io` is Available on both clusters

**AC5: Cross-DC replication smoke test**
Given Soteria is running with ScyllaDB cross-DC
When a test resource is created via the Soteria API on east
Then it is visible on west after ScyllaDB replication delay

## Tasks / Subtasks

- [x] Task 1: Build Soteria container image and load into Minikube (AC: 1)
  - [x] 1.1: Build operator image via `make docker-build IMG=localhost/soteria:dev CONTAINER_TOOL=podman`
  - [x] 1.2: Load image into both Minikube clusters via `minikube image load localhost/soteria:dev -p east` and `-p west`
  - [x] 1.3: Verify image is available in both clusters (`minikube ssh -p east -- "sudo crictl images | grep soteria"`)

- [x] Task 2: Create Kustomize overlays for Minikube multisite deployment (AC: 1, 2)
  - [x] 2.1: Create `hack/multisite/overlays/base/manager-args-patch.yaml` — ScyllaDB connection args adapted for Minikube (contact-points uses `soteria-scylladb-client.soteria.svc:9142`, `--scylladb-dc-replication=east:1,west:1`, TLS cert paths, apiserver TLS, disable admission plugins)
  - [x] 2.2: Create `hack/multisite/overlays/base/manager-scylladb-patch.yaml` — volume mounts for apiserver-tls and scylladb-client-tls secrets (mirrors `hack/overlays/base/manager-scylladb-patch.yaml`)
  - [x] 2.3: Create `hack/multisite/overlays/base/apiserver-rbac.yaml` — auth-delegation, admission, flowcontrol RBAC (mirrors `hack/overlays/base/apiserver-rbac.yaml`)
  - [x] 2.4: Create `hack/multisite/overlays/base/apiserver-cert.yaml` — Certificate for the apiserver service TLS (mirrors `hack/overlays/base/apiserver-cert.yaml`)
  - [x] 2.5: Create `hack/multisite/overlays/soteria/base/kustomization.yaml` — Soteria overlay using config/default as base, with patches, apiserver-rbac, apiserver-cert, NO console plugin, NO storageclass-xfs, NO serviceexport (Cilium replaces Submariner)
  - [x] 2.6: Create `hack/multisite/overlays/east/manager-dc-patch.yaml` — `--scylladb-local-dc=east` and `--site-name=east` args
  - [x] 2.7: Create `hack/multisite/overlays/west/manager-dc-patch.yaml` — `--scylladb-local-dc=west` and `--site-name=west` args
  - [x] 2.8: Create `hack/multisite/overlays/soteria/east/kustomization.yaml` — inherits soteria/base, adds east manager-dc-patch
  - [x] 2.9: Create `hack/multisite/overlays/soteria/west/kustomization.yaml` — inherits soteria/base, adds west manager-dc-patch

- [x] Task 3: Create `hack/multisite/deploy-soteria.sh` (AC: 1, 2, 4, 5)
  - [x] 3.1: Env-var configuration block (IMG, EAST_CONTEXT, WEST_CONTEXT, NAMESPACE, KUSTOMIZE path)
  - [x] 3.2: Prerequisite checks (kubectl, kustomize, Minikube clusters running, ScyllaDB ready on both, KubeVirt Deployed on both, image loaded in Minikube)
  - [x] 3.3: Set image in kustomize: `cd config/manager && kustomize edit set image controller=${IMG}`
  - [x] 3.4: Build east overlay and apply: `kustomize build --load-restrictor LoadRestrictionsNone hack/multisite/overlays/soteria/east | kubectl --context=east apply --server-side --force-conflicts -f -`
  - [x] 3.5: Wait for Soteria controller-manager deployment rollout on east (timeout 5m)
  - [x] 3.6: Build west overlay and apply: `kustomize build --load-restrictor LoadRestrictionsNone hack/multisite/overlays/soteria/west | kubectl --context=west apply --server-side --force-conflicts -f -`
  - [x] 3.7: Wait for Soteria controller-manager deployment rollout on west (timeout 5m)
  - [x] 3.8: Verify APIService `v1alpha1.soteria.io` is Available on both clusters

- [x] Task 4: Cross-DC replication smoke test (AC: 5)
  - [x] 4.1: Create a cluster-scoped DRPlan `smoke-test-plan` via the Soteria API on east cluster with `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`, `primarySite: east`, `secondarySite: west`
  - [x] 4.2: Wait and verify the DRPlan is visible on west cluster via the Soteria API (ScyllaDB replication)
  - [x] 4.3: Delete the test DRPlan

- [x] Task 5: README and finalization
  - [x] 5.1: Update `hack/multisite/README.md` with Soteria deployment section (prerequisites, build, deploy, verify, troubleshooting)
  - [x] 5.2: Add idempotency checks throughout script (check deployment exists before apply, etc.)

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a shell script, Kustomize overlay additions, and a README update. No Go code changes. The outputs are:
- `hack/multisite/deploy-soteria.sh` — main deployment script
- `hack/multisite/overlays/{base,east,west}/` — Kustomize overlay additions for Soteria-specific patches
- `hack/multisite/README.md` — updated with Soteria deployment section

Soteria is the DR orchestration operator. It runs as a single binary (API server + controller) and requires ScyllaDB as its cross-site state store and KubeVirt as the workload runtime it protects. This story deploys it against real infrastructure (real Ceph RBD replication, real ScyllaDB cross-DC) for the first time.

### Critical: Image Build and Load Strategy

Soteria is NOT available from a public registry in the integration test environment. The image must be built locally and loaded into the Minikube clusters:

```bash
IMG="localhost/soteria:dev"
make docker-build IMG="${IMG}" CONTAINER_TOOL=podman
minikube image load "${IMG}" -p east
minikube image load "${IMG}" -p west
```

The `minikube image load` command copies the image from the local container runtime into the Minikube node's container image cache. After loading, the image is available with `imagePullPolicy: IfNotPresent`.

**Important:** The `config/manager/manager.yaml` uses `imagePullPolicy: Always` by default. The Kustomize overlay must either:
- Override `imagePullPolicy` to `IfNotPresent` (preferred for Minikube), OR
- Use a tag other than `latest` (e.g., `localhost/soteria:dev`) which Minikube nodes can satisfy from local cache

Add an image pull policy patch to the overlay:
```yaml
- op: replace
  path: /spec/template/spec/containers/0/imagePullPolicy
  value: IfNotPresent
```

### Critical: Overlay Architecture Adaptation

The existing `hack/overlays/` structure deploys Soteria on real OpenShift clusters (etl6/etl7) with:
- Submariner MCS for cross-cluster connectivity → replaced by Cilium global services (already handled in 14.5)
- `ontap-san-xfs` StorageClass → replaced by `rook-ceph-block`
- `soteria-internal` cert-manager Issuer → same (created in 14.5)
- ScyllaDB 2 members/rack → 1 member/rack (developer mode)

The `hack/multisite/overlays/` structure ALREADY has ScyllaDB overlays from Story 14.5. This story **adds** Soteria-specific patches to the existing overlay structure rather than duplicating the full base. The key additions:
- Manager args patch (ScyllaDB connection + API server TLS + admission disabling)
- Manager volume mounts (TLS secrets)
- API server RBAC (auth-delegation)
- API server Certificate
- Per-DC patches (site-name + scylladb-local-dc)

### Critical: ScyllaDB Contact Points for Cilium

In the existing `hack/overlays/base/manager-args-patch.yaml`, the ScyllaDB contact point is:
```
--scylladb-contact-points=soteria-scylladb-client.soteria.svc:9142
```

With Cilium global services (from Story 14.5), the same FQDN is used (`svc.cluster.local`, not `svc.clusterset.local`). The contact points remain the same. Each Soteria instance connects to its local ScyllaDB DC via the in-cluster service — ScyllaDB handles cross-DC replication internally.

### Critical: DC Replication Factor

The existing deployment uses `--scylladb-dc-replication=etl6:2,etl7:2` (RF=2 per DC with 2 members). For Minikube, use `--scylladb-dc-replication=east:1,west:1` (RF=1 per DC with 1 member in developer mode).

### Critical: Console Plugin Exclusion

Minikube clusters don't have OCP Console. The deployment must NOT include:
- Console plugin Deployment
- ConsolePlugin CR
- Console-related RBAC

The existing `hack/overlays/base/console-plugin.yaml` is deployed separately in the stretched-local-test.sh script. For the Minikube deployment, simply don't reference it in the overlay or the deploy script.

### Critical: No Noop VR Controller

The noop VolumeReplication controller from Epic 12 (`pkg/controller/volumereplication/`) is NOT deployed in this environment. Real CSI Addons sidecar (deployed in Story 14.2 via Rook-Ceph) handles VR/VGR reconciliation. Soteria's CSI Extension driver creates VR/VGR CRs, and the CSI Addons controller reconciles them against Ceph.

### Critical: Kustomize Build with LoadRestrictionsNone

The `kustomize build` command requires `--load-restrictor LoadRestrictionsNone` when the overlay references `../../../config/default` (crossing the hack/ directory boundary). Follow the same pattern from `hack/stretched-local-test.sh`:

```bash
kustomize build --load-restrictor LoadRestrictionsNone hack/multisite/overlays/east
```

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters + Cilium | 14.1 | Both `east` and `west` clusters running with Cluster Mesh |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block`, VolumeReplicationClass `rook-ceph-rbd-vrc`, CSI Addons running |
| KubeVirt | 14.3 | KubeVirt Deployed on both clusters (VM webhook requires KubeVirt CRDs) |
| ScyllaDB | 14.5 | Cross-DC ScyllaDB cluster running on both clusters with mTLS, cert-manager Issuer `soteria-internal` in `soteria` namespace |

### Existing Deployment Pattern Comparison

| Aspect | Existing (etl6/etl7) | This Story (east/west) |
|--------|----------------------|------------------------|
| Image source | Registry (`quay.io/raffaelespazzoli/soteria:latest`) | Local build + `minikube image load` |
| ScyllaDB contact | `soteria-scylladb-client.soteria.svc:9142` | Same (Cilium handles cross-cluster) |
| DC replication | `etl6:2,etl7:2` | `east:1,west:1` |
| Site names | `etl6` / `etl7` | `east` / `west` |
| TLS | cert-manager `soteria-internal` Issuer | Same (created in 14.5) |
| Console plugin | Deployed (ConsolePlugin CR) | **NOT deployed** (no OCP Console in Minikube) |
| Image pull | `Always` (from registry) | `IfNotPresent` (local Minikube cache) |
| APIServer | Port 6443, TLS cert | Same |
| Cross-cluster | Submariner MCS | Cilium global services |

### Script Conventions to Follow

Follow the same conventions from Stories 14.1-14.5:

- `set -euo pipefail` at top
- Env-var-driven configuration block at top of script
- `keast()` / `kwest()` helpers with explicit `--context`
- Cluster profiles: `east` and `west` (contexts match profile names)
- Idempotent operations (`kubectl apply --server-side --force-conflicts`)
- Status messages via `info()`, `warn()`, `error()`, `fatal()` helpers
- Prerequisite checks at script start
- `SCRIPT_DIR` derived from script location

### Kubeconfig / Context Pattern

```bash
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"
NAMESPACE="${NAMESPACE:-soteria}"
IMG="${IMG:-localhost/soteria:dev}"

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }
```

### Deploy Sequence

1. Verify prerequisites (Minikube clusters, ScyllaDB ready, KubeVirt Deployed, image loaded)
2. Set image in kustomize config
3. Build and apply east overlay (Soteria deploys, connects to east ScyllaDB DC)
4. Wait for controller-manager rollout on east
5. Build and apply west overlay (Soteria deploys, connects to west ScyllaDB DC)
6. Wait for controller-manager rollout on west
7. Verify APIService Available on both clusters
8. Run cross-DC replication smoke test (create DRPlan on east, verify on west)
9. Clean up smoke test resources

### APIService Availability Check

```bash
check_apiservice() {
  local ctx="$1"
  local status
  status=$(kubectl --context="${ctx}" get apiservice v1alpha1.soteria.io \
    -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null) || true
  if [[ "${status}" == "True" ]]; then
    return 0
  fi
  return 1
}
```

The APIService becomes Available once:
- The Soteria pod is Running and passing readiness probes (`/readyz` on port 6443 HTTPS)
- The kube-apiserver can proxy requests to the Soteria service

### Cross-DC Replication Smoke Test

Create a DRPlan on east and verify it appears on west. DRPlan is cluster-scoped (no namespace):

```bash
kubectl --context=east apply -f - <<'EOF'
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: smoke-test-plan
spec:
  maxConcurrentFailovers: 2
  primarySite: east
  secondarySite: west
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: rook-ceph-rbd-vrc
EOF

# Wait for cross-DC replication (ScyllaDB eventual consistency, up to 60s)
for i in $(seq 1 30); do
  if kubectl --context=west get drplan smoke-test-plan &>/dev/null; then
    echo "Cross-DC replication verified"
    break
  fi
  sleep 2
done
```

### Potential Failure Modes

1. **Image not found** — `minikube image load` must run BEFORE deploy. Error manifests as `ImagePullBackOff` on the pod.
2. **ScyllaDB connection timeout** — Soteria pod won't become Ready if it can't connect to ScyllaDB. Check that `soteria-scylladb-client.soteria.svc:9142` resolves and TLS certs are valid.
3. **APIService stays Unavailable** — Typically means the pod isn't Ready (readiness probe fails on `/readyz`). Check pod logs: `kubectl -n soteria logs deployment/soteria-controller-manager -c manager`
4. **TLS cert not found** — cert-manager must have issued `soteria-apiserver-tls` and `soteria-scylladb-client-tls` secrets. These are issued by the `soteria-internal` Issuer created in Story 14.5.
5. **Cross-DC replication delay** — ScyllaDB with RF=1 in developer mode may have faster or slower CDC propagation. The smoke test should allow up to 60s.
6. **Kustomize build failure** — Missing `--load-restrictor LoadRestrictionsNone` when crossing directory boundaries. Also verify the `config/manager` image edit persists (kustomize edit modifies in-place).
7. **RBAC errors** — APIServer auth-delegation ClusterRoleBindings must exist. The `apiserver-rbac.yaml` creates these. Check `kubectl auth can-i` from the Soteria service account.

### File Structure

```
hack/multisite/
├── deploy-soteria.sh                    # Soteria deployment script (NEW — this story)
├── overlays/
│   ├── base/
│   │   ├── kustomization.yaml           # MODIFIED — add Soteria resources/patches
│   │   ├── manager-args-patch.yaml      # NEW — ScyllaDB/APIServer args
│   │   ├── manager-scylladb-patch.yaml  # NEW — TLS volume mounts
│   │   ├── apiserver-rbac.yaml          # NEW — auth-delegation RBAC
│   │   └── apiserver-cert.yaml          # NEW — APIServer TLS Certificate
│   ├── east/
│   │   ├── kustomization.yaml           # MODIFIED — add manager-dc-patch
│   │   ├── manager-dc-patch.yaml        # NEW — --site-name=east --scylladb-local-dc=east
│   │   └── scyllacluster-patch.yaml     # From Story 14.5
│   └── west/
│       ├── kustomization.yaml           # MODIFIED — add manager-dc-patch
│       ├── manager-dc-patch.yaml        # NEW — --site-name=west --scylladb-local-dc=west
│       └── scyllacluster-patch.yaml     # From Story 14.5
├── setup-clusters.sh                    # From Story 14.1
├── setup-rook-ceph.sh                   # From Story 14.2
├── setup-kubevirt.sh                    # From Story 14.3
├── validate-fedora-vm.sh               # From Story 14.4
├── setup-scylladb.sh                    # From Story 14.5
├── manifests/                           # From Story 14.2
├── teardown.sh                          # From Story 14.1
└── README.md                            # Updated with Soteria section
```

### Key Files to Mirror from Existing Overlays

The following files from `hack/overlays/` should be adapted for `hack/multisite/overlays/`:

| Source File | Adaptation Needed |
|-------------|-------------------|
| `hack/overlays/base/manager-args-patch.yaml` | Change `--scylladb-dc-replication=etl6:2,etl7:2` → `east:1,west:1`, add `--disable-admission-plugins`, add imagePullPolicy override |
| `hack/overlays/base/manager-scylladb-patch.yaml` | Copy as-is (same TLS volume mounts) |
| `hack/overlays/base/apiserver-rbac.yaml` | Copy as-is (same RBAC) |
| `hack/overlays/base/apiserver-cert.yaml` | Copy as-is (Certificate for API server TLS) |
| `hack/overlays/etl6/manager-dc-patch.yaml` | Change `etl6` → `east` |
| `hack/overlays/etl7/manager-dc-patch.yaml` | Change `etl7` → `west` |
| `hack/overlays/base/serviceexport.yaml` | NOT NEEDED — Cilium replaces Submariner |
| `hack/overlays/base/storageclass-xfs.yaml` | NOT NEEDED — Rook-Ceph SC already exists from 14.2 |
| `hack/overlays/base/console-plugin.yaml` | NOT NEEDED — no OCP Console in Minikube |
| `hack/overlays/base/scylladb-tls-config.yaml` | Already handled in 14.5 overlays |
| `hack/overlays/base/scylladb-tls-patch.yaml` | Already handled in 14.5 overlays |

### Overlay Integration Design

The Story 14.5 overlays already have ScyllaDB resources. Soteria's deployment resources come from `config/default`. The base kustomization must include BOTH:
- ScyllaDB resources (from 14.5 — ScyllaCluster, TLS config, global service annotation)
- Soteria operator resources (from `config/default` — RBAC, manager, webhook, certmanager, prometheus, apiservice, scylladb)

**Important conflict:** `config/default` already includes `../scylladb` which defines a ScyllaCluster. Since Story 14.5 already deploys ScyllaDB via its own overlays, the approach should be:
- Use `config/default` as the base for Soteria (it brings manager, RBAC, apiservice, webhook, certmanager, prometheus)
- EXCLUDE or patch out the ScyllaDB resource that comes from `config/default` (since ScyllaDB is already deployed by `setup-scylladb.sh` + its own Kustomize overlays)
- OR: structure the overlay so Soteria deployment is separate from ScyllaDB deployment (deploy script applies Soteria after ScyllaDB is confirmed running)

**Recommended approach:** Create a Soteria-specific kustomization that uses `config/default` as base but patches/removes the ScyllaDB resources (since ScyllaDB is already running from 14.5). Use a `patchesStrategicMerge` or simply accept that `kustomize build` will include ScyllaDB manifests which `kubectl apply --server-side --force-conflicts` will harmlessly re-apply (idempotent — ScyllaDB is already running with the same spec).

### DRPlan Spec for Smoke Test

The DRPlan created in the smoke test (and later in Story 14.7) must use:

```yaml
spec:
  maxConcurrentFailovers: 2
  primarySite: east
  secondarySite: west
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: rook-ceph-rbd-vrc
```

This references the VolumeReplicationClass `rook-ceph-rbd-vrc` created in Story 14.2.

### Testing Standards

No Go tests for this story — validation is via the deploy script's built-in verification steps (deployment rollout, APIService Available, cross-DC replication smoke test). Story 14.7 provides comprehensive integration testing.

### Previous Story Intelligence

**From Story 14.1 (Minikube KVM2 Cluster Provisioning):**
- Cluster profiles/contexts are `east` and `west`
- 4 nodes per cluster (1 CP + 3 workers)
- MetalLB for LoadBalancer IPs

**From Story 14.2 (Rook-Ceph Deployment):**
- VolumeReplicationClass `rook-ceph-rbd-vrc` available on both clusters
- StorageClass `rook-ceph-block` for PVCs
- CSI Addons controller running (reconciles VR/VGR CRs)

**From Story 14.3 (KubeVirt Deployment):**
- KubeVirt Deployed on both clusters with nested virtualization (KVM)
- CDI deployed for DataVolume support
- `kubevirt.io/v1` VirtualMachine CRD available (required for Soteria's VM webhook)

**From Story 14.5 (ScyllaDB Deployment):**
- ScyllaDB deployed in `soteria` namespace on both clusters
- cert-manager installed with `soteria-internal` Issuer in `soteria` namespace
- `hack/multisite/overlays/{base,east,west}/` structure created with ScyllaDB resources
- mTLS between ScyllaDB nodes and clients
- Cilium global service annotation (`service.cilium.io/global: "true"`) for cross-cluster discovery
- Kustomize build uses `--load-restrictor LoadRestrictionsNone`

**Epic 15 Context (this story moved from 14.6 to 15.8):**
- All Epic 15 Go code changes (Stories 15.1-15.6: resync guard, ShadowPV, reprotect simplification) will be built into the operator image before deployment
- The operator image includes the ResyncVolume driver method, planned failover resync guard, reprotect handler simplification, and ShadowPV controllers
- Deployment approach TBD: standalone `deploy-soteria.sh` script OR integrated as a test fixture in Story 15.9's `BeforeSuite`

**From existing `hack/stretched-local-test.sh`:**
- Soteria deployment via kustomize build + apply --server-side --force-conflicts
- APIService verification pattern (`get apiservice v1alpha1.soteria.io`)
- Deployment rollout status wait pattern
- DRPlan creation with site topology fields
- Console plugin deployment is a separate step (skip for Minikube)

### Project Structure Notes

- Script goes in `hack/multisite/` — follows convention from Stories 14.1-14.5
- Overlay additions go in existing `hack/multisite/overlays/` structure
- No Makefile target additions needed (user runs the script directly)
- `config/manager` is temporarily modified by `kustomize edit set image` — this is normal and matches the existing pattern from `hack/stretched-local-test.sh`

### References

- [Source: epics.md#Story 14.6] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing Soteria deployment pattern (kustomize build + apply + rollout + APIService check)
- [Source: hack/overlays/base/kustomization.yaml] — base overlay structure (resources + patches + replacements)
- [Source: hack/overlays/base/manager-args-patch.yaml] — ScyllaDB connection and APIServer TLS args
- [Source: hack/overlays/base/manager-scylladb-patch.yaml] — TLS volume mounts
- [Source: hack/overlays/base/apiserver-rbac.yaml] — auth-delegation RBAC
- [Source: hack/overlays/base/apiserver-cert.yaml] — APIServer TLS Certificate
- [Source: hack/overlays/etl6/manager-dc-patch.yaml] — per-DC args pattern (site-name + scylladb-local-dc)
- [Source: config/default/kustomization.yaml] — default kustomize base (includes all operator resources)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — VolumeReplicationDriverConfig struct (type + volumeReplicationClass)
- [Source: Story 14.2] — VolumeReplicationClass `rook-ceph-rbd-vrc`
- [Source: Story 14.3] — KubeVirt deployment with nested virtualization, CDI
- [Source: Story 14.5] — ScyllaDB overlays, cert-manager setup, Cilium global services
- [Source: project-context.md] — Soteria architecture, single binary, aggregated API server, site-aware reconciliation

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

- Kustomize build validated for both east/west overlays
- All unit tests pass (no Go code changes in this story)
- Pre-existing lint issues in Go code (dupl, errcheck, lll) not introduced by this story
- Bash syntax validation passed for deploy-soteria.sh

### Completion Notes List

- Created separate Soteria overlay tree (`hack/multisite/overlays/soteria/{base,east,west}/`) to avoid breaking existing `setup-scylladb.sh` which uses `kubectl apply -k` on the ScyllaDB overlays. The Soteria overlay references `config/default` and requires `--load-restrictor LoadRestrictionsNone` (crossing directory boundaries).
- Patch files placed in existing `base/`, `east/`, `west/` directories for shared use; kustomization.yaml files in `soteria/` subdirectory reference them via relative paths.
- `config/default` includes ScyllaDB manifests — these are harmlessly re-applied when deploy-soteria.sh runs (ScyllaDB already deployed by setup-scylladb.sh, idempotent with `--server-side --force-conflicts`).
- The imagePullPolicy override (`IfNotPresent`) is applied via the manager-args-patch to work with Minikube's local image cache.
- All cert-manager Certificate resources patched to use `soteria-internal` Issuer (created in Story 14.5).
- Kustomize replacements correctly wire APIService CA injection annotation and apiserver cert DNS names from the Service name/namespace.
- Deploy script includes: image build, image load, prerequisite checks (clusters, ScyllaDB, KubeVirt), kustomize apply, rollout wait, APIService verification, and cross-DC replication smoke test.
- Idempotency: script detects existing healthy deployment and logs re-apply message; all kubectl operations use `--server-side --force-conflicts`.

### File List

- hack/multisite/deploy-soteria.sh (NEW)
- hack/multisite/overlays/base/manager-args-patch.yaml (NEW)
- hack/multisite/overlays/base/manager-scylladb-patch.yaml (NEW)
- hack/multisite/overlays/base/apiserver-rbac.yaml (NEW)
- hack/multisite/overlays/base/apiserver-cert.yaml (NEW)
- hack/multisite/overlays/east/manager-dc-patch.yaml (NEW)
- hack/multisite/overlays/west/manager-dc-patch.yaml (NEW)
- hack/multisite/overlays/soteria/base/kustomization.yaml (NEW)
- hack/multisite/overlays/soteria/east/kustomization.yaml (NEW)
- hack/multisite/overlays/soteria/west/kustomization.yaml (NEW)
- hack/multisite/README.md (MODIFIED — added Soteria deployment section + updated file structure)
- .gitignore (MODIFIED — fixed binary ignore patterns to use root-relative /soteria, /console-proxy)
- cmd/console-proxy/main.go (MODIFIED — go fmt alignment fix, pre-existing)
- _bmad-output/implementation-artifacts/sprint-status.yaml (MODIFIED — status → in-progress → review)
- _bmad-output/implementation-artifacts/15-8-soteria-operator-deployment.md (MODIFIED — tasks, status, dev record)

### Change Log

- 2026-07-05: Implemented Story 15.8 — Soteria operator deployment for Minikube multisite environment. Created deploy script, Kustomize overlays, and README documentation.

### Review Findings

- [x] [Review][Patch] Imported `config/default` still pulls in Prometheus `ServiceMonitor`, so Minikube deploy can fail without Prometheus Operator CRDs [hack/multisite/overlays/soteria/base/kustomization.yaml:13]
- [x] [Review][Patch] ScyllaDB readiness is only warned on, but the story requires ScyllaDB to be ready before deploying Soteria [hack/multisite/deploy-soteria.sh:115]
- [x] [Review][Patch] KubeVirt prerequisite check only verifies the `virtualmachines.kubevirt.io` CRD exists, not that KubeVirt is actually deployed and healthy [hack/multisite/deploy-soteria.sh:121]
- [x] [Review][Patch] Smoke test does not verify `rook-ceph-rbd-vrc` exists before creating the DRPlan required by AC3 [hack/multisite/deploy-soteria.sh:251]
- [x] [Review][Patch] Smoke test uses a fixed `smoke-test-plan` name and then deletes it, which can collide with a real pre-existing DRPlan on re-run [hack/multisite/deploy-soteria.sh:251]
- [x] [Review][Patch] README verification step applies `config/samples/`, but the sample DRPlan uses the `noop` driver instead of the required Ceph-backed `csi-extension` configuration [hack/multisite/README.md:643]
