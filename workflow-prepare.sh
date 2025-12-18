#!/bin/bash
set -e

echo "Setting up envtest binaries..."

# Install setup-envtest
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Download and setup envtest binaries
ENVTEST_ASSETS=$(setup-envtest use 1.31.0 --bin-dir /tmp/envtest -p path)
echo "KUBEBUILDER_ASSETS=${ENVTEST_ASSETS}" >> $GITHUB_ENV

echo "Envtest binaries installed at: ${ENVTEST_ASSETS}"
