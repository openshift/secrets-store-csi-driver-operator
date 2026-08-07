#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Usage:
#   ./hack/update-crds.sh <DRIVER_TAG> [DRIVER_REPO_PATH]
#
#   DRIVER_TAG is the upstream secrets-store-csi-driver release tag to sync
#   from, e.g. v1.6.0. Required.
#
#   DRIVER_REPO_PATH is an optional path to a local clone of
#   github.com/kubernetes-sigs/secrets-store-csi-driver. If omitted, it
#   defaults to ../secrets-store-csi-driver relative to this repo (a sibling
#   checkout), and if that doesn't exist, falls back to fetching the raw
#   files over HTTPS from GitHub. The DRIVER_REPO env var overrides the
#   default local path.
#
#   The upstream CRDs live in manifest_staging/deploy/, which is the staging
#   area that is later promoted verbatim to deploy/ at release time, so both
#   contain identical content for a given tag.
#
# Examples:
#   ./hack/update-crds.sh v1.6.0
#   ./hack/update-crds.sh v1.6.0 /path/to/secrets-store-csi-driver
#   DRIVER_ORG=openshift ./hack/update-crds.sh v1.6.0   # fetch from a fork over HTTPS

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "Usage: $0 <DRIVER_TAG> [DRIVER_REPO_PATH]" 1>&2
	exit 1
fi

DRIVER_TAG=$1
DRIVER_REPO=${2:-${DRIVER_REPO:-"$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/secrets-store-csi-driver"}}
DRIVER_ORG=${DRIVER_ORG:-"kubernetes-sigs"}

STAGING_DIR="manifest_staging/deploy"
TARGET_DIR="config/manifests/stable"

# CRD manifests that are copied verbatim from upstream. Add new entries here
# if the driver ever ships additional CRDs.
CRD_FILES=(
	"secrets-store.csi.x-k8s.io_secretproviderclasses.yaml"
	"secrets-store.csi.x-k8s.io_secretproviderclasspodstatuses.yaml"
)

if [ ! -d "${TARGET_DIR}" ]; then
	echo "Error: ${TARGET_DIR} not found. Run this script from the repo root." 1>&2
	exit 1
fi

# Fetch into a scratch directory first, so a failure partway through never
# leaves TARGET_DIR with only some of the CRDs updated.
TMP_DIR="$(mktemp -d "${TARGET_DIR}/.update-crds.XXXXXX")"
trap 'rm -rf -- "${TMP_DIR}"' EXIT

fetch_from_local_repo() {
	local file=$1
	local ref="${DRIVER_TAG}:${STAGING_DIR}/${file}"

	if ! git -C "${DRIVER_REPO}" cat-file -e "${DRIVER_TAG}^{commit}" 2>/dev/null; then
		echo "Tag ${DRIVER_TAG} not found locally in ${DRIVER_REPO}, fetching tags..."
		git -C "${DRIVER_REPO}" fetch --tags --quiet
	fi

	git -C "${DRIVER_REPO}" cat-file -e "${DRIVER_TAG}^{commit}" 2>/dev/null || return 1
	git -C "${DRIVER_REPO}" show "${ref}" > "${TMP_DIR}/${file}" 2>/dev/null || return 1
}

fetch_from_github() {
	local file=$1
	local url="https://raw.githubusercontent.com/${DRIVER_ORG}/secrets-store-csi-driver/${DRIVER_TAG}/${STAGING_DIR}/${file}"

	echo "Downloading ${url}"
	curl --fail --silent --show-error --location "${url}" -o "${TMP_DIR}/${file}"
}

for file in "${CRD_FILES[@]}"; do
	echo "Fetching ${file} from ${DRIVER_TAG}"

	if [ -d "${DRIVER_REPO}/.git" ]; then
		if ! fetch_from_local_repo "${file}"; then
			echo "  local lookup failed, falling back to GitHub raw download"
			fetch_from_github "${file}"
		fi
	else
		fetch_from_github "${file}"
	fi
done

for file in "${CRD_FILES[@]}"; do
	mv -- "${TMP_DIR}/${file}" "${TARGET_DIR}/${file}"
done

echo
echo "Done. Review the changes with:"
echo "  git diff -- \"${TARGET_DIR}\""
echo
echo "Suggested commit message:"
echo "  update CRDs to secrets-store-csi-driver ${DRIVER_TAG}"
