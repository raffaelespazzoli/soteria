# ScyllaDB with Submariner

This guide walks through deploying ScyllaDB as a multi-datacenter cluster
across two Kubernetes clusters connected via
[Submariner](https://submariner.io/) Multicluster Services (MCS).
Soteria uses ScyllaDB for cross-site state replication (DRPlans,
DRExecutions) with eventual consistency
(`NetworkTopologyStrategy`, `LOCAL_ONE` reads/writes).

The reference deployment uses two clusters—**etl6** (seed DC) and
**etl7** (joining DC)—but the procedure generalises to any pair of
Submariner-connected clusters.

---

## Architecture overview

```text
┌──────────── Cluster etl6 ────────────┐   ┌──────────── Cluster etl7 ────────────┐
│  ScyllaDB DC "etl6" (seed)           │   │  ScyllaDB DC "etl7" (joins etl6)     │
│  ┌────────┐  ┌────────┐             │   │  ┌────────┐  ┌────────┐             │
│  │ node-0 │  │ node-1 │             │   │  │ node-0 │  │ node-1 │             │
│  └───┬────┘  └───┬────┘             │   │  └───┬────┘  └───┬────┘             │
│      │  gossip   │                   │   │      │  gossip   │                   │
│      └─────┬─────┘                   │   │      └─────┬─────┘                   │
│            │                         │   │            │                         │
│  ServiceExport ──────── Submariner MCS ──────── externalSeeds                  │
│  soteria-scylladb-client             │   │  soteria-scylladb-client             │
│            │                         │   │  .soteria.svc.clusterset.local       │
│  Soteria controller-manager          │   │  Soteria controller-manager          │
│  (--scylladb-local-dc=etl6)         │   │  (--scylladb-local-dc=etl7)         │
└──────────────────────────────────────┘   └──────────────────────────────────────┘
```

Cross-DC seed discovery uses Submariner MCS: etl6 exports the ScyllaDB
client headless service via a `ServiceExport`, and etl7 references it as
an external seed at
`soteria-scylladb-client.soteria.svc.clusterset.local`.

---

## Prerequisites

Before starting, verify the following on **both** clusters:

| Requirement | How to check |
|---|---|
| `kubectl` contexts `etl6` and `etl7` configured | `kubectl --context=etl6 cluster-info` |
| `kustomize` installed (or use `make kustomize`) | `kustomize version` |
| [cert-manager](https://cert-manager.io/) installed | `kubectl get crd certificates.cert-manager.io` |
| [scylla-operator](https://operator.docs.scylladb.com/) installed | `kubectl get crd scyllaclusters.scylla.scylladb.com` |
| Submariner with MCS API active | `kubectl get crd serviceexports.multicluster.x-k8s.io` |
| Issuer `soteria-internal` in `soteria` namespace | `kubectl -n soteria get issuer soteria-internal` |
| Container image available | `make docker-build docker-push IMG=<registry>/<image>:<tag>` |

!!! tip "Shared CA requirement"
    Both clusters **must** trust the same Certificate Authority for
    ScyllaDB internode mTLS. The `soteria-internal` Issuer on each
    cluster must reference the same CA key pair. Cross-CA trust is
    **not** supported.

---

## Step 1 — cert-manager and TLS setup

Soteria's Submariner overlay switches **all** cert-manager `Certificate`
resources from the default `soteria-ca` Issuer to `soteria-internal`.
This includes:

| Certificate | Purpose |
|---|---|
| `soteria-scylladb-serving` | ScyllaDB server-side TLS (internode + CQL) |
| `soteria-scylladb-client` | Soteria → ScyllaDB client mTLS |
| `soteria-serving-cert` | Webhook serving certificate |
| `soteria-metrics-certs` | Metrics endpoint TLS |
| `soteria-apiserver-cert` | Aggregated API server TLS |

### 1.1 — Create the shared Issuer

Create the `soteria-internal` Issuer on **both** clusters. The Issuer
must reference a `Secret` containing the shared CA key pair:

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-internal
  namespace: soteria
spec:
  ca:
    secretName: soteria-internal-ca  # must contain tls.crt + tls.key
```

Apply on both clusters:

```bash
kubectl --context=etl6 -n soteria apply -f issuer.yaml
kubectl --context=etl7 -n soteria apply -f issuer.yaml
```

!!! warning "Same CA key pair"
    The `soteria-internal-ca` Secret **must** contain identical CA
    certificate and key on both clusters. Generate the CA once, then
    distribute the Secret to both clusters. If each cluster has its
    own CA, internode mTLS handshakes will fail.

### 1.2 — API server TLS certificate

The base overlay includes an API server `Certificate`
(`hack/overlays/base/apiserver-cert.yaml`) that is issued by
`soteria-internal`:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-apiserver-cert
  namespace: soteria
spec:
  secretName: soteria-apiserver-tls
  duration: 8760h    # 1 year
  renewBefore: 720h  # 30 days
  issuerRef:
    name: soteria-internal
    kind: Issuer
  commonName: soteria-apiserver
  dnsNames:
    - soteria-apiserver.soteria.svc
    - soteria-apiserver.soteria.svc.cluster.local
  usages:
    - server auth
```

The DNS names are wired automatically by kustomize replacements from the
`soteria-apiserver` Service.

---

## Step 2 — ScyllaDB mTLS configuration

### 2.1 — scylla.yaml ConfigMap

The overlay creates a `ConfigMap` named `scylla-config` that overrides
both server and client encryption options
(`hack/overlays/base/scylladb-tls-config.yaml`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: scylla-config
  namespace: soteria
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

Key details:

- **Internode** (`server_encryption_options`): encrypts all gossip and
  streaming traffic. The truststore points to the cert-manager CA.
- **Client** (`client_encryption_options`): encrypts CQL connections.
  The truststore points to a **combined CA** bundle (see below).

### 2.2 — TLS volumes on ScyllaCluster

The ScyllaCluster TLS patch (`hack/overlays/base/scylladb-tls-patch.yaml`)
adds three volumes and mounts to rack-level configuration:

| Volume | Source | Mount path |
|---|---|---|
| `certmanager-serving` | Secret `scylladb-serving-tls` | `/etc/scylla/certmanager-tls` |
| `certmanager-ca` | Secret `scylladb-serving-tls` (key `ca.crt`) | `/etc/scylla/certmanager-ca` |
| `combined-ca` | ConfigMap `scylladb-combined-ca` | `/etc/scylla/combined-ca` |

!!! note "Operator volume propagation workaround"
    scylla-operator v1.20.2 does **not** propagate rack-level
    `volumes`/`volumeMounts` from the `ScyllaCluster` CR to the
    `StatefulSet`. The deployment script patches the StatefulSet
    directly as a post-deploy step (see
    [Step 5](#step-5-post-deploy-tls-volume-patch)).

### 2.3 — Combined CA ConfigMap

ScyllaDB client connections need to trust both the cert-manager CA and
the scylla-operator's internal client CA. The deployment script builds a
`scylladb-combined-ca` ConfigMap at deploy time by concatenating:

1. `ca.crt` from the `scylladb-serving-tls` Secret (cert-manager CA)
2. `ca-bundle.crt` from the `soteria-scylladb-local-client-ca` ConfigMap
   (operator client CA)

This ConfigMap is created automatically during deployment (see
[Step 4](#step-4-deploy-scylladb)).

---

## Step 3 — Submariner MCS ServiceExport

The base overlay includes a `ServiceExport` resource
(`hack/overlays/base/serviceexport.yaml`) that exports the ScyllaDB
client headless service for cross-cluster discovery:

```yaml
apiVersion: multicluster.x-k8s.io/v1alpha1
kind: ServiceExport
metadata:
  name: soteria-scylladb-client
  namespace: soteria
```

Once applied, Submariner makes the ScyllaDB endpoints available at:

```text
soteria-scylladb-client.soteria.svc.clusterset.local
```

The joining DC (etl7) uses this DNS name as its external seed.

---

## Step 4 — Deploy ScyllaDB

### 4.1 — Kustomize overlay structure

The overlays are organised in three layers:

```text
hack/overlays/
├── base/                    # Shared resources and patches
│   ├── kustomization.yaml   # Imports config/default, adds TLS + MCS
│   ├── serviceexport.yaml   # MCS ServiceExport
│   ├── apiserver-cert.yaml  # API server Certificate
│   ├── apiserver-rbac.yaml  # auth-delegation + admission RBAC
│   ├── scylladb-tls-config.yaml   # scylla.yaml mTLS ConfigMap
│   ├── scylladb-tls-patch.yaml    # ScyllaCluster TLS volumes
│   ├── manager-scylladb-patch.yaml # Manager TLS volume mounts
│   ├── manager-args-patch.yaml     # ScyllaDB + API server args
│   ├── manager-apiserver-patch.yaml # API server port/probes
│   └── storageclass-xfs.yaml       # Lab StorageClass
├── etl6/                    # Seed DC overlay
│   ├── kustomization.yaml   # Patches: DC name = etl6, no externalSeeds
│   ├── scyllacluster-patch.yaml
│   └── manager-dc-patch.yaml       # --scylladb-local-dc=etl6
└── etl7/                    # Joining DC overlay
    ├── kustomization.yaml   # Patches: DC name = etl7, externalSeeds
    ├── scyllacluster-patch.yaml
    └── manager-dc-patch.yaml       # --scylladb-local-dc=etl7
```

The **etl6** ScyllaCluster patch sets the datacenter name and clears
zone affinity:

```yaml
# hack/overlays/etl6/scyllacluster-patch.yaml
- op: replace
  path: /spec/datacenter/name
  value: etl6
- op: replace
  path: /spec/datacenter/racks/0/placement
  value: {}
```

The **etl7** patch adds `externalSeeds` pointing to the Submariner MCS
DNS name:

```yaml
# hack/overlays/etl7/scyllacluster-patch.yaml
- op: replace
  path: /spec/datacenter/name
  value: etl7
- op: add
  path: /spec/externalSeeds
  value:
    - soteria-scylladb-client.soteria.svc.clusterset.local
- op: replace
  path: /spec/datacenter/racks/0/placement
  value: {}
```

### 4.2 — Set the container image

```bash
cd config/manager
kustomize edit set image "controller=<registry>/<image>:<tag>"
```

### 4.3 — Deploy etl6 (seed DC)

Deploy the seed cluster **first**. It has no `externalSeeds` because it
is the first DC in the ScyllaDB cluster:

```bash
kustomize build --load-restrictor LoadRestrictionsNone hack/overlays/etl6 \
  | kubectl --context=etl6 apply --server-side --force-conflicts -f -
```

#### Create the combined CA ConfigMap

After applying the manifests, wait for cert-manager to issue the
`scylladb-serving-tls` Secret and for the scylla-operator to create the
`soteria-scylladb-local-client-ca` ConfigMap, then build the combined CA
bundle:

```bash
# Wait for both secrets to appear (poll up to 5 minutes)
CM_CA=""
OP_CA=""
for i in $(seq 1 60); do
  raw_ca=$(kubectl --context=etl6 -n soteria \
    get secret scylladb-serving-tls \
    -o jsonpath='{.data.ca\.crt}' 2>/dev/null) || true
  if [ -n "${raw_ca}" ]; then
    CM_CA=$(echo "${raw_ca}" | base64 -d)
  fi
  OP_CA=$(kubectl --context=etl6 -n soteria \
    get configmap soteria-scylladb-local-client-ca \
    -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null) || true
  if [ -n "${CM_CA}" ] && [ -n "${OP_CA}" ]; then
    break
  fi
  sleep 5
done

# Create the combined CA ConfigMap
kubectl --context=etl6 -n soteria apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: scylladb-combined-ca
  namespace: soteria
data:
  ca-bundle.crt: |
$(echo "${CM_CA}" | sed 's/^/    /')
$(echo "${OP_CA}" | sed 's/^/    /')
EOF
```

#### Wait for ScyllaDB readiness

```bash
kubectl --context=etl6 -n soteria \
  get scyllaclusters.scylla.scylladb.com soteria-scylladb -w
```

Wait until `.status.racks.rack1.readyMembers` reaches the expected count
(default: **2**).

### 4.4 — Deploy etl7 (joining DC)

Deploy etl7 **only after** etl6 ScyllaDB is fully ready:

```bash
kustomize build --load-restrictor LoadRestrictionsNone hack/overlays/etl7 \
  | kubectl --context=etl7 apply --server-side --force-conflicts -f -
```

Repeat the combined CA creation and readiness wait for etl7:

```bash
# (Same combined CA script as above, but with --context=etl7)
```

---

## Step 5 — Post-deploy TLS volume patch

!!! warning "Operator workaround"
    This step is required because scylla-operator v1.20.2 does not
    propagate rack-level `volumes`/`volumeMounts` to the StatefulSet.
    If a future operator version supports this, skip this step.

For **each** cluster (`etl6` and `etl7`):

### 5.1 — Identify the StatefulSet and scylla container index

```bash
CTX="etl6"  # repeat with etl7

STS=$(kubectl --context="${CTX}" -n soteria \
  get sts -l scylla/cluster=soteria-scylladb -o name | head -1)

SCYLLA_IDX=$(kubectl --context="${CTX}" -n soteria \
  get "${STS}" -o json \
  | jq -r '.spec.template.spec.containers
           | to_entries[] | select(.value.name=="scylla") | .key')
```

### 5.2 — Patch volumes and volume mounts

```bash
# Check if already patched
HAS_CM_VOL=$(kubectl --context="${CTX}" -n soteria \
  get "${STS}" -o json \
  | jq -r '[.spec.template.spec.volumes[].name]
           | if index("certmanager-serving") then "yes" else "no" end')

if [ "${HAS_CM_VOL}" = "no" ]; then
  # Add volumes
  kubectl --context="${CTX}" -n soteria patch "${STS}" --type=json -p "[
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",
     \"value\":{\"name\":\"certmanager-serving\",
                \"secret\":{\"secretName\":\"scylladb-serving-tls\"}}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",
     \"value\":{\"name\":\"certmanager-ca\",
                \"secret\":{\"secretName\":\"scylladb-serving-tls\",
                            \"items\":[{\"key\":\"ca.crt\",
                                        \"path\":\"ca-bundle.crt\"}]}}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",
     \"value\":{\"name\":\"combined-ca\",
                \"configMap\":{\"name\":\"scylladb-combined-ca\"}}}
  ]"

  # Add volume mounts to the scylla container
  kubectl --context="${CTX}" -n soteria patch "${STS}" --type=json -p "[
    {\"op\":\"add\",
     \"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",
     \"value\":{\"name\":\"certmanager-serving\",
                \"mountPath\":\"/etc/scylla/certmanager-tls\",
                \"readOnly\":true}},
    {\"op\":\"add\",
     \"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",
     \"value\":{\"name\":\"certmanager-ca\",
                \"mountPath\":\"/etc/scylla/certmanager-ca\",
                \"readOnly\":true}},
    {\"op\":\"add\",
     \"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",
     \"value\":{\"name\":\"combined-ca\",
                \"mountPath\":\"/etc/scylla/combined-ca\",
                \"readOnly\":true}}
  ]"

  # Restart ScyllaDB pods to pick up the new volumes
  kubectl --context="${CTX}" -n soteria \
    delete pods -l scylla/cluster=soteria-scylladb --wait=false
fi
```

### 5.3 — Wait for pods to restart

```bash
kubectl --context="${CTX}" -n soteria \
  get scyllaclusters.scylla.scylladb.com soteria-scylladb -w
```

Wait until `.status.racks.rack1.readyMembers` returns to the expected
count on both clusters.

---

## Step 6 — Verify multi-DC convergence

### 6.1 — nodetool status

Run `nodetool status` from any ScyllaDB pod to confirm nodes from
**both** datacenters are visible and in **UN** (Up/Normal) state:

```bash
ETL6_POD=$(kubectl --context=etl6 -n soteria \
  get pods -l scylla/cluster=soteria-scylladb \
  -o jsonpath='{.items[0].metadata.name}')

kubectl --context=etl6 -n soteria \
  exec "${ETL6_POD}" -c scylla -- nodetool status
```

Expected output (4 nodes, 2 per DC):

```text
Datacenter: etl6
===============
Status=Up/Down
|/ State=Normal/Leaving/Joining/Moving
--  Address       Load       Tokens  ...  Rack
UN  10.x.x.1     256.0 KB   256     ...  rack1
UN  10.x.x.2     256.0 KB   256     ...  rack1

Datacenter: etl7
===============
Status=Up/Down
|/ State=Normal/Leaving/Joining/Moving
--  Address       Load       Tokens  ...  Rack
UN  10.y.y.1     256.0 KB   256     ...  rack1
UN  10.y.y.2     256.0 KB   256     ...  rack1
```

All nodes should show **UN**. If any show **DN** (Down/Normal) or are
missing, check Submariner connectivity and the `externalSeeds`
configuration.

### 6.2 — CQL connectivity check

Verify CQL connections work with mTLS from a ScyllaDB pod:

```bash
kubectl --context=etl6 -n soteria \
  exec "${ETL6_POD}" -c scylla -- cqlsh \
    --ssl \
    --connect-timeout=10 \
    $(kubectl --context=etl6 -n soteria \
        get pod "${ETL6_POD}" -o jsonpath='{.status.podIP}') \
    9142
```

If the connection succeeds, the CQL shell prompt appears. Type
`DESCRIBE KEYSPACES;` to list available keyspaces, then `EXIT;`.

### 6.3 — Verify the Soteria keyspace

Soteria auto-creates its keyspace with `NetworkTopologyStrategy`
replication. Verify it:

```bash
kubectl --context=etl6 -n soteria \
  exec "${ETL6_POD}" -c scylla -- cqlsh \
    --ssl \
    $(kubectl --context=etl6 -n soteria \
        get pod "${ETL6_POD}" -o jsonpath='{.status.podIP}') \
    9142 \
    -e "DESCRIBE KEYSPACE soteria;"
```

The output should show `NetworkTopologyStrategy` with replication
factors matching the `--scylladb-dc-replication` flag:

```text
CREATE KEYSPACE soteria WITH replication = {
  'class': 'NetworkTopologyStrategy',
  'etl6': '2',
  'etl7': '2'
} AND tablets = {'enabled': false};
```

### 6.4 — Verify Soteria API server

Check that the aggregated API server is registered and available on
both clusters:

```bash
for ctx in etl6 etl7; do
  echo "${ctx}:"
  kubectl --context="${ctx}" get apiservice v1alpha1.soteria.io \
    -o jsonpath='  Available={.status.conditions[?(@.type=="Available")].status}'
  echo
done
```

Expected output:

```text
etl6:
  Available=True
etl7:
  Available=True
```

---

## Soteria manager configuration

The kustomize overlays configure the Soteria controller-manager with
the following ScyllaDB and API server flags:

| Flag | Value |
|---|---|
| `--secure-port` | `6443` |
| `--scylladb-contact-points` | `soteria-scylladb-client.soteria.svc:9142` |
| `--scylladb-keyspace` | `soteria` |
| `--scylladb-dc-replication` | `etl6:2,etl7:2` |
| `--scylladb-tls-cert` | `/etc/soteria/scylladb-client-tls/tls.crt` |
| `--scylladb-tls-key` | `/etc/soteria/scylladb-client-tls/tls.key` |
| `--scylladb-tls-ca` | `/etc/soteria/scylladb-client-tls/ca.crt` |
| `--scylladb-tls-server-name` | `soteria-scylladb-client.soteria.svc` |
| `--tls-cert-file` | `/etc/soteria/apiserver-tls/tls.crt` |
| `--tls-private-key-file` | `/etc/soteria/apiserver-tls/tls.key` |
| `--scylladb-local-dc` | `etl6` or `etl7` (per-cluster) |
| `--site-name` | `etl6` or `etl7` (per-cluster) |

The manager pod mounts two TLS secrets:

- `soteria-apiserver-tls` → `/etc/soteria/apiserver-tls`
- `soteria-scylladb-client-tls` → `/etc/soteria/scylladb-client-tls`

---

## Tear down

To remove the deployment from both clusters:

```bash
./hack/stretched-local-test.sh stop
```

This deletes all kustomize-managed resources from both clusters in
reverse order (etl7 first, then etl6).

---

## Troubleshooting

### ScyllaDB pods stuck in CrashLoopBackOff

Check if the TLS volumes are correctly mounted:

```bash
kubectl --context=etl6 -n soteria describe pod -l scylla/cluster=soteria-scylladb
```

Look for volume mount errors. Common causes:

- `scylladb-serving-tls` Secret not yet issued by cert-manager
- `scylladb-combined-ca` ConfigMap not created
- Issuer `soteria-internal` not configured or CA Secret missing

### etl7 nodes not joining the cluster

1. Verify Submariner MCS is working:

    ```bash
    kubectl --context=etl7 get serviceimports -A
    ```

    You should see `soteria-scylladb-client` imported.

2. Verify DNS resolution from a pod in etl7:

    ```bash
    kubectl --context=etl7 -n soteria run dns-test --rm -it \
      --image=busybox -- nslookup \
      soteria-scylladb-client.soteria.svc.clusterset.local
    ```

3. Check the ScyllaCluster `externalSeeds`:

    ```bash
    kubectl --context=etl7 -n soteria \
      get scyllaclusters.scylla.scylladb.com soteria-scylladb \
      -o jsonpath='{.spec.externalSeeds}'
    ```

### APIService not available

Check cert-manager CA injection:

```bash
kubectl --context=etl6 get apiservice v1alpha1.soteria.io -o yaml \
  | grep cert-manager.io/inject-ca-from
```

The annotation should point to `soteria/soteria-apiserver-cert`. If
the CA is not injected, verify the `soteria-apiserver-cert` Certificate
is in a `Ready` state:

```bash
kubectl --context=etl6 -n soteria get certificate soteria-apiserver-cert
```

### Viewing Soteria logs

```bash
kubectl --context=etl6 -n soteria \
  logs deployment/soteria-controller-manager -c manager -f
kubectl --context=etl7 -n soteria \
  logs deployment/soteria-controller-manager -c manager -f
```
