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

# Shared helpers for hack/multisite scripts.
# Source after SCRIPT_DIR is set (BIN_DIR defaults to ${SCRIPT_DIR}/.bin).

_MULTISITE_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -z "${BIN_DIR:-}" ]]; then
  BIN_DIR="${SCRIPT_DIR:-${_MULTISITE_LIB_DIR}}/.bin"
fi

_multisite_info() {
  if declare -F info >/dev/null 2>&1; then
    info "$@"
  else
    echo "[INFO]  $*"
  fi
}

_multisite_fatal() {
  if declare -F fatal >/dev/null 2>&1; then
    fatal "$@"
  else
    echo "[ERROR] $*" >&2
    exit 1
  fi
}

ensure_minikube() {
  if command -v minikube &>/dev/null; then
    return 0
  fi

  if [[ -x "${BIN_DIR}/minikube" ]]; then
    export PATH="${BIN_DIR}:${PATH}"
    return 0
  fi

  _multisite_info "minikube not found — downloading to ${BIN_DIR}/..."
  mkdir -p "${BIN_DIR}"

  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
  esac

  curl -fsSL "https://storage.googleapis.com/minikube/releases/latest/minikube-${os}-${arch}" \
    -o "${BIN_DIR}/minikube"
  chmod +x "${BIN_DIR}/minikube"

  export PATH="${BIN_DIR}:${PATH}"
  _multisite_info "minikube installed: $(minikube version --short 2>/dev/null)"
}

ensure_cilium_cli() {
  if command -v cilium &>/dev/null; then
    return 0
  fi

  if [[ -x "${BIN_DIR}/cilium" ]]; then
    export PATH="${BIN_DIR}:${PATH}"
    return 0
  fi

  _multisite_info "cilium CLI not found — downloading to ${BIN_DIR}/..."
  mkdir -p "${BIN_DIR}"

  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
  esac

  local release_url="https://github.com/cilium/cilium-cli/releases/latest/download"
  local tarball="cilium-${os}-${arch}.tar.gz"
  local checksum_file="cilium-${os}-${arch}.tar.gz.sha256sum"

  curl -fsSL "${release_url}/${tarball}" -o "${BIN_DIR}/${tarball}"
  curl -fsSL "${release_url}/${checksum_file}" -o "${BIN_DIR}/${checksum_file}"

  (cd "${BIN_DIR}" && sha256sum --check "${checksum_file}") || _multisite_fatal "cilium CLI checksum verification failed"

  tar -xzf "${BIN_DIR}/${tarball}" -C "${BIN_DIR}"
  rm -f "${BIN_DIR}/${tarball}" "${BIN_DIR}/${checksum_file}"
  chmod +x "${BIN_DIR}/cilium"

  export PATH="${BIN_DIR}:${PATH}"
  _multisite_info "cilium CLI installed: $(cilium version --client 2>/dev/null | head -1)"
}
