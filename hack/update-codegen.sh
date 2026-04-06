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

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${SCRIPT_ROOT}"

CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator)

source "${CODEGEN_PKG}/kube_codegen.sh"

echo "Generating deepcopy helpers..."
kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/pkg/apis"

echo "Generating OpenAPI definitions..."
kube::codegen::gen_openapi \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    --output-dir "${SCRIPT_ROOT}/pkg/apis/soteria.io/v1alpha1" \
    --output-pkg "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1" \
    --report-filename "${SCRIPT_ROOT}/hack/api-violations.list" \
    --update-report \
    --extra-pkgs "k8s.io/apimachinery/pkg/apis/meta/v1" \
    --extra-pkgs "k8s.io/apimachinery/pkg/runtime" \
    --extra-pkgs "k8s.io/apimachinery/pkg/version" \
    "${SCRIPT_ROOT}/pkg/apis"

echo "Code generation complete."
