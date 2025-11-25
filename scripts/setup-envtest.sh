#!/bin/bash

set -e

ENVTEST_K8S_VERSION="${ENVTEST_K8S_VERSION:-1.32.0}"
TESTBIN_DIR="${TESTBIN_DIR:-./testbin}"

echo "Setting up envtest binaries..."

# Install setup-envtest if not available
if ! command -v setup-envtest &> /dev/null; then
    echo "Installing setup-envtest..."
    go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
fi

# Download envtest binaries
echo "Downloading Kubernetes ${ENVTEST_K8S_VERSION} binaries to ${TESTBIN_DIR}..."
setup-envtest use "${ENVTEST_K8S_VERSION}" --bin-dir "${TESTBIN_DIR}"

# Create symlink for backward compatibility
ENVTEST_BIN_PATH=$(setup-envtest use "${ENVTEST_K8S_VERSION}" --bin-dir "${TESTBIN_DIR}" -p path)
EXPECTED_PATH="${TESTBIN_DIR}/k8s/k8s/${ENVTEST_K8S_VERSION}-linux-amd64"

# Create the directory structure if it doesn't exist
mkdir -p "$(dirname "${EXPECTED_PATH}")"

# Get absolute paths
ABSOLUTE_ENVTEST_PATH=$(cd "$(dirname "${ENVTEST_BIN_PATH}")" && pwd)/$(basename "${ENVTEST_BIN_PATH}")
ABSOLUTE_EXPECTED_PATH=$(cd "$(dirname "${EXPECTED_PATH}")" && pwd)/$(basename "${EXPECTED_PATH}")

# Create symlink if it doesn't exist
if [ ! -L "${ABSOLUTE_EXPECTED_PATH}" ] && [ ! -d "${ABSOLUTE_EXPECTED_PATH}" ]; then
    echo "Creating compatibility symlink: ${ABSOLUTE_EXPECTED_PATH} -> ${ABSOLUTE_ENVTEST_PATH}"
    ln -s "${ABSOLUTE_ENVTEST_PATH}" "${ABSOLUTE_EXPECTED_PATH}"
fi

echo "Envtest setup complete!"
echo "Binaries available at: ${ENVTEST_BIN_PATH}"