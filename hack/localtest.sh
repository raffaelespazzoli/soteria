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

# Starts a local ScyllaDB container and the Soteria API server + controller
# manager against the current kubeconfig context.
#
# Prerequisites:
#   - podman (or docker)
#   - go 1.25+
#   - A valid kubeconfig (oc login / kubectl context)
#
# Usage:
#   ./hack/localtest.sh          # start everything
#   ./hack/localtest.sh stop     # tear down ScyllaDB container

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${SCRIPT_ROOT}"

CONTAINER_NAME="scylladb-soteria-local"
CONTAINER_IMAGE="docker.io/scylladb/scylla:latest"
SCYLLA_PORT="${SCYLLA_PORT:-9042}"
SECURE_PORT="${SECURE_PORT:-6443}"
KEYSPACE="${KEYSPACE:-soteria}"
KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"

# Prefer podman; fall back to docker.
if command -v podman &>/dev/null; then
  CTR=podman
elif command -v docker &>/dev/null; then
  CTR=docker
else
  echo "Error: neither podman nor docker found" >&2
  exit 1
fi

stop() {
  echo "Stopping ScyllaDB container ${CONTAINER_NAME}..."
  ${CTR} rm -f "${CONTAINER_NAME}" 2>/dev/null || true
  echo "Done."
}

if [[ "${1:-}" == "stop" ]]; then
  stop
  exit 0
fi

trap 'echo; echo "Shutting down..."; stop' EXIT INT TERM

# --- ScyllaDB -----------------------------------------------------------

if ${CTR} inspect "${CONTAINER_NAME}" &>/dev/null; then
  echo "ScyllaDB container ${CONTAINER_NAME} already exists, reusing it."
  ${CTR} start "${CONTAINER_NAME}" 2>/dev/null || true
else
  echo "Starting ScyllaDB container ${CONTAINER_NAME} on port ${SCYLLA_PORT}..."
  ${CTR} run -d \
    --name "${CONTAINER_NAME}" \
    -p "${SCYLLA_PORT}:9042" \
    "${CONTAINER_IMAGE}" \
    --smp 1 --memory 512M --overprovisioned 1
fi

echo "Waiting for ScyllaDB to accept CQL connections..."
for i in $(seq 1 60); do
  if ${CTR} exec "${CONTAINER_NAME}" cqlsh -e "SELECT now() FROM system.local" &>/dev/null; then
    echo "ScyllaDB is ready."
    break
  fi
  if [[ ${i} -eq 60 ]]; then
    echo "Error: ScyllaDB did not become ready in time" >&2
    exit 1
  fi
  sleep 2
done

echo "Ensuring keyspace '${KEYSPACE}' exists..."
${CTR} exec "${CONTAINER_NAME}" cqlsh -e \
  "CREATE KEYSPACE IF NOT EXISTS ${KEYSPACE} WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};" \
  2>/dev/null

# --- Soteria -------------------------------------------------------------

echo ""
echo "Starting Soteria (API server on :${SECURE_PORT}, health probe on :8081)..."
echo "  kubeconfig:  ${KUBECONFIG}"
echo "  scylladb:    localhost:${SCYLLA_PORT}  keyspace=${KEYSPACE}"
echo ""

exec go run ./cmd/soteria/ \
  --secure-port="${SECURE_PORT}" \
  --kubeconfig="${KUBECONFIG}" \
  --authentication-kubeconfig="${KUBECONFIG}" \
  --authorization-kubeconfig="${KUBECONFIG}" \
  --authentication-skip-lookup \
  --scylladb-contact-points="localhost:${SCYLLA_PORT}" \
  --scylladb-keyspace="${KEYSPACE}"
