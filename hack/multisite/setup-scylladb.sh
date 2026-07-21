#!/usr/bin/env bash

# Copyright 2026 The Soteria Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Deploys ScyllaDB as a cross-DC cluster on both Minikube KVM2 clusters
# using Rook-Ceph storage and Cilium Cluster Mesh for inter-node gossip.
#
# Installs cert-manager and scylla-operator via Helm, applies ScyllaCluster
# CRs via Kustomize overlays, configures mTLS via cert-manager + STS
# patching, and verifies multi-DC convergence via nodetool + CQL.
#
# Key design decisions:
#   - Shared CA: east generates the CA key-pair; it is copied to west so both
#     clusters issue certificates from the same root (no cross-CA trust needed).
#   - PodIP broadcast: ScyllaCluster uses broadcastOptions.nodes.type=PodIP so
#     internode addresses are routable via Cilium Cluster Mesh pod-to-pod routing.
#   - Cross-DC seed discovery: west uses externalSeeds DNS to discover east
#     via the soteria-scylladb-client service resolved through Cilium Cluster Mesh.
#
# Prerequisites:
#   - Minikube KVM2 clusters created by setup-clusters.sh (east + west)
#   - Rook-Ceph deployed by setup-rook-ceph.sh (StorageClass rook-ceph-block)
#   - Cilium Cluster Mesh connected between east and west
#   - helm, jq CLIs available
#
# Usage:
#   ./hack/multisite/setup-scylladb.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME     Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME     Name of the west Minikube profile (default: west)
#   NAMESPACE             Namespace for ScyllaDB deployment (default: soteria)
#   CERT_MANAGER_VERSION    cert-manager Helm chart version (default: v1.20.3)
#   SCYLLA_OPERATOR_VERSION scylla-operator Helm chart version (default: v1.21.0)
#   SCYLLA_OPERATOR_NS      scylla-operator namespace (default: scylla-operator)
#   SMOKE_TEST            Set to "0" to skip the convergence smoke test
#   MEMBERS_PER_RACK      ScyllaDB members per rack (default: 1)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"
NAMESPACE="${NAMESPACE:-soteria}"

CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.20.3}"
SCYLLA_OPERATOR_VERSION="${SCYLLA_OPERATOR_VERSION:-v1.21.0}"
SCYLLA_OPERATOR_NS="${SCYLLA_OPERATOR_NS:-scylla-operator}"
SMOKE_TEST="${SMOKE_TEST:-1}"
MEMBERS_PER_RACK="${MEMBERS_PER_RACK:-1}"

OVERLAYS_DIR="${SCRIPT_DIR}/overlays"
MANIFESTS_DIR="${SCRIPT_DIR}/manifests"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
info "Checking prerequisites..."

command -v helm >/dev/null 2>&1 || fatal "helm CLI not found"
command -v kubectl >/dev/null 2>&1 || fatal "kubectl CLI not found"
command -v jq >/dev/null 2>&1 || fatal "jq CLI not found (required for STS TLS patching)"

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  kubectl --context "${ctx}" cluster-info >/dev/null 2>&1 \
    || fatal "Cannot reach cluster '${ctx}' — is minikube running?"
done

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  kubectl --context "${ctx}" get sc rook-ceph-block >/dev/null 2>&1 \
    || fatal "StorageClass 'rook-ceph-block' not found on '${ctx}' — run setup-rook-ceph.sh first"
done

if command -v cilium >/dev/null 2>&1; then
  cilium clustermesh status --context "${EAST_CONTEXT}" --wait 2>/dev/null \
    || warn "Cilium Cluster Mesh not fully connected on '${EAST_CONTEXT}' — cross-DC gossip may fail"
else
  warn "cilium CLI not found — skipping Cluster Mesh connectivity check"
fi

info "Prerequisites OK"

# ---------------------------------------------------------------------------
# Task 0: Create XFS-formatted StorageClass for ScyllaDB
# ---------------------------------------------------------------------------
info "Creating rook-ceph-block-xfs StorageClass on both clusters..."

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  if kubectl --context "${ctx}" get sc rook-ceph-block-xfs >/dev/null 2>&1; then
    info "  ${ctx}: rook-ceph-block-xfs already exists"
  else
    kubectl --context "${ctx}" apply -f "${MANIFESTS_DIR}/storage-class-xfs.yaml"
    info "  ${ctx}: rook-ceph-block-xfs created"
  fi
done

# ---------------------------------------------------------------------------
# Task 1: Deploy cert-manager on both clusters
# ---------------------------------------------------------------------------
deploy_cert_manager() {
  local ctx="$1"
  info "  ${ctx}: installing cert-manager ${CERT_MANAGER_VERSION}..."

  helm upgrade --install cert-manager \
    oci://quay.io/jetstack/charts/cert-manager \
    --version "${CERT_MANAGER_VERSION}" \
    --namespace cert-manager \
    --create-namespace \
    --set crds.enabled=true \
    --kube-context "${ctx}" \
    --wait --timeout 5m

  info "  ${ctx}: waiting for cert-manager pods..."
  for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
    kubectl --context "${ctx}" -n cert-manager \
      rollout status deployment/"${deploy}" --timeout=3m
  done
  info "  ${ctx}: cert-manager ready"
}

info "Deploying cert-manager on both clusters..."
deploy_cert_manager "${EAST_CONTEXT}"
deploy_cert_manager "${WEST_CONTEXT}"

# ---------------------------------------------------------------------------
# Task 1.3: Create shared CA across clusters
# ---------------------------------------------------------------------------
# Cross-DC mTLS requires both clusters to trust each other's certificates.
# We generate the CA on east, then copy the key-pair to west so both Issuers
# sign with the same root. This avoids maintaining a cross-CA trust bundle.
# ---------------------------------------------------------------------------
create_east_ca() {
  local ctx="${EAST_CONTEXT}"
  info "  ${ctx}: creating CA issuers and ${NAMESPACE} namespace..."

  kubectl --context "${ctx}" create namespace "${NAMESPACE}" --dry-run=client -o yaml \
    | kubectl --context "${ctx}" apply -f -

  kubectl --context "${ctx}" apply -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: soteria-selfsigned
spec:
  selfSigned: {}
EOF

  kubectl --context "${ctx}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-ca
  namespace: ${NAMESPACE}
spec:
  isCA: true
  commonName: soteria-ca
  secretName: soteria-ca-key-pair
  issuerRef:
    name: soteria-selfsigned
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-internal
  namespace: ${NAMESPACE}
spec:
  ca:
    secretName: soteria-ca-key-pair
EOF

  info "  ${ctx}: waiting for CA certificate to be ready..."
  local ready=""
  for _ in $(seq 1 60); do
    ready=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
      get certificate soteria-ca -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [[ "${ready}" == "True" ]]; then
      break
    fi
    sleep 2
  done
  if [[ "${ready}" != "True" ]]; then
    fatal "  ${ctx}: CA certificate 'soteria-ca' did not become ready within 2 minutes"
  fi
  info "  ${ctx}: CA issuers ready"
}

copy_ca_to_west() {
  local src="${EAST_CONTEXT}" dst="${WEST_CONTEXT}"
  info "  ${dst}: copying shared CA key-pair from ${src}..."

  kubectl --context "${dst}" create namespace "${NAMESPACE}" --dry-run=client -o yaml \
    | kubectl --context "${dst}" apply -f -

  kubectl --context "${src}" -n "${NAMESPACE}" get secret soteria-ca-key-pair -o json \
    | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp,
              .metadata.managedFields, .metadata.annotations["cert-manager.io/certificate-name"])' \
    | kubectl --context "${dst}" -n "${NAMESPACE}" apply --server-side --force-conflicts -f -

  kubectl --context "${dst}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-internal
  namespace: ${NAMESPACE}
spec:
  ca:
    secretName: soteria-ca-key-pair
EOF

  info "  ${dst}: waiting for Issuer to become ready..."
  local ready=""
  for _ in $(seq 1 30); do
    ready=$(kubectl --context "${dst}" -n "${NAMESPACE}" \
      get issuer soteria-internal -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [[ "${ready}" == "True" ]]; then
      break
    fi
    sleep 2
  done
  if [[ "${ready}" != "True" ]]; then
    fatal "  ${dst}: Issuer 'soteria-internal' did not become ready"
  fi
  info "  ${dst}: shared CA configured"
}

info "Creating shared CA (east generates, west receives copy)..."
create_east_ca
copy_ca_to_west

# ---------------------------------------------------------------------------
# Create ScyllaDB serving certificate (issued by soteria-internal)
# ---------------------------------------------------------------------------
create_scylladb_tls_cert() {
  local ctx="$1"
  info "  ${ctx}: creating ScyllaDB serving certificate..."

  kubectl --context "${ctx}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-scylladb-serving
  namespace: ${NAMESPACE}
spec:
  secretName: scylladb-serving-tls
  issuerRef:
    name: soteria-internal
    kind: Issuer
  dnsNames:
    - soteria-scylladb-client.${NAMESPACE}.svc.cluster.local
    - soteria-scylladb-client.${NAMESPACE}.svc
    - "*.soteria-scylladb-client.${NAMESPACE}.svc.cluster.local"
    - "*.${NAMESPACE}.svc.cluster.local"
  usages:
    - server auth
    - client auth
EOF
  info "  ${ctx}: ScyllaDB TLS certificate created"
}

create_scylladb_tls_cert "${EAST_CONTEXT}"
create_scylladb_tls_cert "${WEST_CONTEXT}"

# ---------------------------------------------------------------------------
# Task 2: Deploy scylla-operator on both clusters
# ---------------------------------------------------------------------------
deploy_scylla_operator() {
  local ctx="$1"
  info "  ${ctx}: installing scylla-operator..."

  helm repo add scylla https://scylla-operator-charts.storage.googleapis.com/stable 2>/dev/null || true
  helm repo update scylla 2>/dev/null || true

  local scylla_version_args=()
  if [[ -n "${SCYLLA_OPERATOR_VERSION}" ]]; then
    scylla_version_args=(--version "${SCYLLA_OPERATOR_VERSION}")
  fi

  helm upgrade --install scylla-operator scylla/scylla-operator \
    --create-namespace \
    --namespace "${SCYLLA_OPERATOR_NS}" \
    --kube-context "${ctx}" \
    "${scylla_version_args[@]+"${scylla_version_args[@]}"}" \
    --wait --timeout 5m

  info "  ${ctx}: waiting for scylla-operator deployments..."
  kubectl --context "${ctx}" -n "${SCYLLA_OPERATOR_NS}" \
    rollout status deployment/scylla-operator --timeout=5m
  kubectl --context "${ctx}" -n "${SCYLLA_OPERATOR_NS}" \
    rollout status deployment/webhook-server --timeout=5m

  kubectl --context "${ctx}" get crd scyllaclusters.scylla.scylladb.com >/dev/null 2>&1 \
    || fatal "  ${ctx}: ScyllaCluster CRD not found after operator install"

  info "  ${ctx}: scylla-operator ready"
}

info "Deploying scylla-operator on both clusters..."
deploy_scylla_operator "${EAST_CONTEXT}"
deploy_scylla_operator "${WEST_CONTEXT}"

# ---------------------------------------------------------------------------
# Helper: wait for ScyllaDB to become ready
# ---------------------------------------------------------------------------
wait_scylladb_ready() {
  local ctx="$1"
  info "Waiting for ScyllaDB to become ready on ${ctx} (${MEMBERS_PER_RACK} member(s))..."
  for i in $(seq 1 180); do
    status=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
      get scyllaclusters.scylla.scylladb.com soteria-scylladb \
      -o jsonpath='{.status.racks.rack1.readyMembers}' 2>/dev/null || echo "0")
    if [[ "${status}" -ge "${MEMBERS_PER_RACK}" ]]; then
      info "  ScyllaDB on ${ctx}: ${status} member(s) ready"
      return 0
    fi
    if [[ ${i} -eq 180 ]]; then
      error "ScyllaDB on ${ctx} did not become ready in 15 minutes"
      kubectl --context "${ctx}" -n "${NAMESPACE}" \
        describe scyllaclusters.scylla.scylladb.com soteria-scylladb || true
      return 1
    fi
    sleep 5
  done
}

# ---------------------------------------------------------------------------
# Helper: wait for cert-manager CA secret to be available
# ---------------------------------------------------------------------------
wait_for_ca_secret() {
  local ctx="$1"
  info "  ${ctx}: waiting for cert-manager CA secret (scylladb-serving-tls)..."
  for _ in $(seq 1 60); do
    local raw_ca
    raw_ca=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
      get secret scylladb-serving-tls -o jsonpath='{.data.ca\.crt}' 2>/dev/null) || true
    if [[ -n "${raw_ca}" ]]; then
      info "  ${ctx}: cert-manager CA secret available"
      return 0
    fi
    sleep 5
  done
  warn "  ${ctx}: cert-manager CA secret not found after 5 minutes"
  return 1
}

# ---------------------------------------------------------------------------
# Helper: patch ScyllaDB StatefulSet with TLS volumes
# ---------------------------------------------------------------------------
patch_scylladb_sts() {
  local ctx="$1"
  info "  ${ctx}: patching ScyllaDB STS with TLS volumes..."

  local STS
  STS=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
    get sts -l scylla/cluster=soteria-scylladb -o name 2>/dev/null | head -1)

  if [[ -z "${STS}" ]]; then
    warn "  ${ctx}: no ScyllaDB StatefulSet found"
    return 1
  fi

  local SCYLLA_IDX
  SCYLLA_IDX=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
    get "${STS}" -o json 2>/dev/null \
    | jq -r '.spec.template.spec.containers | to_entries[] | select(.value.name=="scylla") | .key')

  if [[ -z "${SCYLLA_IDX}" ]]; then
    fatal "  ${ctx}: could not find 'scylla' container in STS ${STS}"
  fi

  local STS_JSON
  STS_JSON=$(kubectl --context "${ctx}" -n "${NAMESPACE}" get "${STS}" -o json 2>/dev/null)

  local HAS_ALL_VOLS
  HAS_ALL_VOLS=$(echo "${STS_JSON}" \
    | jq -r '[.spec.template.spec.volumes[].name] | if (index("certmanager-serving") and index("certmanager-ca") and index("combined-ca")) then "yes" else "no" end')

  local HAS_ALL_MOUNTS
  HAS_ALL_MOUNTS=$(echo "${STS_JSON}" \
    | jq -r --argjson idx "${SCYLLA_IDX}" '[.spec.template.spec.containers[$idx].volumeMounts[].name] | if (index("certmanager-serving") and index("certmanager-ca") and index("combined-ca")) then "yes" else "no" end')

  if [[ "${HAS_ALL_VOLS}" == "yes" && "${HAS_ALL_MOUNTS}" == "yes" ]]; then
    info "  ${ctx}: TLS volumes and mounts already present in STS"
    return 0
  fi

  # Remove any partial leftovers before re-applying
  if [[ "${HAS_ALL_VOLS}" == "yes" && "${HAS_ALL_MOUNTS}" != "yes" ]]; then
    info "  ${ctx}: partial TLS patch detected — removing stale volumes before reapplying"
    kubectl --context "${ctx}" -n "${NAMESPACE}" patch "${STS}" --type=json -p "[
      {\"op\":\"remove\",\"path\":\"/spec/template/spec/volumes/$(echo "${STS_JSON}" | jq -r '[.spec.template.spec.volumes[].name] | to_entries[] | select(.value=="certmanager-serving") | .key')\"},
      {\"op\":\"remove\",\"path\":\"/spec/template/spec/volumes/$(echo "${STS_JSON}" | jq -r '[.spec.template.spec.volumes[].name] | to_entries[] | select(.value=="certmanager-ca") | .key')\"},
      {\"op\":\"remove\",\"path\":\"/spec/template/spec/volumes/$(echo "${STS_JSON}" | jq -r '[.spec.template.spec.volumes[].name] | to_entries[] | select(.value=="combined-ca") | .key')\"}
    ]" 2>/dev/null || true
  fi

  kubectl --context "${ctx}" -n "${NAMESPACE}" patch "${STS}" --type=json -p "[
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"certmanager-serving\",\"secret\":{\"secretName\":\"scylladb-serving-tls\"}}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"certmanager-ca\",\"secret\":{\"secretName\":\"scylladb-serving-tls\",\"items\":[{\"key\":\"ca.crt\",\"path\":\"ca-bundle.crt\"}]}}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"combined-ca\",\"secret\":{\"secretName\":\"scylladb-serving-tls\",\"items\":[{\"key\":\"ca.crt\",\"path\":\"ca-bundle.crt\"}]}}}
  ]"
  kubectl --context "${ctx}" -n "${NAMESPACE}" patch "${STS}" --type=json -p "[
    {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"certmanager-serving\",\"mountPath\":\"/etc/scylla/certmanager-tls\",\"readOnly\":true}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"certmanager-ca\",\"mountPath\":\"/etc/scylla/certmanager-ca\",\"readOnly\":true}},
    {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"combined-ca\",\"mountPath\":\"/etc/scylla/combined-ca\",\"readOnly\":true}}
  ]"

  info "  ${ctx}: restarting ScyllaDB pods to apply TLS volumes..."
  kubectl --context "${ctx}" -n "${NAMESPACE}" delete pods \
    -l scylla/cluster=soteria-scylladb --wait=false
  info "  ${ctx}: STS patched"
}

# ---------------------------------------------------------------------------
# Task 4.6: Deploy east (seed cluster)
# ---------------------------------------------------------------------------
echo ""
info "=== Deploying ScyllaDB on east (seed DC) ==="
kubectl --context "${EAST_CONTEXT}" apply --server-side --force-conflicts \
  -k "${OVERLAYS_DIR}/east"

# ---------------------------------------------------------------------------
# Task 4.8-4.9: CA secret + wait for east readiness
# ---------------------------------------------------------------------------
wait_for_ca_secret "${EAST_CONTEXT}"
wait_scylladb_ready "${EAST_CONTEXT}" || fatal "East ScyllaDB did not become ready"

# ---------------------------------------------------------------------------
# Task 5: Patch east STS with TLS volumes + restart
# ---------------------------------------------------------------------------
echo ""
info "=== Patching east ScyllaDB STS with TLS volumes ==="
patch_scylladb_sts "${EAST_CONTEXT}"
wait_scylladb_ready "${EAST_CONTEXT}" || fatal "East ScyllaDB did not recover after TLS patch"

# ---------------------------------------------------------------------------
# Task 4.10: Deploy west (joining cluster)
# ---------------------------------------------------------------------------
echo ""
info "=== Deploying ScyllaDB on west (joining DC) ==="
kubectl --context "${WEST_CONTEXT}" apply --server-side --force-conflicts \
  -k "${OVERLAYS_DIR}/west"

# ---------------------------------------------------------------------------
# Task 4.12: CA secret + wait for west readiness
# ---------------------------------------------------------------------------
wait_for_ca_secret "${WEST_CONTEXT}"
wait_scylladb_ready "${WEST_CONTEXT}" || fatal "West ScyllaDB did not become ready"

# ---------------------------------------------------------------------------
# Task 5: Patch west STS with TLS volumes + restart
# ---------------------------------------------------------------------------
echo ""
info "=== Patching west ScyllaDB STS with TLS volumes ==="
patch_scylladb_sts "${WEST_CONTEXT}"
wait_scylladb_ready "${WEST_CONTEXT}" || fatal "West ScyllaDB did not recover after TLS patch"

# ---------------------------------------------------------------------------
# Task 5b: Patch east with west external seed for symmetric reconnection
# ---------------------------------------------------------------------------
# East bootstrapped without externalSeeds (standalone seed DC). Now that west
# has joined, add the west seed so east can rediscover west after partitions.
echo ""
info "=== Adding west seed to east for symmetric reconnection ==="
keast -n "${NAMESPACE}" patch scyllaclusters.scylla.scylladb.com soteria-scylladb \
  --type=merge -p "{\"spec\":{\"externalSeeds\":[\"soteria-scylladb-west-rack1-0.soteria.svc.clusterset.local\"]}}"
info "East ScyllaDB patched with west external seed"
wait_scylladb_ready "${EAST_CONTEXT}" || fatal "East ScyllaDB did not recover after symmetric seed patch"

# ---------------------------------------------------------------------------
# Task 6: Multi-DC convergence smoke test
# ---------------------------------------------------------------------------
if [[ "${SMOKE_TEST}" == "0" ]]; then
  info "Skipping multi-DC convergence smoke test (SMOKE_TEST=0)"
else
  echo ""
  info "=== Multi-DC convergence smoke test ==="

  # 6.1: Wait for nodetool to show all nodes UN across both DCs
  EAST_POD=$(keast -n "${NAMESPACE}" \
    get pods -l "scylla/cluster=soteria-scylladb" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

  EXPECTED_NODES=$(( MEMBERS_PER_RACK * 2 ))

  if [[ -n "${EAST_POD}" ]]; then
    info "Waiting for multi-DC cluster convergence (${EXPECTED_NODES} UN nodes)..."
    CONVERGED=false
    for i in $(seq 1 120); do
      node_count=$(keast -n "${NAMESPACE}" \
        exec "${EAST_POD}" -c scylla -- nodetool status 2>/dev/null \
        | grep -c "^UN" || true)
      node_count="${node_count:-0}"
      if [[ "${node_count}" -ge "${EXPECTED_NODES}" ]]; then
        info "Cluster converged — ${node_count} UN nodes across DCs"
        echo ""
        keast -n "${NAMESPACE}" \
          exec "${EAST_POD}" -c scylla -- nodetool status 2>/dev/null || true
        echo ""
        CONVERGED=true
        break
      fi
      if (( i % 12 == 0 )); then
        info "  Attempt ${i}/120: ${node_count} UN node(s) so far (waiting for ${EXPECTED_NODES})..."
      fi
      sleep 5
    done
    if [[ "${CONVERGED}" != "true" ]]; then
      error "Final nodetool status output:"
      keast -n "${NAMESPACE}" \
        exec "${EAST_POD}" -c scylla -- nodetool status 2>/dev/null || true
      fatal "Multi-DC convergence failed — only ${node_count} UN nodes detected (expected >=${EXPECTED_NODES})"
    fi

    # 6.2: CQL cross-DC replication test
    info "Running CQL cross-DC replication test..."
    EAST_IP=$(keast -n "${NAMESPACE}" get pod "${EAST_POD}" -o jsonpath='{.status.podIP}')
    CQL_WRITE_OK=false
    cql_err=""
    for ci in $(seq 1 24); do
      cql_err=$(keast -n "${NAMESPACE}" exec "${EAST_POD}" -c scylla -- \
        cqlsh "${EAST_IP}" -e "CREATE KEYSPACE IF NOT EXISTS smoke_test WITH replication = {'class': 'NetworkTopologyStrategy', 'east': 1, 'west': 1};" \
        2>&1) \
      && cql_err=$(keast -n "${NAMESPACE}" exec "${EAST_POD}" -c scylla -- \
        cqlsh "${EAST_IP}" -e "CREATE TABLE IF NOT EXISTS smoke_test.test_table (id int PRIMARY KEY, value text);" \
        2>&1) \
      && cql_err=$(keast -n "${NAMESPACE}" exec "${EAST_POD}" -c scylla -- \
        cqlsh "${EAST_IP}" -e "INSERT INTO smoke_test.test_table (id, value) VALUES (1, 'cross-dc-test');" \
        2>&1) \
      && { CQL_WRITE_OK=true; break; }
      if [[ $((ci % 4)) -eq 0 ]]; then
        info "  Attempt ${ci}/24: CQL write not ready yet, retrying..."
      fi
      sleep 5
    done
    if [[ "${CQL_WRITE_OK}" != "true" ]]; then
      error "Last CQL error: ${cql_err}"
      fatal "CQL write on east failed after retries"
    fi

    sleep 5

    WEST_POD=$(kwest -n "${NAMESPACE}" \
      get pods -l "scylla/cluster=soteria-scylladb" \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

    if [[ -n "${WEST_POD}" ]]; then
      WEST_IP=$(kwest -n "${NAMESPACE}" get pod "${WEST_POD}" -o jsonpath='{.status.podIP}')
      RESULT=""
      for _ in $(seq 1 12); do
        RESULT=$(kwest -n "${NAMESPACE}" exec "${WEST_POD}" -c scylla -- \
          cqlsh "${WEST_IP}" -e "SELECT value FROM smoke_test.test_table WHERE id = 1;" \
          2>&1 || true)
        if echo "${RESULT}" | grep -q "cross-dc-test"; then
          info "CQL cross-DC read successful — data replicated from east to west"
          break
        fi
        sleep 5
      done
      if ! echo "${RESULT}" | grep -q "cross-dc-test"; then
        fatal "CQL cross-DC read did not return expected value — replication verification failed"
      fi
    else
      fatal "No ScyllaDB pod found on west — cannot verify cross-DC replication"
    fi

    # 6.3: Clean up test keyspace
    EAST_IP=$(keast -n "${NAMESPACE}" get pod "${EAST_POD}" -o jsonpath='{.status.podIP}')
    keast -n "${NAMESPACE}" exec "${EAST_POD}" -c scylla -- \
      cqlsh "${EAST_IP}" -e "DROP KEYSPACE IF EXISTS smoke_test;" 2>/dev/null || true
    info "Smoke test keyspace cleaned up"
  else
    fatal "No ScyllaDB pod found on east — cannot run convergence smoke test"
  fi
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
info "=========================================="
info "ScyllaDB cross-DC deployment complete!"
info "=========================================="
info ""
info "  East DC: kubectl --context ${EAST_CONTEXT} -n ${NAMESPACE} get scyllaclusters"
info "  West DC: kubectl --context ${WEST_CONTEXT} -n ${NAMESPACE} get scyllaclusters"
info ""
info "  nodetool: kubectl --context ${EAST_CONTEXT} -n ${NAMESPACE} exec -it <pod> -c scylla -- nodetool status"
info "  cqlsh:    kubectl --context ${EAST_CONTEXT} -n ${NAMESPACE} exec -it <pod> -c scylla -- cqlsh"
