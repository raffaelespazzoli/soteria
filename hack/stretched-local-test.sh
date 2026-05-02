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

# Stretched-cluster deployment and validation test.
#
# Deploys Soteria and ScyllaDB onto two Kubernetes clusters connected
# via Submariner, forming a multi-DC ScyllaDB cluster with eventual
# consistency (NetworkTopologyStrategy, LocalOne reads/writes).
#
#   etl6 cluster: ScyllaDB DC "etl6" + Soteria API server
#   etl7 cluster: ScyllaDB DC "etl7" + Soteria API server
#
# Cross-DC ScyllaDB gossip uses Submariner MCS (Multicluster Services API)
# to discover seed nodes in the remote cluster.
#
# Prerequisites:
#   - kubectl with contexts "etl6" and "etl7"
#   - kustomize (or use bin/kustomize from make kustomize)
#   - cert-manager installed on both clusters
#   - scylla-operator installed on both clusters
#   - Submariner with MCS API active between both clusters
#   - Issuer "soteria-internal" exists in soteria namespace on both clusters
#   - Container image available (set IMG= or push via make docker-push)
#
# Usage:
#   ./hack/stretched-local-test.sh          # deploy + validate
#   ./hack/stretched-local-test.sh stop     # tear down

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${SCRIPT_ROOT}"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
IMG="${IMG:-quay.io/raffaelespazzoli/soteria:latest}"
CONSOLE_PLUGIN_IMG="${CONSOLE_PLUGIN_IMG:-quay.io/raffaelespazzoli/soteria-console-plugin:latest}"
KEYSPACE="${KEYSPACE:-soteria}"
NAMESPACE="soteria"
DR_TEST_NS="soteria-dr-test"
DR_TEST_SC="${DR_TEST_SC:-ontap-san}"
DR_TEST_DISK_SIZE="${DR_TEST_DISK_SIZE:-30Gi}"

KUBECONFIG_FILE="${KUBECONFIG:-${HOME}/.kube/config}"
CTX_ETL6="${CTX_ETL6:-etl6}"
CTX_ETL7="${CTX_ETL7:-etl7}"

OVERLAY_ETL6="${SCRIPT_ROOT}/hack/overlays/etl6"
OVERLAY_ETL7="${SCRIPT_ROOT}/hack/overlays/etl7"

# Detect kustomize binary
if [[ -x "${SCRIPT_ROOT}/bin/kustomize" ]]; then
  KUSTOMIZE="${SCRIPT_ROOT}/bin/kustomize"
elif command -v kustomize &>/dev/null; then
  KUSTOMIZE=kustomize
else
  echo "Error: kustomize not found. Run 'make kustomize' or install kustomize." >&2
  exit 1
fi

kctl() {
  kubectl --kubeconfig="${KUBECONFIG_FILE}" "$@"
}

kustomize_build() {
  "${KUSTOMIZE}" build --load-restrictor LoadRestrictionsNone "$@"
}

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
stop() {
  echo ""
  echo "=== Tearing down stretched-cluster deployment ==="

  for ctx in "${CTX_ETL7}" "${CTX_ETL6}"; do
    local overlay="${OVERLAY_ETL6}"
    [[ "${ctx}" == "${CTX_ETL7}" ]] && overlay="${OVERLAY_ETL7}"

    echo "Deleting console plugin from context ${ctx}..."
    kctl --context="${ctx}" delete consoleplugin soteria-console-plugin --ignore-not-found 2>/dev/null || true
    kctl --context="${ctx}" -n "${NAMESPACE}" delete -f "${SCRIPT_ROOT}/hack/overlays/base/console-plugin.yaml" --ignore-not-found 2>/dev/null || true

    echo "Deleting resources from context ${ctx}..."
    kustomize_build "${overlay}" \
      | kctl --context="${ctx}" delete --ignore-not-found -f - 2>/dev/null || true

    echo "Deleting DR test namespace from context ${ctx}..."
    kctl --context="${ctx}" delete namespace "${DR_TEST_NS}" --ignore-not-found 2>/dev/null || true
  done

  echo "Done."
}

if [[ "${1:-}" == "stop" ]]; then
  stop
  exit 0
fi

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
echo "=== Preflight checks ==="

for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  if ! kctl --context="${ctx}" cluster-info &>/dev/null; then
    echo "Error: cannot reach cluster via context '${ctx}'" >&2
    exit 1
  fi
  echo "  Context ${ctx}: reachable"
done

# Verify scylla-operator is running on both clusters
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  if ! kctl --context="${ctx}" get crd scyllaclusters.scylla.scylladb.com &>/dev/null; then
    echo "Error: scylla-operator CRD not found on ${ctx}. Install scylla-operator first." >&2
    exit 1
  fi
done
echo "  scylla-operator: installed on both clusters"

# Verify cert-manager is running
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  if ! kctl --context="${ctx}" get crd certificates.cert-manager.io &>/dev/null; then
    echo "Error: cert-manager CRD not found on ${ctx}. Install cert-manager first." >&2
    exit 1
  fi
done
echo "  cert-manager: installed on both clusters"

# Verify Submariner MCS
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  if ! kctl --context="${ctx}" get crd serviceexports.multicluster.x-k8s.io &>/dev/null; then
    echo "Warning: MCS ServiceExport CRD not found on ${ctx}. Submariner MCS may not be active." >&2
  fi
done
echo "  Submariner MCS: checked"

echo ""

# ---------------------------------------------------------------------------
# Build and push container image
# ---------------------------------------------------------------------------
echo "=== Building and pushing container image: ${IMG} ==="
make -C "${SCRIPT_ROOT}" docker-build docker-push IMG="${IMG}"

# ---------------------------------------------------------------------------
# Build and push console plugin image
# ---------------------------------------------------------------------------
CONTAINER_TOOL="${CONTAINER_TOOL:-docker}"

echo "=== Building and pushing console plugin image: ${CONSOLE_PLUGIN_IMG} ==="
"${CONTAINER_TOOL}" build -t "${CONSOLE_PLUGIN_IMG}" "${SCRIPT_ROOT}/console-plugin"
"${CONTAINER_TOOL}" push "${CONSOLE_PLUGIN_IMG}"

# ---------------------------------------------------------------------------
# Set container image in kustomize
# ---------------------------------------------------------------------------
echo "=== Setting container image: ${IMG} ==="
(cd "${SCRIPT_ROOT}/config/manager" && "${KUSTOMIZE}" edit set image "controller=${IMG}")

# ---------------------------------------------------------------------------
# Helper: wait for ScyllaDB prerequisites and create combined-ca ConfigMap
# ---------------------------------------------------------------------------
# ScyllaDB pods mount scylladb-combined-ca which bundles the cert-manager CA
# and the operator's internal client CA. Both secrets are created
# asynchronously after `kctl apply`, so we wait for them and create the
# ConfigMap before the ScyllaDB readiness check.
create_combined_ca() {
  local ctx="$1"
  local CM_CA="" OP_CA=""
  echo "  ${ctx}: waiting for cert-manager and operator secrets..."
  for _ in $(seq 1 60); do
    local raw_ca
    raw_ca=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
      get secret scylladb-serving-tls -o jsonpath='{.data.ca\.crt}' 2>/dev/null) || true
    if [[ -n "${raw_ca}" ]]; then
      CM_CA=$(echo "${raw_ca}" | base64 -d 2>/dev/null) || true
    fi
    OP_CA=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
      get configmap soteria-scylladb-local-client-ca -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null) || true
    if [[ -n "${CM_CA}" && -n "${OP_CA}" ]]; then
      break
    fi
    sleep 5
  done
  if [[ -z "${CM_CA}" || -z "${OP_CA}" ]]; then
    echo "Warning: could not build combined CA on ${ctx} (cert-manager or operator secret missing)" >&2
    return 1
  fi
  kctl --context="${ctx}" -n "${NAMESPACE}" apply -f - <<CAEOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: scylladb-combined-ca
  namespace: ${NAMESPACE}
data:
  ca-bundle.crt: |
$(echo "${CM_CA}" | sed 's/^/    /')
$(echo "${OP_CA}" | sed 's/^/    /')
CAEOF
  echo "  ${ctx}: combined CA ConfigMap created"
}

# ---------------------------------------------------------------------------
# Helper: wait for ScyllaDB to become ready
# ---------------------------------------------------------------------------
wait_scylladb_ready() {
  local ctx="$1"
  echo "Waiting for ScyllaDB to become ready on ${ctx} (${MEMBERS_PER_RACK} members)..."
  for i in $(seq 1 180); do
    status=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
      get scyllaclusters.scylla.scylladb.com soteria-scylladb \
      -o jsonpath='{.status.racks.rack1.readyMembers}' 2>/dev/null || echo "0")
    if [[ "${status}" -ge "${MEMBERS_PER_RACK}" ]]; then
      echo "  ScyllaDB on ${ctx}: ${status} member(s) ready"
      return 0
    fi
    if [[ ${i} -eq 180 ]]; then
      echo "Error: ScyllaDB on ${ctx} did not become ready in 15 minutes" >&2
      kctl --context="${ctx}" -n "${NAMESPACE}" describe scyllaclusters.scylla.scylladb.com soteria-scylladb || true
      return 1
    fi
    sleep 5
  done
}

# ---------------------------------------------------------------------------
# Deploy etl6 (seed cluster — no externalSeeds)
# ---------------------------------------------------------------------------
echo ""
echo "=== Deploying to ${CTX_ETL6} (seed DC) ==="
kustomize_build "${OVERLAY_ETL6}" | kctl --context="${CTX_ETL6}" apply --server-side --force-conflicts -f -

MEMBERS_PER_RACK=2
create_combined_ca "${CTX_ETL6}"
wait_scylladb_ready "${CTX_ETL6}" || exit 1

# ---------------------------------------------------------------------------
# Deploy etl7 (joins etl6 via MCS externalSeeds)
# ---------------------------------------------------------------------------
echo ""
echo "=== Deploying to ${CTX_ETL7} (joining DC) ==="
kustomize_build "${OVERLAY_ETL7}" | kctl --context="${CTX_ETL7}" apply --server-side --force-conflicts -f -

create_combined_ca "${CTX_ETL7}"
wait_scylladb_ready "${CTX_ETL7}" || exit 1

# ---------------------------------------------------------------------------
# Post-deploy: cert-manager TLS volumes on ScyllaDB STS
# ---------------------------------------------------------------------------
# The scylla-operator v1.20.2 does NOT propagate rack-level volumes/
# volumeMounts from the ScyllaCluster CR to the StatefulSet. Work around
# this by patching the STS directly. The combined-ca ConfigMap was already
# created in the create_combined_ca step above.
echo ""
echo "=== Patching ScyllaDB StatefulSets with cert-manager TLS volumes ==="

for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  echo "  ${ctx}: patching STS volumes..."
  STS=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
    get sts -l scylla/cluster=soteria-scylladb -o name 2>/dev/null | head -1)

  SCYLLA_IDX=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
    get "${STS}" -o json 2>/dev/null \
    | jq -r '.spec.template.spec.containers | to_entries[] | select(.value.name=="scylla") | .key')

  # Add volumes (idempotent: check if already present)
  HAS_CM_VOL=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
    get "${STS}" -o json 2>/dev/null \
    | jq -r '[.spec.template.spec.volumes[].name] | if index("certmanager-serving") then "yes" else "no" end')

  if [[ "${HAS_CM_VOL}" == "no" ]]; then
    kctl --context="${ctx}" -n "${NAMESPACE}" patch "${STS}" --type=json -p "[
      {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"certmanager-serving\",\"secret\":{\"secretName\":\"scylladb-serving-tls\"}}},
      {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"certmanager-ca\",\"secret\":{\"secretName\":\"scylladb-serving-tls\",\"items\":[{\"key\":\"ca.crt\",\"path\":\"ca-bundle.crt\"}]}}},
      {\"op\":\"add\",\"path\":\"/spec/template/spec/volumes/-\",\"value\":{\"name\":\"combined-ca\",\"configMap\":{\"name\":\"scylladb-combined-ca\"}}}
    ]"
    kctl --context="${ctx}" -n "${NAMESPACE}" patch "${STS}" --type=json -p "[
      {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"certmanager-serving\",\"mountPath\":\"/etc/scylla/certmanager-tls\",\"readOnly\":true}},
      {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"certmanager-ca\",\"mountPath\":\"/etc/scylla/certmanager-ca\",\"readOnly\":true}},
      {\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${SCYLLA_IDX}/volumeMounts/-\",\"value\":{\"name\":\"combined-ca\",\"mountPath\":\"/etc/scylla/combined-ca\",\"readOnly\":true}}
    ]"
    echo "  ${ctx}: restarting ScyllaDB pods to apply TLS volume changes..."
    kctl --context="${ctx}" -n "${NAMESPACE}" delete pods -l scylla/cluster=soteria-scylladb --wait=false
  else
    echo "  ${ctx}: cert-manager volumes already present in STS"
  fi
done

# Wait for ScyllaDB pods to restart with TLS config
echo "Waiting for ScyllaDB pods to restart..."
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  for i in $(seq 1 180); do
    status=$(kctl --context="${ctx}" -n "${NAMESPACE}" \
      get scyllaclusters.scylla.scylladb.com soteria-scylladb \
      -o jsonpath='{.status.racks.rack1.readyMembers}' 2>/dev/null || echo "0")
    if [[ "${status}" -ge "${MEMBERS_PER_RACK}" ]]; then
      echo "  ${ctx}: ${status} member(s) ready"
      break
    fi
    if [[ ${i} -eq 180 ]]; then
      echo "Warning: ScyllaDB on ${ctx} not ready after TLS restart" >&2
    fi
    sleep 5
  done
done

# ---------------------------------------------------------------------------
# Wait for multi-DC cluster convergence
# ---------------------------------------------------------------------------
echo ""
echo "Waiting for ScyllaDB multi-DC cluster convergence..."
ETL6_POD=$(kctl --context="${CTX_ETL6}" -n "${NAMESPACE}" \
  get pods -l "scylla/cluster=soteria-scylladb" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

if [[ -n "${ETL6_POD}" ]]; then
  for i in $(seq 1 60); do
    node_count=$(kctl --context="${CTX_ETL6}" -n "${NAMESPACE}" \
      exec "${ETL6_POD}" -c scylla -- nodetool status 2>/dev/null \
      | grep -c "^UN" || true)
    node_count="${node_count:-0}"
    if [[ "${node_count}" -ge 4 ]]; then
      echo "  Cluster converged — ${node_count} UN nodes across DCs"
      echo ""
      kctl --context="${CTX_ETL6}" -n "${NAMESPACE}" \
        exec "${ETL6_POD}" -c scylla -- nodetool status 2>/dev/null || true
      echo ""
      break
    fi
    if [[ ${i} -eq 60 ]]; then
      echo "Warning: only ${node_count} UN nodes detected (expected >=4 for multi-DC)" >&2
    fi
    sleep 5
  done
fi

# ---------------------------------------------------------------------------
# Wait for Soteria deployments to roll out
# (Keyspace and tables are auto-created by Soteria via --scylladb-dc-replication)
# ---------------------------------------------------------------------------
echo ""
echo "=== Waiting for Soteria deployments ==="
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  echo "  Waiting for rollout on ${ctx}..."
  kctl --context="${ctx}" -n "${NAMESPACE}" \
    rollout status deployment/soteria-controller-manager --timeout=900s || {
      echo "Warning: Soteria deployment on ${ctx} did not become ready" >&2
      kctl --context="${ctx}" -n "${NAMESPACE}" describe deployment/soteria-controller-manager || true
    }
done

# ---------------------------------------------------------------------------
# Verify APIService availability
# ---------------------------------------------------------------------------
echo ""
echo "=== Verifying APIService registration ==="
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  available=$(kctl --context="${ctx}" get apiservice v1alpha1.soteria.io \
    -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || echo "Unknown")
  echo "  ${ctx}: APIService v1alpha1.soteria.io Available=${available}"
done

# ---------------------------------------------------------------------------
# Deploy console plugin
# ---------------------------------------------------------------------------
echo ""
echo "=== Deploying console plugin ==="

CONSOLE_PLUGIN_MANIFEST="${SCRIPT_ROOT}/hack/overlays/base/console-plugin.yaml"

for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  echo "  ${ctx}: deploying console plugin..."
  sed "s|CONSOLE_PLUGIN_IMG_PLACEHOLDER|${CONSOLE_PLUGIN_IMG}|g" "${CONSOLE_PLUGIN_MANIFEST}" \
    | kctl --context="${ctx}" apply --server-side -f -
done

echo "Waiting for console plugin rollout..."
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  kctl --context="${ctx}" -n "${NAMESPACE}" \
    rollout status deployment/soteria-console-plugin --timeout=120s || {
      echo "Warning: Console plugin deployment on ${ctx} did not become ready" >&2
      kctl --context="${ctx}" -n "${NAMESPACE}" describe deployment/soteria-console-plugin || true
    }
done

echo "Enabling console plugin on OpenShift console..."
for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  CURRENT_PLUGINS=$(kctl --context="${ctx}" get consoles.operator.openshift.io cluster \
    -o jsonpath='{.spec.plugins[*]}' 2>/dev/null || echo "")
  if echo "${CURRENT_PLUGINS}" | grep -qw "soteria-console-plugin"; then
    echo "  ${ctx}: console plugin already enabled"
  else
    if [[ -z "${CURRENT_PLUGINS}" ]]; then
      kctl --context="${ctx}" patch consoles.operator.openshift.io cluster --type=merge \
        -p '{"spec":{"plugins":["soteria-console-plugin"]}}' 2>/dev/null || true
    else
      kctl --context="${ctx}" patch consoles.operator.openshift.io cluster --type=json \
        -p '[{"op":"add","path":"/spec/plugins/-","value":"soteria-console-plugin"}]' 2>/dev/null || true
    fi
    echo "  ${ctx}: console plugin enabled"
  fi
done

# ---------------------------------------------------------------------------
# Deploy DR test VMs (Fedora) with DRPlan
# ---------------------------------------------------------------------------
echo ""
echo "=== Creating DR test namespace and Fedora VMs ==="

for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  kctl --context="${ctx}" create namespace "${DR_TEST_NS}" --dry-run=client -o yaml \
    | kctl --context="${ctx}" apply --server-side -f -
  echo "  ${ctx}: namespace ${DR_TEST_NS} ready"
done

create_fedora_vm() {
  local ctx="$1" name="$2" wave="$3" run_strategy="$4"
  echo "  ${ctx}: creating VM ${name} (wave=${wave}, runStrategy=${run_strategy})..."
  kctl --context="${ctx}" apply -f - <<VMEOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${name}
  namespace: ${DR_TEST_NS}
  labels:
    soteria.io/drplan: fedora-app
    soteria.io/wave: "${wave}"
spec:
  runStrategy: ${run_strategy}
  dataVolumeTemplates:
    - metadata:
        name: ${name}-rootdisk
      spec:
        sourceRef:
          kind: DataSource
          name: fedora
          namespace: openshift-virtualization-os-images
        storage:
          resources:
            requests:
              storage: ${DR_TEST_DISK_SIZE}
          storageClassName: ${DR_TEST_SC}
  template:
    metadata:
      labels:
        soteria.io/drplan: fedora-app
        soteria.io/wave: "${wave}"
    spec:
      domain:
        resources:
          requests:
            memory: 2Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          dataVolume:
            name: ${name}-rootdisk
VMEOF
}

declare -A VM_WAVES=(
  ["fedora-db"]="1"
  ["fedora-appserver-1"]="2"
  ["fedora-appserver-2"]="2"
  ["fedora-webserver-1"]="3"
  ["fedora-webserver-2"]="3"
  ["fedora-webserver-3"]="3"
)

for vm_name in fedora-db fedora-appserver-1 fedora-appserver-2 fedora-webserver-1 fedora-webserver-2 fedora-webserver-3; do
  wave="${VM_WAVES[${vm_name}]}"
  create_fedora_vm "${CTX_ETL6}" "${vm_name}" "${wave}" "Always"
  create_fedora_vm "${CTX_ETL7}" "${vm_name}" "${wave}" "Halted"
done

echo ""
echo "=== Creating DRPlan fedora-app ==="

for ctx in "${CTX_ETL6}" "${CTX_ETL7}"; do
  echo "  DRPlan 'fedora-app' → ${ctx}..."
  kctl --context="${ctx}" apply -f - <<'EOF'
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: fedora-app
  namespace: soteria-dr-test
spec:
  maxConcurrentFailovers: 2
  primarySite: etl6
  secondarySite: etl7
EOF
done

sleep 2
echo "  DR test VMs and DRPlan deployed."

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
cat <<SUMMARY

==================================================================
 Stretched-cluster deployment is running
==================================================================

 Namespace: ${NAMESPACE}

 ScyllaDB multi-DC cluster (eventual consistency):
   DC etl6 : context ${CTX_ETL6}  (ScyllaCluster soteria-scylladb)
   DC etl7 : context ${CTX_ETL7}  (ScyllaCluster soteria-scylladb)
   Keyspace: ${KEYSPACE}  (auto-created by Soteria: NTS, etl6:2, etl7:2, tablets=off)
   Cross-DC: Submariner MCS (soteria-scylladb-client.${NAMESPACE}.svc.clusterset.local)

 Soteria API servers:
   etl6 : APIService v1alpha1.soteria.io (--scylladb-local-dc=etl6, --site-name=etl6)
   etl7 : APIService v1alpha1.soteria.io (--scylladb-local-dc=etl7, --site-name=etl7)

 Console plugin:
   Image: ${CONSOLE_PLUGIN_IMG}
   Plugin: soteria-console-plugin (ConsolePlugin CR)
   Service: soteria-console-plugin:9443 → nginx:9443 (TLS via OpenShift service-CA)

 DR test (namespace: ${DR_TEST_NS}):
   DRPlan: fedora-app (waveLabel: soteria.io/wave, maxConcurrentFailovers: 2)
   Wave 1: fedora-db
   Wave 2: fedora-appserver-1, fedora-appserver-2
   Wave 3: fedora-webserver-1, fedora-webserver-2, fedora-webserver-3
   ${CTX_ETL6}: VMs running (runStrategy: Always)
   ${CTX_ETL7}: VMs stopped (runStrategy: Halted)

 Retrieve DRPlans (via aggregated API):
   kubectl --context=${CTX_ETL6} get drplans -n ${DR_TEST_NS}
   kubectl --context=${CTX_ETL7} get drplans -n ${DR_TEST_NS}

 Retrieve Fedora VMs:
   kubectl --context=${CTX_ETL6} get vm -n ${DR_TEST_NS}
   kubectl --context=${CTX_ETL7} get vm -n ${DR_TEST_NS}

 Cross-DC replication test:
   kubectl --context=${CTX_ETL6} get drplans -n ${DR_TEST_NS} -o name
   kubectl --context=${CTX_ETL7} get drplans -n ${DR_TEST_NS} -o name
   # Both should show the same DRPlans after replication delay

 Logs:
   kubectl --context=${CTX_ETL6} -n ${NAMESPACE} logs deployment/soteria-controller-manager -c manager -f
   kubectl --context=${CTX_ETL7} -n ${NAMESPACE} logs deployment/soteria-controller-manager -c manager -f

 Tear down:
   ./hack/stretched-local-test.sh stop

==================================================================

SUMMARY
