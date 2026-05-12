#!/usr/bin/env bash
#
# E2E coverage lifecycle script for CI and local use.
#
# Usage:
#   hack/e2e-coverage.sh setup    Prepare the operator for coverage collection
#   hack/e2e-coverage.sh collect  Collect, convert, and optionally upload coverage data
#
# Environment variables:
#   COVERAGE_IMAGE          (setup)   Full pullspec of the coverage-instrumented image
#   CODECOV_TOKEN           (collect) Codecov upload token; skip upload if unset
#   ARTIFACT_DIR            (collect) Directory for CI artifacts; defaults to "."
set -euo pipefail

NAMESPACE="openshift-cluster-csi-drivers"
DEPLOYMENT="secrets-store-csi-driver-operator"
POD_LABEL="app=secrets-store-csi-driver-operator"
GOCOVERDIR_PATH="/tmp/e2e-cover"
CODECOV_SECRET_PATH="/var/run/secrets/codecov/CODECOV_TOKEN"

setup() {
    echo "--- E2E Coverage Setup ---"

    if [[ -z "${COVERAGE_IMAGE:-}" ]]; then
        echo "Error: COVERAGE_IMAGE env var must be set"
        exit 1
    fi
    echo "Coverage image: ${COVERAGE_IMAGE}"

    echo "Discovering CSV from deployment ownerReference..."
    local csv
    csv=$(oc get deployment "${DEPLOYMENT}" -n "${NAMESPACE}" \
        -o jsonpath='{.metadata.ownerReferences[?(@.kind=="ClusterServiceVersion")].name}')
    if [[ -z "${csv}" ]]; then
        echo "Error: no CSV found for ${DEPLOYMENT}"
        exit 1
    fi
    echo "Found CSV: ${csv}"

    echo "Patching CSV with coverage image..."
    oc patch csv "${csv}" -n "${NAMESPACE}" --type=json -p "[
        {\"op\": \"replace\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/image\", \"value\": \"${COVERAGE_IMAGE}\"}
    ]"

    local has_gocoverdir
    has_gocoverdir=$(oc get csv "${csv}" -n "${NAMESPACE}" \
        -o jsonpath='{.spec.install.spec.deployments[0].spec.template.spec.containers[0].env[?(@.name=="GOCOVERDIR")].name}' 2>/dev/null)
    if [[ -z "${has_gocoverdir}" ]]; then
        echo "Adding GOCOVERDIR env var to CSV..."
        oc patch csv "${csv}" -n "${NAMESPACE}" --type=json -p "[
            {\"op\": \"add\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-\", \"value\": {\"name\": \"GOCOVERDIR\", \"value\": \"${GOCOVERDIR_PATH}\"}}
        ]"
    else
        echo "GOCOVERDIR env var already present in CSV"
    fi

    echo "Waiting for operator rollout with coverage image..."
    sleep 5
    oc rollout status "deployment/${DEPLOYMENT}" -n "${NAMESPACE}" --timeout=180s

    echo "Verifying GOCOVERDIR is set in the running pod..."
    oc exec -n "${NAMESPACE}" "deploy/${DEPLOYMENT}" -- env | grep GOCOVERDIR || \
        echo "Warning: GOCOVERDIR not found in pod env (non-fatal)"

    echo "--- Coverage setup complete ---"
}

collect() {
    echo "--- E2E Coverage Collection ---"

    local artifact_dir="${ARTIFACT_DIR:-.}"
    local coverage_dir="${artifact_dir}/e2e-cover-data"
    local coverage_profile="${artifact_dir}/coverage-e2e.out"

    if [[ -z "${CODECOV_TOKEN:-}" ]] && [[ -f "${CODECOV_SECRET_PATH}" ]]; then
        CODECOV_TOKEN=$(cat "${CODECOV_SECRET_PATH}")
        export CODECOV_TOKEN
    fi

    local pod
    pod=$(oc get pods -n "${NAMESPACE}" -l "${POD_LABEL}" \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [[ -z "${pod}" ]]; then
        echo "Error: no operator pod found with label ${POD_LABEL}"
        echo "Coverage collection requires the operator pod to be running."
        exit 1
    fi
    echo "Found operator pod: ${pod}"

    echo "Creating coverage directory inside the pod..."
    oc exec -n "${NAMESPACE}" "${pod}" -- /bin/sh -c "mkdir -p ${GOCOVERDIR_PATH}" 2>/dev/null || true

    echo "Sending SIGUSR1 to flush coverage data..."
    oc exec -n "${NAMESPACE}" "${pod}" -- /bin/sh -c 'kill -USR1 1'
    sleep 3

    echo "Copying coverage data from the running pod..."
    mkdir -p "${coverage_dir}"
    oc cp "${NAMESPACE}/${pod}:${GOCOVERDIR_PATH}/." "${coverage_dir}"

    echo "Coverage files:"
    ls -la "${coverage_dir}/" 2>/dev/null || true

    if ls "${coverage_dir}"/covmeta.* >/dev/null 2>&1; then
        echo "Converting coverage data to Go profile format..."
        go tool covdata textfmt -i="${coverage_dir}" -o="${coverage_profile}"

        echo ""
        echo "=== E2E Coverage Summary ==="
        go tool covdata percent -i="${coverage_dir}"
        echo "============================="
        echo ""
        echo "Coverage profile: ${coverage_profile} ($(wc -l < "${coverage_profile}") lines)"

        if [[ -n "${CODECOV_TOKEN:-}" ]]; then
            echo "Uploading to Codecov..."
            local codecov_bin="${artifact_dir}/codecov"
            curl -sS -o "${codecov_bin}"              https://uploader.codecov.io/latest/linux/codecov
            curl -sS -o "${codecov_bin}.SHA256SUM"    https://uploader.codecov.io/latest/linux/codecov.SHA256SUM
            curl -sS -o "${codecov_bin}.SHA256SUM.sig" https://uploader.codecov.io/latest/linux/codecov.SHA256SUM.sig

            if command -v gpg >/dev/null 2>&1 && command -v gpgv >/dev/null 2>&1; then
                curl -sS https://keybase.io/codecovsecurity/pgp_keys.asc \
                    | gpg --no-default-keyring --keyring trustedkeys.gpg --import 2>/dev/null || true
                if gpgv "${codecov_bin}.SHA256SUM.sig" "${codecov_bin}.SHA256SUM" 2>/dev/null; then
                    echo "PGP signature verified"
                else
                    echo "Warning: PGP signature verification failed (continuing with SHA256 check)"
                fi
            fi
            cd "$(dirname "${codecov_bin}")" && sha256sum -c "$(basename "${codecov_bin}").SHA256SUM" && cd - >/dev/null
            chmod +x "${codecov_bin}"

            local -a codecov_args=(
                --file="${coverage_profile}"
                --flags=e2e
                --name="E2E Coverage"
                --verbose
            )

            local job_type="${JOB_TYPE:-local}"
            if [[ "${job_type}" == "presubmit" ]]; then
                echo "Detected presubmit (PR #${PULL_NUMBER:-unknown})"
                [[ -n "${PULL_NUMBER:-}" ]]    && codecov_args+=(--pr "${PULL_NUMBER}")
                [[ -n "${PULL_PULL_SHA:-}" ]]   && codecov_args+=(--sha "${PULL_PULL_SHA}")
                [[ -n "${PULL_BASE_REF:-}" ]]   && codecov_args+=(--branch "${PULL_BASE_REF}")
                [[ -n "${REPO_OWNER:-}" && -n "${REPO_NAME:-}" ]] && codecov_args+=(--slug "${REPO_OWNER}/${REPO_NAME}")
            elif [[ "${job_type}" == "postsubmit" ]]; then
                echo "Detected postsubmit (branch ${PULL_BASE_REF:-unknown})"
                [[ -n "${PULL_BASE_SHA:-}" ]]   && codecov_args+=(--sha "${PULL_BASE_SHA}")
                [[ -n "${PULL_BASE_REF:-}" ]]   && codecov_args+=(--branch "${PULL_BASE_REF}")
                [[ -n "${REPO_OWNER:-}" && -n "${REPO_NAME:-}" ]] && codecov_args+=(--slug "${REPO_OWNER}/${REPO_NAME}")
            else
                echo "Local run -- no Prow context, Codecov will auto-detect from git"
            fi

            "${codecov_bin}" "${codecov_args[@]}" || echo "Warning: Codecov upload failed (non-fatal)"
            rm -f "${codecov_bin}" "${codecov_bin}.SHA256SUM" "${codecov_bin}.SHA256SUM.sig"
        else
            echo "CODECOV_TOKEN not set -- skipping Codecov upload."
            echo "Coverage profile saved as artifact: ${coverage_profile}"
        fi
    else
        echo "Warning: No coverage data found in ${coverage_dir}"
        echo "The operator may not have been built with coverage instrumentation,"
        echo "or the SIGUSR1 flush may not have succeeded."
    fi

    echo "--- Coverage collection complete ---"
}

case "${1:-}" in
    setup)
        setup
        ;;
    collect)
        collect
        ;;
    *)
        echo "Usage: $0 {setup|collect}" >&2
        exit 1
        ;;
esac
