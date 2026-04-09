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

# Stretched-cluster validation test.
#
# Simulates a two-site DR topology on the local machine:
#
#   etl6 cluster ←→ ScyllaDB DC "etl6" ←→ Soteria :6443
#   etl7 cluster ←→ ScyllaDB DC "etl7" ←→ Soteria :6444
#
# The two ScyllaDB nodes form a single cluster using
# NetworkTopologyStrategy (1 replica per DC) and eventual consistency
# (LocalOne reads/writes). Data written through one Soteria instance
# asynchronously replicates to the other DC.
#
# Prerequisites:
#   - podman (or docker)
#   - go 1.25+
#   - kubectl
#   - curl, openssl (for health checks / CA extraction)
#   - Kubeconfig with contexts "etl6" and "etl7"
#
# Port allocation (override via env vars):
#   ScyllaDB etl6 CQL : 9042  (SCYLLA_ETL6_PORT)
#   ScyllaDB etl7 CQL : 9043  (SCYLLA_ETL7_PORT)
#   Soteria  etl6 API : 6443  (SOTERIA_ETL6_PORT)
#   Soteria  etl7 API : 6444  (SOTERIA_ETL7_PORT)
#   Health   etl6     : 8081  (HEALTH_ETL6_PORT)
#   Health   etl7     : 8082  (HEALTH_ETL7_PORT)
#
# Usage:
#   ./hack/stretched-local-test.sh          # start everything
#   ./hack/stretched-local-test.sh stop     # tear down

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${SCRIPT_ROOT}"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
NETWORK_NAME="soteria-stretched"
CONTAINER_IMAGE="docker.io/scylladb/scylla:latest"
KEYSPACE="${KEYSPACE:-soteria}"

SCYLLA_ETL6_NAME="scylladb-soteria-etl6"
SCYLLA_ETL7_NAME="scylladb-soteria-etl7"
SCYLLA_ETL6_PORT="${SCYLLA_ETL6_PORT:-9042}"
SCYLLA_ETL7_PORT="${SCYLLA_ETL7_PORT:-9043}"

SOTERIA_ETL6_PORT="${SOTERIA_ETL6_PORT:-6443}"
SOTERIA_ETL7_PORT="${SOTERIA_ETL7_PORT:-6444}"

HEALTH_ETL6_PORT="${HEALTH_ETL6_PORT:-8081}"
HEALTH_ETL7_PORT="${HEALTH_ETL7_PORT:-8082}"

KUBECONFIG_FILE="${KUBECONFIG:-${HOME}/.kube/config}"
CTX_ETL6="etl6"
CTX_ETL7="etl7"

SOTERIA_NS="soteria-system"

LOG_DIR="/tmp/soteria-stretched"
mkdir -p "${LOG_DIR}"

# ---------------------------------------------------------------------------
# Container runtime detection
# ---------------------------------------------------------------------------
if command -v podman &>/dev/null; then
  CTR=podman
elif command -v docker &>/dev/null; then
  CTR=docker
else
  echo "Error: neither podman nor docker found" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
stop() {
  echo ""
  echo "=== Tearing down stretched-cluster test environment ==="

  for pidfile in "${LOG_DIR}/soteria-etl6.pid" "${LOG_DIR}/soteria-etl7.pid"; do
    if [[ -f "${pidfile}" ]]; then
      pid=$(cat "${pidfile}")
      if kill -0 "${pid}" 2>/dev/null; then
        echo "Stopping Soteria (PID ${pid})..."
        kill "${pid}" 2>/dev/null || true
        wait "${pid}" 2>/dev/null || true
      fi
      rm -f "${pidfile}"
    fi
  done

  for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
    echo "Removing APIService from context ${ctx}..."
    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" \
      delete apiservice v1alpha1.soteria.io --ignore-not-found 2>/dev/null || true
    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" \
      delete namespace "${SOTERIA_NS}" --ignore-not-found 2>/dev/null || true
  done

  echo "Removing ScyllaDB containers..."
  ${CTR} rm -f "${SCYLLA_ETL6_NAME}" 2>/dev/null || true
  ${CTR} rm -f "${SCYLLA_ETL7_NAME}" 2>/dev/null || true

  echo "Removing container network..."
  ${CTR} network rm "${NETWORK_NAME}" 2>/dev/null || true

  rm -f "${LOG_DIR}/kubeconfig-etl6.yaml" "${LOG_DIR}/kubeconfig-etl7.yaml"

  echo "Done."
}

if [[ "${1:-}" == "stop" ]]; then
  stop
  exit 0
fi

trap 'echo; echo "Shutting down..."; stop' EXIT INT TERM

# ---------------------------------------------------------------------------
# Container network (enables DNS-based inter-node discovery)
# ---------------------------------------------------------------------------
if ! ${CTR} network inspect "${NETWORK_NAME}" &>/dev/null; then
  echo "Creating container network ${NETWORK_NAME}..."
  ${CTR} network create "${NETWORK_NAME}"
fi

# ---------------------------------------------------------------------------
# ScyllaDB node 1 — DC etl6 (seed)
# ---------------------------------------------------------------------------
if ${CTR} inspect "${SCYLLA_ETL6_NAME}" &>/dev/null; then
  echo "ScyllaDB node ${SCYLLA_ETL6_NAME} already exists, reusing."
  ${CTR} start "${SCYLLA_ETL6_NAME}" 2>/dev/null || true
else
  echo "Starting ScyllaDB seed node (DC=etl6) on CQL port ${SCYLLA_ETL6_PORT}..."
  ${CTR} run -d \
    --name "${SCYLLA_ETL6_NAME}" \
    --network "${NETWORK_NAME}" \
    -p "${SCYLLA_ETL6_PORT}:9042" \
    "${CONTAINER_IMAGE}" \
    --smp 1 --memory 512M --overprovisioned 1 \
    --seeds "${SCYLLA_ETL6_NAME}" \
    --dc etl6 --rack rack1
fi

echo "Waiting for ScyllaDB seed (etl6) to accept CQL connections..."
for i in $(seq 1 90); do
  if ${CTR} exec "${SCYLLA_ETL6_NAME}" cqlsh -e "SELECT now() FROM system.local" &>/dev/null; then
    echo "ScyllaDB seed (etl6) is ready."
    break
  fi
  if [[ ${i} -eq 90 ]]; then
    echo "Error: ScyllaDB seed (etl6) did not become ready in time" >&2
    exit 1
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# ScyllaDB node 2 — DC etl7 (joins seed via gossip)
# ---------------------------------------------------------------------------
if ${CTR} inspect "${SCYLLA_ETL7_NAME}" &>/dev/null; then
  echo "ScyllaDB node ${SCYLLA_ETL7_NAME} already exists, reusing."
  ${CTR} start "${SCYLLA_ETL7_NAME}" 2>/dev/null || true
else
  echo "Starting ScyllaDB node (DC=etl7) on CQL port ${SCYLLA_ETL7_PORT}..."
  ${CTR} run -d \
    --name "${SCYLLA_ETL7_NAME}" \
    --network "${NETWORK_NAME}" \
    -p "${SCYLLA_ETL7_PORT}:9042" \
    "${CONTAINER_IMAGE}" \
    --smp 1 --memory 512M --overprovisioned 1 \
    --seeds "${SCYLLA_ETL6_NAME}" \
    --dc etl7 --rack rack1
fi

echo "Waiting for ScyllaDB node (etl7) to accept CQL connections..."
for i in $(seq 1 90); do
  if ${CTR} exec "${SCYLLA_ETL7_NAME}" cqlsh -e "SELECT now() FROM system.local" &>/dev/null; then
    echo "ScyllaDB node (etl7) is ready."
    break
  fi
  if [[ ${i} -eq 90 ]]; then
    echo "Error: ScyllaDB node (etl7) did not become ready in time" >&2
    exit 1
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# Wait for two-node cluster convergence
# ---------------------------------------------------------------------------
echo "Waiting for ScyllaDB cluster to converge (both DCs visible)..."
for i in $(seq 1 60); do
  node_count=$(${CTR} exec "${SCYLLA_ETL6_NAME}" nodetool status 2>/dev/null \
    | grep -c "^UN" || true)
  if [[ "${node_count}" -ge 2 ]]; then
    echo "ScyllaDB cluster converged — ${node_count} nodes UN (Up/Normal)."
    echo ""
    ${CTR} exec "${SCYLLA_ETL6_NAME}" nodetool status
    echo ""
    break
  fi
  if [[ ${i} -eq 60 ]]; then
    echo "Error: cluster did not converge (only ${node_count} UN nodes)" >&2
    ${CTR} exec "${SCYLLA_ETL6_NAME}" nodetool status || true
    exit 1
  fi
  sleep 3
done

# ---------------------------------------------------------------------------
# Create keyspace (NTS) + tables
#
# NetworkTopologyStrategy with RF=1 per DC gives eventual consistency:
# writes at LocalOne succeed on the local replica and asynchronously
# replicate to the remote DC. Reads at LocalOne serve from local data.
# ---------------------------------------------------------------------------
echo "Creating keyspace '${KEYSPACE}' with NetworkTopologyStrategy (etl6:1, etl7:1)..."
${CTR} exec "${SCYLLA_ETL6_NAME}" cqlsh -e \
  "CREATE KEYSPACE IF NOT EXISTS ${KEYSPACE}
   WITH replication = {
     'class': 'NetworkTopologyStrategy',
     'etl6': 1,
     'etl7': 1
   };"

echo "Creating kv_store table with CDC..."
${CTR} exec "${SCYLLA_ETL6_NAME}" cqlsh -e \
  "CREATE TABLE IF NOT EXISTS ${KEYSPACE}.kv_store (
     api_group text,
     resource_type text,
     namespace text,
     name text,
     value blob,
     resource_version timeuuid,
     PRIMARY KEY ((api_group, resource_type), namespace, name)
   ) WITH cdc = {'enabled': true};"

echo "Creating kv_store_labels index table..."
${CTR} exec "${SCYLLA_ETL6_NAME}" cqlsh -e \
  "CREATE TABLE IF NOT EXISTS ${KEYSPACE}.kv_store_labels (
     api_group text,
     resource_type text,
     label_key text,
     label_value text,
     namespace text,
     name text,
     PRIMARY KEY ((api_group, resource_type, label_key), label_value, namespace, name)
   );"

echo "Schema ready."

# ---------------------------------------------------------------------------
# Build the Soteria binary once (avoids double-compile from two go-run calls)
# ---------------------------------------------------------------------------
echo ""
echo "Building Soteria binary..."
go build -o "${SCRIPT_ROOT}/bin/soteria-stretched-test" ./cmd/soteria/

# ---------------------------------------------------------------------------
# Extract per-context kubeconfigs (each Soteria instance delegates
# authn/authz to its own cluster's kube-apiserver)
# ---------------------------------------------------------------------------
echo "Extracting per-context kubeconfigs..."
kubectl --kubeconfig="${KUBECONFIG_FILE}" config view \
  --context="${CTX_ETL6}" --minify --flatten > "${LOG_DIR}/kubeconfig-etl6.yaml"
kubectl --kubeconfig="${KUBECONFIG_FILE}" config view \
  --context="${CTX_ETL7}" --minify --flatten > "${LOG_DIR}/kubeconfig-etl7.yaml"

# ---------------------------------------------------------------------------
# Start Soteria — etl6
# ---------------------------------------------------------------------------
echo ""
echo "Starting Soteria (etl6) — API :${SOTERIA_ETL6_PORT}, health :${HEALTH_ETL6_PORT}..."
KUBECONFIG="${LOG_DIR}/kubeconfig-etl6.yaml" \
  "${SCRIPT_ROOT}/bin/soteria-stretched-test" \
    --secure-port="${SOTERIA_ETL6_PORT}" \
    --kubeconfig="${LOG_DIR}/kubeconfig-etl6.yaml" \
    --authentication-kubeconfig="${LOG_DIR}/kubeconfig-etl6.yaml" \
    --authorization-kubeconfig="${LOG_DIR}/kubeconfig-etl6.yaml" \
    --authentication-skip-lookup \
    --health-probe-bind-address=":${HEALTH_ETL6_PORT}" \
    --metrics-bind-address="0" \
    --scylladb-contact-points="localhost:${SCYLLA_ETL6_PORT}" \
    --scylladb-keyspace="${KEYSPACE}" \
    --scylladb-local-dc="etl6" \
    >"${LOG_DIR}/soteria-etl6.log" 2>&1 &
echo $! > "${LOG_DIR}/soteria-etl6.pid"

# ---------------------------------------------------------------------------
# Start Soteria — etl7
# ---------------------------------------------------------------------------
echo "Starting Soteria (etl7) — API :${SOTERIA_ETL7_PORT}, health :${HEALTH_ETL7_PORT}..."
KUBECONFIG="${LOG_DIR}/kubeconfig-etl7.yaml" \
  "${SCRIPT_ROOT}/bin/soteria-stretched-test" \
    --secure-port="${SOTERIA_ETL7_PORT}" \
    --kubeconfig="${LOG_DIR}/kubeconfig-etl7.yaml" \
    --authentication-kubeconfig="${LOG_DIR}/kubeconfig-etl7.yaml" \
    --authorization-kubeconfig="${LOG_DIR}/kubeconfig-etl7.yaml" \
    --authentication-skip-lookup \
    --health-probe-bind-address=":${HEALTH_ETL7_PORT}" \
    --metrics-bind-address="0" \
    --scylladb-contact-points="localhost:${SCYLLA_ETL7_PORT}" \
    --scylladb-keyspace="${KEYSPACE}" \
    --scylladb-local-dc="etl7" \
    >"${LOG_DIR}/soteria-etl7.log" 2>&1 &
echo $! > "${LOG_DIR}/soteria-etl7.pid"

# ---------------------------------------------------------------------------
# Wait for both Soteria instances to become healthy
# ---------------------------------------------------------------------------
echo ""
echo "Waiting for Soteria instances to become healthy..."
for label_port in "etl6:${SOTERIA_ETL6_PORT}" "etl7:${SOTERIA_ETL7_PORT}"; do
  label="${label_port%%:*}"
  port="${label_port##*:}"
  for i in $(seq 1 60); do
    if curl -sk "https://localhost:${port}/readyz" &>/dev/null; then
      echo "  Soteria (${label}) on :${port} is ready."
      break
    fi
    if [[ ${i} -eq 60 ]]; then
      echo "Error: Soteria (${label}) did not become ready" >&2
      echo "  Check logs: ${LOG_DIR}/soteria-${label}.log"
      tail -30 "${LOG_DIR}/soteria-${label}.log" || true
      exit 1
    fi
    sleep 2
  done
done

# ---------------------------------------------------------------------------
# Register APIService on both Kubernetes clusters
#
# Creates a headless Service + Endpoints pointing at the local machine so
# each cluster's kube-apiserver proxies soteria.io/v1alpha1 requests to the
# local Soteria instance. Uses insecureSkipTLSVerify for simplicity (the
# Soteria instances use self-signed certs in local-dev mode).
# ---------------------------------------------------------------------------
if [[ -z "${HOST_IP:-}" ]]; then
  HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
  if [[ -z "${HOST_IP}" ]]; then
    HOST_IP=$(ip route get 1.1.1.1 2>/dev/null \
      | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' || true)
  fi
fi

if [[ -z "${HOST_IP:-}" ]]; then
  echo ""
  echo "Warning: could not detect host IP reachable from the clusters."
  echo "  Set HOST_IP=<ip> and re-run, or register the APIService manually."
  echo "  Soteria instances are still running and reachable directly."
else
  echo ""
  echo "Registering APIService on both clusters (host IP: ${HOST_IP})..."

  register_apiservice() {
    local ctx="$1"
    local port="$2"

    echo "  Context ${ctx} → localhost:${port}..."

    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" \
      create namespace "${SOTERIA_NS}" --dry-run=client -o yaml \
      | kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" apply -f -

    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: soteria-apiserver
  namespace: ${SOTERIA_NS}
spec:
  type: ClusterIP
  ports:
  - port: 443
    targetPort: ${port}
    protocol: TCP
EOF

    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" apply -f - <<EOF
apiVersion: v1
kind: Endpoints
metadata:
  name: soteria-apiserver
  namespace: ${SOTERIA_NS}
subsets:
- addresses:
  - ip: "${HOST_IP}"
  ports:
  - port: ${port}
    protocol: TCP
EOF

    kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${ctx}" apply -f - <<EOF
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.soteria.io
spec:
  group: soteria.io
  version: v1alpha1
  service:
    namespace: ${SOTERIA_NS}
    name: soteria-apiserver
  groupPriorityMinimum: 1000
  versionPriority: 100
  insecureSkipTLSVerify: true
EOF
  }

  register_apiservice "${CTX_ETL6}" "${SOTERIA_ETL6_PORT}"
  register_apiservice "${CTX_ETL7}" "${SOTERIA_ETL7_PORT}"

  echo "  APIService registration complete."
fi

# ---------------------------------------------------------------------------
# Populate sample data (DRPlans + DRExecutions across both DCs)
# ---------------------------------------------------------------------------
echo ""
echo "Populating sample DRPlans and DRExecutions..."

ETL6_TOKEN=$(kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${CTX_ETL6}" \
  config view --minify -o jsonpath='{.users[0].user.token}' --raw 2>/dev/null)
ETL7_TOKEN=$(kubectl --kubeconfig="${KUBECONFIG_FILE}" --context="${CTX_ETL7}" \
  config view --minify -o jsonpath='{.users[0].user.token}' --raw 2>/dev/null)

soteria_create() {
  local port="$1" token="$2" resource="$3" body="$4"
  curl -sk -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -X POST "https://localhost:${port}/apis/soteria.io/v1alpha1/namespaces/default/${resource}" \
    -d "${body}"
}

echo "  DRPlan 'finance-dr' → etl6..."
soteria_create "${SOTERIA_ETL6_PORT}" "${ETL6_TOKEN}" "drplans" '{
  "apiVersion": "soteria.io/v1alpha1", "kind": "DRPlan",
  "metadata": {"name": "finance-dr", "namespace": "default"},
  "spec": {
    "vmSelector": {"matchLabels": {"department": "finance", "tier": "critical"}},
    "waveLabel": "dr-wave",
    "maxConcurrentFailovers": 5
  }
}' >/dev/null

echo "  DRPlan 'payments-dr' → etl7..."
soteria_create "${SOTERIA_ETL7_PORT}" "${ETL7_TOKEN}" "drplans" '{
  "apiVersion": "soteria.io/v1alpha1", "kind": "DRPlan",
  "metadata": {"name": "payments-dr", "namespace": "default"},
  "spec": {
    "vmSelector": {"matchLabels": {"app": "payments", "env": "prod"}},
    "waveLabel": "failover-wave",
    "maxConcurrentFailovers": 2
  }
}' >/dev/null

sleep 1

echo "  DRExecution 'finance-failover-001' → etl6..."
soteria_create "${SOTERIA_ETL6_PORT}" "${ETL6_TOKEN}" "drexecutions" '{
  "apiVersion": "soteria.io/v1alpha1", "kind": "DRExecution",
  "metadata": {"name": "finance-failover-001", "namespace": "default"},
  "spec": {"planName": "finance-dr", "mode": "planned_migration"}
}' >/dev/null

echo "  DRExecution 'payments-disaster-001' → etl7..."
soteria_create "${SOTERIA_ETL7_PORT}" "${ETL7_TOKEN}" "drexecutions" '{
  "apiVersion": "soteria.io/v1alpha1", "kind": "DRExecution",
  "metadata": {"name": "payments-disaster-001", "namespace": "default"},
  "spec": {"planName": "payments-dr", "mode": "disaster"}
}' >/dev/null

echo "  Sample data populated."

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
cat <<SUMMARY

==================================================================
 Stretched-cluster test environment is running
==================================================================

 ScyllaDB cluster (eventual consistency):
   DC etl6 : localhost:${SCYLLA_ETL6_PORT}  (container: ${SCYLLA_ETL6_NAME})
   DC etl7 : localhost:${SCYLLA_ETL7_PORT}  (container: ${SCYLLA_ETL7_NAME})
   Keyspace: ${KEYSPACE}  (NetworkTopologyStrategy, etl6:1, etl7:1)

 Soteria API servers:
   etl6 : https://localhost:${SOTERIA_ETL6_PORT}  (--scylladb-local-dc=etl6)
   etl7 : https://localhost:${SOTERIA_ETL7_PORT}  (--scylladb-local-dc=etl7)

 Logs:
   ${LOG_DIR}/soteria-etl6.log
   ${LOG_DIR}/soteria-etl7.log

 Sample data:
   DRPlans:      finance-dr (via etl6), payments-dr (via etl7)
   DRExecutions: finance-failover-001 (planned_migration), payments-disaster-001 (disaster)

 Token setup (paste into your shell):
   ETL6_TOKEN=\$(kubectl --context=${CTX_ETL6} config view --minify -o jsonpath='{.users[0].user.token}' --raw)
   ETL7_TOKEN=\$(kubectl --context=${CTX_ETL7} config view --minify -o jsonpath='{.users[0].user.token}' --raw)

 Retrieve DRPlans:
   curl -sk -H "Authorization: Bearer \${ETL6_TOKEN}" https://localhost:${SOTERIA_ETL6_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans
   curl -sk -H "Authorization: Bearer \${ETL7_TOKEN}" https://localhost:${SOTERIA_ETL7_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans
   curl -sk -H "Authorization: Bearer \${ETL6_TOKEN}" https://localhost:${SOTERIA_ETL6_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans/finance-dr
   curl -sk -H "Authorization: Bearer \${ETL7_TOKEN}" https://localhost:${SOTERIA_ETL7_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans/payments-dr

 Retrieve DRExecutions:
   curl -sk -H "Authorization: Bearer \${ETL6_TOKEN}" https://localhost:${SOTERIA_ETL6_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drexecutions
   curl -sk -H "Authorization: Bearer \${ETL7_TOKEN}" https://localhost:${SOTERIA_ETL7_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drexecutions
   curl -sk -H "Authorization: Bearer \${ETL6_TOKEN}" https://localhost:${SOTERIA_ETL6_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drexecutions/finance-failover-001
   curl -sk -H "Authorization: Bearer \${ETL7_TOKEN}" https://localhost:${SOTERIA_ETL7_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drexecutions/payments-disaster-001

 Cross-DC replication (write etl6, read etl7):
   curl -sk -H "Authorization: Bearer \${ETL6_TOKEN}" -H 'Content-Type: application/json' \\
     -X POST https://localhost:${SOTERIA_ETL6_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans \\
     -d '{"apiVersion":"soteria.io/v1alpha1","kind":"DRPlan","metadata":{"name":"cross-dc-test","namespace":"default"},"spec":{"vmSelector":{"matchLabels":{"test":"xdc"}},"waveLabel":"w","maxConcurrentFailovers":1}}'
   sleep 2
   curl -sk -H "Authorization: Bearer \${ETL7_TOKEN}" https://localhost:${SOTERIA_ETL7_PORT}/apis/soteria.io/v1alpha1/namespaces/default/drplans/cross-dc-test

 Tear down:
   ./hack/stretched-local-test.sh stop

==================================================================

SUMMARY

echo "Press Ctrl+C to tear down..."
wait
