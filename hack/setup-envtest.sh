#!/usr/bin/env bash
set -euo pipefail

# Match controller-runtime minor line in go.mod.
ENVTEST_K8S_VERSION="${ENVTEST_K8S_VERSION:-1.31.x}"
SETUP_ENVTEST_VERSION="${SETUP_ENVTEST_VERSION:-release-0.20}"

if ! command -v setup-envtest >/dev/null 2>&1; then
  go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VERSION}"
fi

ASSETS_PATH="$(setup-envtest use "${ENVTEST_K8S_VERSION}" -p path)"
echo "export KUBEBUILDER_ASSETS=${ASSETS_PATH}"
