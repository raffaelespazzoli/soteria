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

TMP_DIFFROOT=$(mktemp -d)

cleanup() {
    rm -rf "${TMP_DIFFROOT}"
}
trap cleanup EXIT

cp -a "${SCRIPT_ROOT}/pkg" "${TMP_DIFFROOT}"
cp -a "${SCRIPT_ROOT}/hack" "${TMP_DIFFROOT}"

"${SCRIPT_ROOT}/hack/update-codegen.sh"

echo "Diffing generated code against committed code..."

ret=0
diff -Naupr "${TMP_DIFFROOT}/pkg" "${SCRIPT_ROOT}/pkg" || ret=$?
diff -Naupr "${TMP_DIFFROOT}/hack/api-violations.list" "${SCRIPT_ROOT}/hack/api-violations.list" || ret=$?

if [[ $ret -ne 0 ]]; then
    echo ""
    echo "ERROR: Generated code is out of date. Run hack/update-codegen.sh to update."
    exit 1
fi

echo "Generated code is up to date."
