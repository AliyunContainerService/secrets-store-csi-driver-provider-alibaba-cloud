#!/bin/bash
# ============================================================================
# Test Script - 11 Test Cases
# ============================================================================
# Test Cases (ordered by auth chain priority):
#   TC-001: Pod SA RRSA Authentication
#   TC-002: Provider RRSA Authentication
#   TC-003: RAM Role Authentication (AK/SK + RoleArn)
#   TC-004: Node Publish Secret Authentication
#   TC-005: AK/SK Authentication
#   TC-006: Cross-account Authentication
#   TC-007: ECS RAM Role Authentication
#   TC-008: JMESPath JSON Parsing
#   TC-009: Secret Rotation
#   TC-010: K8s Secret Sync (secretObjects)
#   TC-011: Post-Deletion Secret Cleanup
#
# Usage:
#   1. Configure environment variables (copy .env.example to .env and fill in)
#   2. set -a && source .env && set +a
#   3. ./run-tests.sh
# ============================================================================

set -euo pipefail

# ============================================================================
# Global Variables and Configuration (~50 lines)
# ============================================================================

# Color definitions
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' CYAN='' BOLD='' NC=''
fi

# Test configuration
NAMESPACE="${NAMESPACE:-staging}"
TEST_RESULTS=()
FAILED_TESTS=()
PASSED=0
FAILED=0
SKIPPED=0
TOTAL=0
START_TIME=$(date +%s)
TEST_TIMEOUT="${TEST_TIMEOUT:-180}"
RESOURCES_CLEANED=false

# Error tracking
ERROR_STEP=""
ERROR_MESSAGE=""
CURRENT_STEP=""
_CURRENT_AUTH_MODE=""
_DIAGNOSTICS_COLLECTED=""  # Flag to prevent duplicate diagnostics collection

# Unique suffix for test resources
TEST_SUFFIX="${START_TIME}-$$"

# KMS Secret names: each defined as local variable in its respective TC function
# (inlined in replace_yaml_placeholders envsubst for YAML placeholder substitution)

# RAM resource names
KMS_POLICY_NAME="shared-kms-policy-${TEST_SUFFIX}"
# NOTE: POD_SA_ROLE_NAME moved to run_tc001_pod_sa_rrsa() as local variable
# NOTE: PROVIDER_ROLE_NAME moved to run_tc002_provider_rrsa() as local variable

# Cross-account test control
SKIP_CROSS_ACCOUNT="false"

# Auth state tracking: TC-001~TC-007 set to true after configuring auth;
# TC-008~TC-011 check this to decide whether to configure auth or reuse existing.
_AUTH_CONFIGURED=false

# Fallback user tracking for feature tests (TC-008~TC-011)
FALLBACK_USER=""
FALLBACK_AK=""

# Provider deployment control (auto-installs if not present, set SKIP_PROVIDER_DEPLOY=true to skip)
SKIP_PROVIDER_DEPLOY="${SKIP_PROVIDER_DEPLOY:-false}"

# Auto-detect Helm Chart path based on script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TESTS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="${TESTS_DIR}/test-logs-$(date +%Y%m%d-%H%M%S)"
HELM_CHART_PATH="${HELM_CHART_PATH:-${PROJECT_ROOT}/charts/csi-secrets-store-provider-alibabacloud}"

# KMS service region
KMS_REGION="${KMS_REGION:-cn-hangzhou}"

# KMS encryption configuration (for DKMS/managed KMS scenarios)
ENCRYPTION_KEY_ID="${ENCRYPTION_KEY_ID:-}"
DKMS_INSTANCE_ID="${DKMS_INSTANCE_ID:-}"

# YAML backup tracking
BACKED_UP_FILES=()

# ============================================================================
# Utility Functions (~150 lines)
# ============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓ PASS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[⚠ WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗ FAIL]${NC} $1" >&2
}

log_step() {
    echo -e "\n${CYAN}${BOLD}=== $1 ===${NC}"
}

# Generic retry wrapper for aliyun CLI commands with exponential backoff.
# Usage: retry_aliyun [max_retries] [initial_delay] <command...>
# Default: 5 retries, 3s initial delay, exponential backoff (3→6→12→24s)
# Returns 0 on success, 1 if all attempts fail.
retry_aliyun() {
    local max_retries="${1:-5}"
    local retry_delay="${2:-3}"
    shift 2
    local attempt=0
    local last_output=""
    while [ $attempt -lt $max_retries ]; do
        if last_output=$("$@" 2>&1); then
            echo "$last_output"
            return 0
        fi
        attempt=$((attempt + 1))
        if [ $attempt -lt $max_retries ]; then
            log_warning "Attempt $attempt/$max_retries failed, retrying in ${retry_delay}s... ($*)"
            sleep "$retry_delay"
            retry_delay=$((retry_delay * 2))  # exponential backoff
        fi
    done
    log_error "All $max_retries attempts failed: $*"
    if [ -n "$last_output" ]; then
        log_error "Last error: $last_output"
    fi
    return 1
}

# Generate standard trust policy JSON
# Usage: generate_trust_policy "ram" "$account_id"
#        generate_trust_policy "oidc" "$provider_arn" "$sa_namespace" "$sa_name"
generate_trust_policy() {
    local type="$1"
    if [ "$type" = "ram" ]; then
        local account_id="$2"
        cat <<EOF
{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Effect": "Allow",
      "Principal": {
        "RAM": [
          "acs:ram::${account_id}:root"
        ]
      }
    }
  ],
  "Version": "1"
}
EOF
    elif [ "$type" = "oidc" ]; then
        local provider_arn="$2"
        local sa_namespace="$3"
        local sa_name="$4"
        # Extract provider name (e.g. ack-rrsa-<cluster-id>) from ARN
        local provider_name
        provider_name=$(echo "$provider_arn" | grep -oP '[^/]+$')
        # Determine OIDC issuer URL with priority:
        # 1) OIDC_ISSUER_URL env var
        # 2) Construct directly from region and cluster-id: oidc-ack-<region>.oss-<region>.aliyuncs.com/<cluster-id>
        local oidc_issuer=""
        if [[ -n "${OIDC_ISSUER_URL:-}" ]]; then
            oidc_issuer="${OIDC_ISSUER_URL}"
            log_info "Using OIDC issuer URL from OIDC_ISSUER_URL env var: ${oidc_issuer}" >&2
        else
            oidc_issuer="https://oidc-ack-${KMS_REGION}.oss-${KMS_REGION}.aliyuncs.com/${CLUSTER_ID}"
            log_info "Constructed OIDC issuer URL from region and clusterID: ${oidc_issuer}" >&2
        fi
        local oidc_sub_condition=""
        if [[ -n "$sa_namespace" && "$sa_name" != "*" ]]; then
            oidc_sub_condition="\"oidc:sub\": [\"system:serviceaccount:${sa_namespace}:${sa_name}\"],"
        fi
        cat <<EOF
{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": {
          ${oidc_sub_condition}
          "oidc:aud": ["sts.aliyuncs.com"],
          "oidc:iss": ["${oidc_issuer}"]
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": [
          "${provider_arn}"
        ]
      }
    }
  ],
  "Version": "1"
}
EOF
    fi
}

# Error handling function
generate_error_report() {
    local report_file="${LOG_DIR}/error-report.md"
    cat > "$report_file" <<EOF
# Error Report
- **Time**: $(date '+%Y-%m-%d %H:%M:%S')
- **Step**: ${ERROR_STEP}
- **Message**: ${ERROR_MESSAGE}
- **Log**: ${LOG_DIR}/test-run.log
EOF
    log_info "Error report generated: $report_file"
}

handle_error() {
    local exit_code=$?
    ERROR_STEP="${CURRENT_STEP:-unknown}"
    ERROR_MESSAGE="Script exited with code $exit_code"
    log_error "Error detected in step: $ERROR_STEP - $ERROR_MESSAGE"
    collect_diagnostics "error-exit"
    _DIAGNOSTICS_COLLECTED=true
    generate_error_report
    exit "$exit_code"
}

trap handle_error ERR

record_result() {
    local test_name="$1"
    local status="$2"
    local message="$3"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    TEST_RESULTS+=("${timestamp}|${test_name}|${status}|${message}")
    TOTAL=$((TOTAL + 1))
    
    case "$status" in
        PASS) PASSED=$((PASSED + 1)) ;;
        FAIL) FAILED=$((FAILED + 1)); FAILED_TESTS+=("$test_name"); collect_diagnostics "test-failure-${test_name}" ;;
        SKIP) SKIPPED=$((SKIPPED + 1)) ;;
    esac
}

should_skip_test() {
    local test_case="$1"
    if [ -n "${SKIP_TESTS:-}" ]; then
        IFS=',' read -ra SKIP_ARRAY <<< "$SKIP_TESTS"
        for skip in "${SKIP_ARRAY[@]}"; do
            if [ "$test_case" = "$skip" ]; then
                return 0
            fi
        done
    fi
    return 1
}

wait_for_pod_ready() {
    local pod_name="$1"
    local timeout="${2:-$TEST_TIMEOUT}"
    local elapsed=0
    
    log_info "Waiting for Pod $pod_name to be ready (timeout: ${timeout}s)..."
    
    while [ $elapsed -lt $timeout ]; do
        local status=$(kubectl get pod "$pod_name" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
        
        if [ $((elapsed % 15)) -eq 0 ]; then
            log_info "Pod $pod_name status: $status (waited ${elapsed}s/${timeout}s)"
        fi
        
        if [ "$status" = "Running" ]; then
            log_success "Pod $pod_name is ready"
            return 0
        elif [ "$status" = "Failed" ]; then
            log_error "Pod $pod_name failed to start"
            kubectl describe pod "$pod_name" -n "$NAMESPACE" --tail=30
            return 1
        fi
        
        sleep 5
        elapsed=$((elapsed + 5))
    done
    
    log_error "Pod $pod_name timed out waiting for ready (current status: $status)"
    return 1
}

verify_mount() {
    local pod_name="$1"
    local secret_name="$2"
    local expected_value="$3"
    local secret_path="/mnt/secrets-store/${secret_name}"
    
    # Wait for CSI mount
    log_info "Waiting for CSI volume mount to complete..."
    local elapsed=0
    while [ $elapsed -lt 60 ]; do
        if kubectl exec "$pod_name" -n "$NAMESPACE" -- test -f "$secret_path" >/dev/null 2>&1; then
            log_success "CSI volume mounted"
            break
        fi
        sleep 5
        elapsed=$((elapsed + 5))
    done
    
    if [ $elapsed -ge 60 ]; then
        log_error "CSI volume mount timed out"
        kubectl logs -n kube-system -l app=csi-secrets-store-provider-alibabacloud --tail=30 || true
        return 1
    fi
    
    # Verify Secret content
    local actual_value
    if ! actual_value=$(kubectl exec "$pod_name" -n "$NAMESPACE" -- cat "$secret_path" 2>&1); then
        log_error "Secret read failed: $actual_value"
        return 1
    fi
    
    if [ "$actual_value" != "$expected_value" ]; then
        log_error "Secret content mismatch: expected '$expected_value', actual '$actual_value'"
        return 1
    fi
    
    log_success "Secret content verified: '$actual_value'"
    return 0
}

cleanup_test_resources() {
    local test_prefix="$1"
    log_info "Cleaning up test resources: $test_prefix"
    kubectl delete pod -l test-case="$test_prefix" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    kubectl delete secretproviderclass -l test-case="$test_prefix" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    sleep 3
}

# ============================================================================
# Cross-Account Resource Management
# ============================================================================

# Create resources in target account for cross-account testing
# setup_target_account_resources() removed: cross-account resources are now created
# inside run_tc006_cross_account() on demand.

# Delete a KMS Secret (idempotent, with retry)
# Usage: delete_kms_secret "$secret_name" ["$target_ak"] ["$target_sk"]
delete_kms_secret() {
    local secret_name="$1"
    local target_ak="${2:-}"
    local target_sk="${3:-}"
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    local retry=0 max_retries=5
    local last_error=""
    while [ $retry -lt $max_retries ]; do
        local delete_output
        if delete_output=$(aliyun kms DeleteSecret --SecretName "$secret_name" --region "${KMS_REGION}" --ForceDeleteWithoutRecovery true "${extra_args[@]}" 2>&1); then
            return 0
        fi
        last_error="$delete_output"
        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            sleep $((retry * 2))
        fi
    done
    # 失败时输出详细错误
    log_warning "Failed to delete KMS Secret after $max_retries retries: $secret_name"
    if [ -n "$last_error" ]; then
        log_warning "  Error: $last_error"
    fi
    return 0  # 幂等操作,失败不阻塞
}

# Delete a RAM Policy with automatic conflict resolution and retry
# Usage: delete_ram_policy "$policy_name" ["$target_ak"] ["$target_sk"]
delete_ram_policy() {
    local policy_name="$1"
    local target_ak="${2:-}"
    local target_sk="${3:-}"
    local max_retries=5
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    # Try direct deletion first
    local delete_output
    if delete_output=$(aliyun ram DeletePolicy --PolicyName "$policy_name" "${extra_args[@]}" 2>&1); then
        return 0
    fi

    # Handle DeleteConflict: detach from all entities with retry
    local retry=0
    local last_error="$delete_output"
    while [ $retry -lt $max_retries ]; do
        local entities
        if entities=$(aliyun ram ListEntitiesForPolicy --PolicyName "$policy_name" --PolicyType Custom "${extra_args[@]}" 2>&1); then
            # Extract role names and detach (avoid subshell by using process substitution)
            while IFS= read -r role_name; do
                [ -z "$role_name" ] && continue
                retry_aliyun 2 2 aliyun ram DetachPolicyFromRole --PolicyType Custom --PolicyName "$policy_name" --RoleName "$role_name" "${extra_args[@]}" || true
            done < <(echo "$entities" | jq -r '.Roles.Role[]?.RoleName' 2>/dev/null)

            # Extract user names and detach
            while IFS= read -r user_name; do
                [ -z "$user_name" ] && continue
                retry_aliyun 2 2 aliyun ram DetachPolicyFromUser --PolicyType Custom --PolicyName "$policy_name" --UserName "$user_name" "${extra_args[@]}" || true
            done < <(echo "$entities" | jq -r '.Users.User[]?.UserName' 2>/dev/null)
        else
            log_warning "Failed to list entities for policy $policy_name (attempt $((retry+1))/$max_retries): $entities"
        fi

        sleep 5  # Wait for detachment to propagate (5s for RAM API eventual consistency)
        
        # Try deletion again
        if delete_output=$(aliyun ram DeletePolicy --PolicyName "$policy_name" "${extra_args[@]}" 2>&1); then
            return 0
        fi
        last_error="$delete_output"

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            sleep $((retry * 2))
        fi
    done

    # 失败时输出详细错误
    log_warning "Failed to delete RAM Policy after $max_retries retries: $policy_name"
    if [ -n "$last_error" ]; then
        log_warning "  Error: $last_error"
        log_warning "  Troubleshooting:"
        log_warning "    1. Check attached roles: aliyun ram ListEntitiesForPolicy --PolicyName $policy_name --PolicyType Custom"
        log_warning "    2. Manually detach: aliyun ram DetachPolicyFromRole --PolicyName $policy_name --RoleName <role>"
        log_warning "    3. Force delete (if supported): aliyun ram DeletePolicy --PolicyName $policy_name --CascadingDelete true"
    fi
    return 1
}

# cleanup_target_account_resources() removed: TC-006 now cleans up its own target account resources.

ensure_rrsa_enabled() {
    local cluster_id="${CLUSTER_ID:-}"
    if [[ -z "${cluster_id}" ]]; then
        log_warning "CLUSTER_ID not set, skipping RRSA enable check"
        return 1
    fi

    log_info "Ensuring RRSA is enabled on cluster: ${cluster_id}"

    # Check if aliyun CLI is available
    if ! command -v aliyun &>/dev/null; then
        log_warning "aliyun CLI not found in PATH, skipping RRSA check"
        log_warning "Please ensure RRSA is enabled manually on the cluster"
        return 1
    fi

    # Step 1: Query cluster detail to check RRSA status (with retry for DNS resilience)
    log_info "Checking RRSA status via aliyun CLI ..."
    local cluster_detail
    local rrsa_query_max_retries=5 rrsa_query_delay=5
    for ((rrsa_qry=1; rrsa_qry<=rrsa_query_max_retries; rrsa_qry++)); do
        if cluster_detail=$(aliyun cs GET "/clusters/${cluster_id}" --region "${KMS_REGION}" --header "Content-Type=application/json" 2>&1); then
            break
        fi
        if [[ $rrsa_qry -lt $rrsa_query_max_retries ]]; then
            log_warning "Failed to describe cluster (attempt $rrsa_qry/$rrsa_query_max_retries), retrying in ${rrsa_query_delay}s..."
            sleep "$rrsa_query_delay"
        else
            log_warning "Failed to describe cluster after $rrsa_query_max_retries attempts, RRSA status unknown"
            return 1
        fi
    done

    # Parse rrsa_config.enabled from JSON response
    local rrsa_enabled
    rrsa_enabled=$(echo "${cluster_detail}" | jq -r '.rrsa_config.enabled // false' 2>/dev/null || echo "false")

    if [[ "${rrsa_enabled}" == "true" ]]; then
        log_success "RRSA is already enabled on the cluster"
        return 0
    fi

    # Step 2: Enable RRSA via aliyun CLI (with retry)
    log_info "RRSA is not enabled, enabling via aliyun CLI ..."
    local enable_retry=0 enable_max=5 enable_delay=5
    local enable_output
    while [ $enable_retry -lt $enable_max ]; do
        if enable_output=$(aliyun cs PUT "/api/v2/clusters/${cluster_id}" --region "${KMS_REGION}" --header "Content-Type=application/json" --body '{"enable_rrsa":true}' 2>&1); then
            log_success "RRSA enabled"
            break
        fi
        enable_retry=$((enable_retry + 1))
        if [ $enable_retry -lt $enable_max ]; then
            log_warning "Failed to enable RRSA (attempt $enable_retry/$enable_max), retrying in ${enable_delay}s..."
            sleep "$enable_delay"
            enable_delay=$((enable_delay * 2))
        else
            log_error "Failed to enable RRSA after $enable_max attempts: ${enable_output}"
            return 1
        fi
    done

    # Step 3: Wait for RRSA to take effect by polling
    log_info "Waiting for RRSA to take effect..."
    local max_retries=12
    local retry=0
    while [[ $retry -lt $max_retries ]]; do
        sleep 30
        local status
        status=$(aliyun cs GET "/clusters/${cluster_id}" --region "${KMS_REGION}" --header "Content-Type=application/json" 2>/dev/null | jq -r '.rrsa_config.enabled // false' || echo "false")
        if [[ "$status" == "true" ]]; then
            log_success "RRSA is now enabled on the cluster"
            return 0
        fi
        retry=$((retry + 1))
        log_info "RRSA not yet enabled, retrying... (${retry}/${max_retries})"
    done
    log_warning "RRSA did not become enabled within 120s"
    return 1
}

deploy_provider() {
    if [ "${SKIP_PROVIDER_DEPLOY}" = "true" ]; then
        log_info "Skipping Provider deployment (SKIP_PROVIDER_DEPLOY=true)"
        return 0
    fi

    # Check if already installed
    if kubectl get daemonset csi-secrets-store-provider-alibabacloud -n kube-system &> /dev/null; then
        log_info "Provider already installed, skipping deployment"
        return 0
    fi

    if ! command -v helm &> /dev/null; then
        log_error "Missing dependency: helm"
        exit 1
    fi

    if [ ! -d "$HELM_CHART_PATH" ]; then
        log_error "Helm Chart path does not exist: $HELM_CHART_PATH"
        exit 1
    fi

    # Check if release with same name already exists
    local existing_release
    existing_release=$(helm list -n kube-system -q 2>/dev/null | grep -i csi-secrets-store-provider || echo "")
    if [ -n "$existing_release" ]; then
        log_warning "Detected existing Provider release: $existing_release, skipping deployment"
        return 0
    fi

    log_info "Installing Provider Chart: $HELM_CHART_PATH"
    if helm install csi-secrets-store-provider-alibabacloud \
        "$HELM_CHART_PATH" \
        --namespace kube-system \
        --wait \
        --timeout 5m \
        --set secrets-store-csi-driver.enableSecretRotation=true \
        --set secrets-store-csi-driver.rotationPollInterval=10s \
        --set secrets-store-csi-driver.syncSecret.enabled=true; then
        log_success "Provider Helm deployment successful"
    else
        log_error "Provider Helm deployment failed"
        exit 1
    fi

    # Wait for Provider DaemonSet to be ready
    log_info "Waiting for Provider DaemonSet to be ready..."
    local elapsed=0
    while [ $elapsed -lt 120 ]; do
        if kubectl get daemonset csi-secrets-store-provider-alibabacloud -n kube-system &> /dev/null; then
            local desired ready
            desired=$(kubectl get daemonset csi-secrets-store-provider-alibabacloud -n kube-system -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
            ready=$(kubectl get daemonset csi-secrets-store-provider-alibabacloud -n kube-system -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
            if [ "$desired" -gt 0 ] && [ "$ready" -ge "$desired" ]; then
                log_success "Provider DaemonSet ready ($ready/$desired)"
                return 0
            fi
            log_info "Provider DaemonSet progressing ($ready/$desired, waited ${elapsed}s)"
        fi
        sleep 10
        elapsed=$((elapsed + 10))
    done

    log_error "Provider DaemonSet not ready (timeout 120s)"
    return 1
}

# ============================================================================
# Cloud Resource Creation (~200 lines)
# ============================================================================

validate_env() {   
    if [ -z "${SOURCE_ACCOUNT_ID:-}" ]; then
        log_error "Missing required environment variable: SOURCE_ACCOUNT_ID"
        exit 1
    fi
    
    if [ -z "${CLUSTER_ID:-}" ]; then
        log_error "Missing required environment variable: CLUSTER_ID"
        exit 1
    fi
    
    log_success "SOURCE_ACCOUNT_ID=$SOURCE_ACCOUNT_ID"
    log_success "CLUSTER_ID=$CLUSTER_ID"
    
    # Auto-construct OIDC_PROVIDER_ARN
    if [ -z "${OIDC_PROVIDER_ARN:-}" ]; then
        export OIDC_PROVIDER_ARN="acs:ram::${SOURCE_ACCOUNT_ID}:oidc-provider/ack-rrsa-${CLUSTER_ID}"
        log_info "Auto-constructed OIDC_PROVIDER_ARN=$OIDC_PROVIDER_ARN"
    fi
    
    # Check dependency tools
    for dep in kubectl aliyun; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Missing dependency: $dep"
            exit 1
        fi
    done
    log_success "Dependency check passed"
    
    # Validate cluster connection
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Unable to connect to Kubernetes cluster"
        exit 1
    fi
    log_success "Cluster connection OK"
    
    # Create test namespace
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 || true
    log_success "Namespace $NAMESPACE ready"
}

# Usage: create_kms_secret "$name" "$data" ["$key_id"] ["$dkms_id"] ["$region"] ["$target_ak"] ["$target_sk"]
# Optional params fall back to global variables when not provided (backward compatible)
create_kms_secret() {
    local secret_name="$1"
    local secret_value="$2"
    local key_id="${3:-${ENCRYPTION_KEY_ID:-}}"
    local dkms_id="${4:-${DKMS_INSTANCE_ID:-}}"
    local region="${5:-${KMS_REGION:-cn-hangzhou}}"
    local target_ak="${6:-}"
    local target_sk="${7:-}"

    # Support cross-account credential passthrough
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    log_info "Creating KMS Secret: $secret_name (region: $region)"

    # Check if already exists (with retry for DNS resilience)
    local check_retry=0 check_max=5
    while [ $check_retry -lt $check_max ]; do
        if aliyun kms DescribeSecret --SecretName "$secret_name" --region "$region" "${extra_args[@]}" &> /dev/null; then
            log_warning "KMS Secret $secret_name already exists, skipping creation"
            return 0
        fi
        check_retry=$((check_retry + 1))
        if [ $check_retry -lt $check_max ]; then
            sleep $((check_retry * 2))
        fi
    done

    local retry=0 max_retries=5 delay=3
    while [ $retry -lt $max_retries ]; do
        # Build CreateSecret command args
        local cmd_args=(
            kms CreateSecret
            --SecretName "$secret_name"
            --SecretData "$secret_value"
            --VersionId "v1"
            --RegionId "$region"
        )
        # Optional: DKMS encryption key support
        if [ -n "$key_id" ]; then
            cmd_args+=(--EncryptionKeyId "$key_id")
        fi
        if [ -n "$dkms_id" ]; then
            cmd_args+=(--DKMSInstanceId "$dkms_id")
        fi

        if aliyun "${cmd_args[@]}" "${extra_args[@]}" 2>&1; then
            # Verify creation with DescribeSecret (with its own retry, non-blocking on DNS failure)
            local verify_retry=0 verify_max=5 verify_delay=2
            while [ $verify_retry -lt $verify_max ]; do
                if aliyun kms DescribeSecret --SecretName "$secret_name" --region "$region" "${extra_args[@]}" &> /dev/null; then
                    log_success "KMS Secret created and verified: $secret_name"
                    return 0
                fi
                verify_retry=$((verify_retry + 1))
                if [ $verify_retry -lt $verify_max ]; then
                    log_warning "DescribeSecret verification attempt $verify_retry/$verify_max failed, retrying in ${verify_delay}s..."
                    sleep "$verify_delay"
                    verify_delay=$((verify_delay * 2))
                fi
            done
            # CreateSecret succeeded but DescribeSecret verification failed - likely DNS timeout, treat as success
            log_warning "KMS Secret created but DescribeSecret verification failed (likely DNS timeout): $secret_name"
            log_warning "Treating as success since CreateSecret API returned OK"
            return 0
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_warning "CreateSecret attempt $retry/$max_retries failed, retrying in ${delay}s..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done
    log_error "KMS Secret creation failed after $max_retries attempts: $secret_name"
    return 1
}

# Usage: create_ram_policy "$name" ["$policy_doc"] ["$description"] ["$target_ak"] ["$target_sk"]
# When policy_doc is not provided, loads the default KMS permission policy
create_ram_policy() {
    local name="$1"
    local policy_doc="${2:-}"
    local description="${3:-CSI Provider Test Policy}"
    local target_ak="${4:-}"
    local target_sk="${5:-}"

    # Support cross-account credential passthrough
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    if [ -z "$policy_doc" ]; then
        policy_doc='{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["kms:GetSecretValue", "kms:Decrypt"],
      "Resource": "*"
    }
  ]
}'
    fi

    log_info "Creating RAM Policy: $name"

    # Check if already exists (with retry for DNS resilience)
    local check_retry=0 check_max=5
    while [ $check_retry -lt $check_max ]; do
        if aliyun ram GetPolicy --PolicyName "$name" --PolicyType Custom "${extra_args[@]}" &> /dev/null; then
            log_warning "RAM Policy $name already exists"
            return 0
        fi
        check_retry=$((check_retry + 1))
        if [ $check_retry -lt $check_max ]; then
            sleep $((check_retry * 2))
        fi
    done

    local retry=0 max_retries=5 delay=3
    while [ $retry -lt $max_retries ]; do
        local create_output
        if create_output=$(aliyun ram CreatePolicy \
            --PolicyName "$name" \
            --PolicyDocument "$policy_doc" \
            --Description "$description" "${extra_args[@]}" 2>&1); then
            # Verify creation with GetPolicy (non-blocking on DNS failure)
            local verify_retry=0 verify_max=5 verify_delay=2
            while [ $verify_retry -lt $verify_max ]; do
                if aliyun ram GetPolicy --PolicyName "$name" --PolicyType Custom "${extra_args[@]}" &> /dev/null; then
                    log_success "RAM Policy created and verified: $name"
                    return 0
                fi
                verify_retry=$((verify_retry + 1))
                if [ $verify_retry -lt $verify_max ]; then
                    log_warning "GetPolicy verification attempt $verify_retry/$verify_max failed, retrying in ${verify_delay}s..."
                    sleep "$verify_delay"
                    verify_delay=$((verify_delay * 2))
                fi
            done
            # CreatePolicy succeeded but GetPolicy verification failed - likely DNS timeout, treat as success
            log_warning "RAM Policy created but GetPolicy verification failed (likely DNS timeout): $name"
            log_warning "Treating as success since CreatePolicy API returned OK"
            return 0
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_warning "CreatePolicy attempt $retry/$max_retries failed, retrying in ${delay}s..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done
    log_error "RAM Policy creation failed after $max_retries attempts: $name"
    return 1
}

# Verify that a policy is attached to a role, re-attach if missing
# Usage: verify_role_policy_attachment "$role_name" "$policy_name" ["$target_ak"] ["$target_sk"]
verify_role_policy_attachment() {
    local role_name="$1"
    local policy_name="$2"
    local target_ak="${3:-}"
    local target_sk="${4:-}"
    local max_retries=5
    local retry=0

    # Support cross-account credential passthrough
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    while [ $retry -lt $max_retries ]; do
        local policies
        if policies=$(aliyun ram ListPoliciesForRole --RoleName "$role_name" "${extra_args[@]}" 2>&1); then
            if echo "$policies" | grep -q "$policy_name"; then
                log_success "Verified policy $policy_name attached to role $role_name"
                return 0
            fi
            log_warning "Policy $policy_name not found on role $role_name, re-attaching (attempt $((retry+1))/$max_retries)..."
            if aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName "$policy_name" --RoleName "$role_name" "${extra_args[@]}" 2>&1; then
                log_success "Re-attached policy $policy_name to role $role_name"
            else
                log_warning "Failed to re-attach policy $policy_name to role $role_name"
            fi
        else
            log_warning "Failed to list policies for role $role_name: $policies"
        fi
        retry=$((retry + 1))
        [ $retry -lt $max_retries ] && sleep 2
    done

    log_error "Policy $policy_name could not be verified on role $role_name after $max_retries attempts"
    return 1
}

# Verify that a policy is attached to a user, re-attach if missing
# Usage: verify_user_policy_attachment "$user_name" "$policy_name" ["$target_ak"] ["$target_sk"]
verify_user_policy_attachment() {
    local user_name="$1"
    local policy_name="$2"
    local target_ak="${3:-}"
    local target_sk="${4:-}"
    local max_retries=5
    local retry=0
    local delay=5

    # Support cross-account credential passthrough
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    while [ $retry -lt $max_retries ]; do
        local policies
        if policies=$(aliyun ram ListPoliciesForUser --UserName "$user_name" "${extra_args[@]}" 2>&1); then
            if echo "$policies" | grep -q "$policy_name"; then
                log_success "Verified policy $policy_name attached to user $user_name"
                return 0
            fi
            log_warning "Policy $policy_name not found on user $user_name, re-attaching (attempt $((retry+1))/$max_retries)..."
            if aliyun ram AttachPolicyToUser --PolicyType Custom --PolicyName "$policy_name" --UserName "$user_name" "${extra_args[@]}" 2>&1; then
                log_success "Re-attached policy $policy_name to user $user_name"
            else
                log_warning "Failed to re-attach policy $policy_name to user $user_name"
            fi
        else
            log_warning "Failed to list policies for user $user_name: $policies"
        fi
        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_info "Waiting ${delay}s before retry..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done

    log_error "Policy $policy_name could not be verified on user $user_name after $max_retries attempts"
    return 1
}

# Usage: create_ram_role "$name" ["$trust_policy"] ["$description"] ["$policy_name"] ["$target_ak"] ["$target_sk"]
# When trust_policy is not provided, uses default RAM trust policy for SOURCE_ACCOUNT_ID
# When policy_name is provided, auto-attaches that policy to the role after creation
create_ram_role() {
    local name="$1"
    local trust_policy="${2:-}"
    local description="${3:-}"
    local policy_name="${4:-}"
    local target_ak="${5:-}"
    local target_sk="${6:-}"

    # Support cross-account credential passthrough
    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    if [ -z "$trust_policy" ]; then
        trust_policy=$(generate_trust_policy "ram" "$SOURCE_ACCOUNT_ID")
    fi

    log_info "Creating RAM Role: $name"

    # Check if already exists (with retry for DNS resilience)
    local check_retry=0 check_max=5
    while [ $check_retry -lt $check_max ]; do
        if aliyun ram GetRole --RoleName "$name" "${extra_args[@]}" &> /dev/null; then
            log_warning "RAM Role $name already exists"
            # Still attach policy if requested
            if [ -n "$policy_name" ]; then
                log_info "Attaching policy $policy_name to existing role $name..."
                local attach_retry=0 attach_max=5 attach_delay=3
                while [ $attach_retry -lt $attach_max ]; do
                    if aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName "$policy_name" --RoleName "$name" "${extra_args[@]}" 2>&1; then
                        log_success "Policy $policy_name attached to role $name"
                        break
                    fi
                    attach_retry=$((attach_retry + 1))
                    if [ $attach_retry -lt $attach_max ]; then
                        log_warning "AttachPolicyToRole attempt $attach_retry/$attach_max failed, retrying in ${attach_delay}s..."
                        sleep "$attach_delay"
                        attach_delay=$((attach_delay * 2))
                    else
                        log_error "Failed to attach policy $policy_name to role $name after $attach_max attempts"
                        return 1
                    fi
                done
            fi
            return 0
        fi
        check_retry=$((check_retry + 1))
        if [ $check_retry -lt $check_max ]; then
            sleep $((check_retry * 2))
        fi
    done

    local retry=0 max_retries=5 delay=3
    while [ $retry -lt $max_retries ]; do
        if aliyun ram CreateRole \
            --RoleName "$name" \
            --AssumeRolePolicyDocument "$trust_policy" \
            --Description "$description" "${extra_args[@]}" 2>&1; then
            # Verify creation with GetRole (non-blocking on DNS failure)
            local verify_retry=0 verify_max=5 verify_delay=2
            local role_verified=false
            while [ $verify_retry -lt $verify_max ]; do
                if aliyun ram GetRole --RoleName "$name" "${extra_args[@]}" &> /dev/null; then
                    role_verified=true
                    break
                fi
                verify_retry=$((verify_retry + 1))
                if [ $verify_retry -lt $verify_max ]; then
                    log_warning "GetRole verification attempt $verify_retry/$verify_max failed, retrying in ${verify_delay}s..."
                    sleep "$verify_delay"
                    verify_delay=$((verify_delay * 2))
                fi
            done
            if [ "$role_verified" = false ]; then
                log_warning "RAM Role created but GetRole verification failed (likely DNS timeout): $name"
                log_warning "Treating as success since CreateRole API returned OK"
            else
                log_success "RAM Role created and verified: $name"
            fi
            # Attach policy if requested
            if [ -n "$policy_name" ]; then
                log_info "Attaching policy $policy_name to role $name..."
                local attach_retry=0 attach_max=5 attach_delay=3
                while [ $attach_retry -lt $attach_max ]; do
                    if aliyun ram AttachPolicyToRole --PolicyType Custom --PolicyName "$policy_name" --RoleName "$name" "${extra_args[@]}" 2>&1; then
                        log_success "Policy $policy_name attached to role $name"
                        break
                    fi
                    attach_retry=$((attach_retry + 1))
                    if [ $attach_retry -lt $attach_max ]; then
                        log_warning "AttachPolicyToRole attempt $attach_retry/$attach_max failed, retrying in ${attach_delay}s..."
                        sleep "$attach_delay"
                        attach_delay=$((attach_delay * 2))
                    else
                        log_error "Failed to attach policy $policy_name to role $name after $attach_max attempts"
                        return 1
                    fi
                done
            fi
            return 0
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_warning "CreateRole attempt $retry/$max_retries failed, retrying in ${delay}s..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done
    log_error "RAM Role creation failed after $max_retries attempts: $name"
    return 1
}

# Create a test RAM User with AccessKey and attach a policy
# Usage: create_test_ram_user "$user_name" "$policy_name"
# Sets global variables: _CREATED_USER_AK, _CREATED_USER_SK
# Returns: 0 on success, 1 on failure
create_test_ram_user() {
    local user_name="$1"
    local policy_name="$2"
    local target_ak="${3:-}"
    local target_sk="${4:-}"

    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    log_info "Creating test RAM User: $user_name"

    # 1. Create User (idempotent, with retry for transient failures)
    if ! aliyun ram GetUser --UserName "$user_name" "${extra_args[@]}" &>/dev/null; then
        local user_retry=0 user_max_retries=5 user_delay=3
        while [ $user_retry -lt $user_max_retries ]; do
            if aliyun ram CreateUser --UserName "$user_name" "${extra_args[@]}" 2>&1; then
                break
            fi
            user_retry=$((user_retry + 1))
            if [ $user_retry -lt $user_max_retries ]; then
                log_warning "CreateUser attempt $user_retry/$user_max_retries failed, retrying in ${user_delay}s..."
                sleep "$user_delay"
                user_delay=$((user_delay * 2))
            else
                log_error "Failed to create RAM User: $user_name after $user_max_retries attempts"
                return 1
            fi
        done
    else
        log_warning "RAM User $user_name already exists"
    fi

    # 2. Create AccessKey (with retry)
    local ak_result
    local max_retries=5 retry_delay=3
    for ((retry=1; retry<=max_retries; retry++)); do
        ak_result=$(aliyun ram CreateAccessKey --UserName "$user_name" "${extra_args[@]}" 2>&1) && break
        if [ $retry -lt $max_retries ]; then
            log_warning "CreateAccessKey failed (attempt $retry/$max_retries), retrying in ${retry_delay}s..."
            sleep $retry_delay
            retry_delay=$((retry_delay * 2))
        else
            log_error "Failed to create AccessKey for $user_name after $max_retries attempts: $ak_result"
            return 1
        fi
    done
    _CREATED_USER_AK=$(echo "$ak_result" | jq -r '.AccessKey.AccessKeyId')
    _CREATED_USER_SK=$(echo "$ak_result" | jq -r '.AccessKey.AccessKeySecret')

    # 3. Attach policy (with retry, hard failure)
    if [ -n "$policy_name" ]; then
        max_retries=3 retry_delay=3
        for ((retry=1; retry<=max_retries; retry++)); do
            if aliyun ram AttachPolicyToUser --PolicyType Custom --PolicyName "$policy_name" --UserName "$user_name" "${extra_args[@]}" 2>&1; then
                break
            fi
            if [ $retry -lt $max_retries ]; then
                log_warning "AttachPolicyToUser failed (attempt $retry/$max_retries), retrying in ${retry_delay}s..."
                sleep $retry_delay
                retry_delay=$((retry_delay * 2))
            else
                log_error "Failed to attach policy $policy_name to user $user_name after $max_retries attempts"
                return 1
            fi
        done
    fi

    log_success "Test RAM User created: $user_name (AK: ${_CREATED_USER_AK:0:8}...)"
    return 0
}

# Cleanup a test RAM User with retry and consistency wait
# Usage: cleanup_test_ram_user "$user_name" "$access_key_id"
cleanup_test_ram_user() {
    local user_name="$1"
    local ak="${2:-}"
    local target_ak="${3:-}"
    local target_sk="${4:-}"
    local max_retries=3

    local -a extra_args=()
    if [[ -n "$target_ak" && -n "$target_sk" ]]; then
        extra_args=(--access-key-id "$target_ak" --access-key-secret "$target_sk")
    fi

    log_info "Cleaning up test RAM User: $user_name"

    # 1. Delete known AccessKey with retry
    if [ -n "$ak" ]; then
        local retry=0
        local ak_error=""
        while [ $retry -lt $max_retries ]; do
            local delete_ak_output
            if delete_ak_output=$(aliyun ram DeleteAccessKey --UserName "$user_name" --UserAccessKeyId "$ak" "${extra_args[@]}" 2>&1); then
                break
            fi
            ak_error="$delete_ak_output"
            retry=$((retry + 1))
            if [ $retry -lt $max_retries ]; then
                sleep 2
            fi
        done
        # 只在失败时输出详细错误
        if [ $retry -ge $max_retries ] && [ -n "$ak_error" ]; then
            log_warning "Failed to delete AccessKey for user $user_name after $max_retries retries: $ak_error"
        fi
    fi

    # 2. Detach all policies with retry and DNS-safe error handling
    local retry=0
    local detach_error=""
    while [ $retry -lt $max_retries ]; do
        local policy_count
        local list_output
        if ! list_output=$(aliyun ram ListPoliciesForUser --UserName "$user_name" "${extra_args[@]}" 2>&1); then
            log_warning "Failed to list policies for user $user_name (attempt $((retry+1))/$max_retries): $list_output"
            retry=$((retry + 1))
            [ $retry -lt $max_retries ] && sleep $((retry * 2))
            continue
        fi
        policy_count=$(echo "$list_output" | jq '.Policies.Policy | length' 2>/dev/null || echo "0")

        if [ "$policy_count" = "0" ] || [ -z "$policy_count" ]; then
            log_info "All policies detached from user $user_name"
            break
        fi

        log_info "Found $policy_count policies attached to user $user_name, detaching..."

        # Detach all policies with individual retry (avoid subshell by using process substitution)
        detach_error=""
        local detach_failed=false
        while IFS=' ' read -r ptype pname; do
            [ -z "$pname" ] && continue
            # Retry individual policy detachment
            local pol_retry=0 pol_max=3 pol_delay=2
            while [ $pol_retry -lt $pol_max ]; do
                local detach_output
                if detach_output=$(aliyun ram DetachPolicyFromUser --PolicyType "$ptype" --PolicyName "$pname" --UserName "$user_name" "${extra_args[@]}" 2>&1); then
                    log_info "Detached policy $pname from user $user_name"
                    break
                fi
                detach_error="$detach_output"
                pol_retry=$((pol_retry + 1))
                if [ $pol_retry -lt $pol_max ]; then
                    log_warning "Failed to detach policy $pname (attempt $pol_retry/$pol_max), retrying in ${pol_delay}s..."
                    sleep "$pol_delay"
                    pol_delay=$((pol_delay * 2))
                else
                    detach_failed=true
                    log_warning "Failed to detach policy $pname from user $user_name after $pol_max attempts: $detach_error"
                fi
            done
        done < <(echo "$list_output" | jq -r '.Policies.Policy[] | "\(.PolicyType) \(.PolicyName)"' 2>/dev/null)

        # Wait for detachment to propagate (5s for RAM API eventual consistency)
        sleep 5
        retry=$((retry + 1))
    done
    # 只在失败时输出详细错误
    if [ $retry -ge $max_retries ] && [ -n "$detach_error" ]; then
        log_warning "Failed to detach policies from user $user_name after $max_retries retries: $detach_error"
    fi

    # 3. Delete User with retry and DeleteConflict auto-recovery
    retry=0
    local user_error=""
    while [ $retry -lt $max_retries ]; do
        local delete_user_output
        if delete_user_output=$(aliyun ram DeleteUser --UserName "$user_name" "${extra_args[@]}" 2>&1); then
            log_success "Cleaned up test RAM User: $user_name"
            return 0
        fi
        user_error="$delete_user_output"

        # Handle DeleteConflict: re-list and detach remaining policies + delete residual AccessKeys
        if echo "$delete_user_output" | grep -q "DeleteConflict"; then
            log_warning "DeleteConflict for user $user_name, running recovery..."

            # Recovery: detach residual policies
            local recover_output
            if recover_output=$(aliyun ram ListPoliciesForUser --UserName "$user_name" "${extra_args[@]}" 2>&1); then
                while IFS=' ' read -r ptype pname; do
                    [ -z "$pname" ] && continue
                    aliyun ram DetachPolicyFromUser --PolicyType "$ptype" --PolicyName "$pname" --UserName "$user_name" "${extra_args[@]}" 2>&1 || true
                    log_info "Recovery: detached residual policy $pname from user $user_name"
                done < <(echo "$recover_output" | jq -r '.Policies.Policy[] | "\(.PolicyType) \(.PolicyName)"' 2>/dev/null)
            else
                log_warning "Recovery: failed to list policies for user $user_name: $recover_output"
            fi

            # Recovery: delete residual AccessKeys
            local ak_list_output
            if ak_list_output=$(aliyun ram ListAccessKeys --UserName "$user_name" "${extra_args[@]}" 2>&1); then
                while IFS= read -r ak_id; do
                    [ -z "$ak_id" ] && continue
                    aliyun ram DeleteAccessKey --UserName "$user_name" --UserAccessKeyId "$ak_id" "${extra_args[@]}" 2>&1 || true
                    log_info "Recovery: deleted residual AccessKey $ak_id for user $user_name"
                done < <(echo "$ak_list_output" | jq -r '.AccessKeys.AccessKey[]?.AccessKeyId' 2>/dev/null)
            else
                log_warning "Recovery: failed to list AccessKeys for user $user_name: $ak_list_output"
            fi
            sleep 5
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            sleep $((retry * 2))
        fi
    done

    # 失败时输出完整诊断信息
    log_warning "Failed to delete RAM User after $max_retries retries: $user_name"
    if [ -n "$user_error" ]; then
        log_warning "  Last error: $user_error"
        log_warning "  Troubleshooting:"
        log_warning "    1. Check if user has remaining AccessKeys: aliyun ram ListAccessKeys --UserName $user_name"
        log_warning "    2. Check if user has attached policies: aliyun ram ListPoliciesForUser --UserName $user_name"
        log_warning "    3. Check if user has login profile: aliyun ram GetLoginProfile --UserName $user_name"
    fi
    return 1
}

# Cleanup a RAM Role with policy detachment and retry
# Usage: cleanup_ram_role "$role_name"
cleanup_ram_role() {
    local role_name="$1"
    local max_retries=3

    # 1. Detach all policies with retry and DNS-safe error handling
    local retry=0
    local detach_error=""
    while [ $retry -lt $max_retries ]; do
        local policy_count
        local list_output
        if ! list_output=$(aliyun ram ListPoliciesForRole --RoleName "$role_name" 2>&1); then
            log_warning "Failed to list policies for role $role_name (attempt $((retry+1))/$max_retries): $list_output"
            retry=$((retry + 1))
            [ $retry -lt $max_retries ] && sleep $((retry * 2))
            continue
        fi
        policy_count=$(echo "$list_output" | jq '.Policies.Policy | length' 2>/dev/null || echo "0")

        if [ "$policy_count" = "0" ] || [ -z "$policy_count" ]; then
            log_info "All policies detached from role $role_name"
            break
        fi

        log_info "Found $policy_count policies attached to role $role_name, detaching..."

        # Detach all policies with individual retry (avoid subshell by using process substitution)
        detach_error=""
        local detach_failed=false
        while IFS= read -r policy_name; do
            [ -z "$policy_name" ] && continue
            # Retry individual policy detachment
            local pol_retry=0 pol_max=3 pol_delay=2
            while [ $pol_retry -lt $pol_max ]; do
                local detach_output
                if detach_output=$(aliyun ram DetachPolicyFromRole --PolicyType Custom --PolicyName "$policy_name" --RoleName "$role_name" 2>&1); then
                    log_info "Detached policy $policy_name from role $role_name"
                    break
                fi
                detach_error="$detach_output"
                pol_retry=$((pol_retry + 1))
                if [ $pol_retry -lt $pol_max ]; then
                    log_warning "Failed to detach policy $policy_name (attempt $pol_retry/$pol_max), retrying in ${pol_delay}s..."
                    sleep "$pol_delay"
                    pol_delay=$((pol_delay * 2))
                else
                    detach_failed=true
                    log_warning "Failed to detach policy $policy_name from role $role_name after $pol_max attempts: $detach_error"
                fi
            done
        done < <(echo "$list_output" | jq -r '.Policies.Policy[]?.PolicyName' 2>/dev/null)

        # Wait for detachment to propagate (5s for RAM API eventual consistency)
        sleep 5
        retry=$((retry + 1))
    done
    # 只在失败时输出详细错误
    if [ $retry -ge $max_retries ] && [ -n "$detach_error" ]; then
        log_warning "Failed to detach policies from role $role_name after $max_retries retries: $detach_error"
    fi

    # 2. Delete Role with retry and DeleteConflict auto-recovery
    retry=0
    local role_error=""
    while [ $retry -lt $max_retries ]; do
        local delete_output
        if delete_output=$(aliyun ram DeleteRole --RoleName "$role_name" 2>&1); then
            return 0
        fi
        role_error="$delete_output"

        # Handle DeleteConflict: re-list and detach remaining policies, then retry
        if echo "$delete_output" | grep -q "DeleteConflict"; then
            log_warning "DeleteConflict for role $role_name, re-detaching residual policies..."
            local recover_output
            if recover_output=$(aliyun ram ListPoliciesForRole --RoleName "$role_name" 2>&1); then
                while IFS= read -r pol_name; do
                    [ -z "$pol_name" ] && continue
                    aliyun ram DetachPolicyFromRole --PolicyType Custom --PolicyName "$pol_name" --RoleName "$role_name" 2>&1 || true
                    log_info "Recovery: detached residual policy $pol_name from role $role_name"
                done < <(echo "$recover_output" | jq -r '.Policies.Policy[]?.PolicyName' 2>/dev/null)
                sleep 5
            else
                log_warning "Recovery: failed to list policies for role $role_name: $recover_output"
            fi
        fi

        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            sleep $((retry * 2))
        fi
    done

    # 失败时输出详细错误
    log_warning "Failed to delete RAM Role after $max_retries retries: $role_name"
    if [ -n "$role_error" ]; then
        log_warning "  Error: $role_error"
        if [ -n "$detach_error" ]; then
            log_warning "  Policy detach error: $detach_error"
        fi
        log_warning "  Troubleshooting:"
        log_warning "    1. Check attached policies: aliyun ram ListPoliciesForRole --RoleName $role_name"
        log_warning "    2. Check instance profiles: aliyun ram ListInstanceProfilesForRole --RoleName $role_name"
    fi
    return 1
}

# Update an existing RAM Role's trust policy (AssumeRolePolicyDocument)
# Usage: update_ram_role_trust_policy "$role_name" "$new_trust_policy"
update_ram_role_trust_policy() {
    local role_name="$1"
    local trust_policy="$2"

    log_info "Updating trust policy for RAM Role: $role_name"
    local retry=0 max_retries=3 delay=3
    while [ $retry -lt $max_retries ]; do
        if aliyun ram UpdateRole \
            --RoleName "$role_name" \
            --NewAssumeRolePolicyDocument "$trust_policy" 2>&1; then
            log_success "Updated trust policy for role: $role_name"
            return 0
        fi
        retry=$((retry + 1))
        if [ $retry -lt $max_retries ]; then
            log_warning "UpdateRole attempt $retry/$max_retries failed, retrying in ${delay}s..."
            sleep "$delay"
            delay=$((delay * 2))
        fi
    done
    log_error "Failed to update trust policy for role $role_name after $max_retries attempts"
    return 1
}

# Wrapper: creates both Provider Role and Pod SA Role (preserves original behavior)
create_ram_roles() {
    log_step "Creating shared RAM Policy"

    # Create unified KMS Policy (shared by all roles)
    create_ram_policy "$KMS_POLICY_NAME"
}

create_cloud_resources() {
    log_step "Creating shared KMS Policy"

    # Create unified RAM Policy
    create_ram_policy "$KMS_POLICY_NAME"
    create_ram_roles
}

prepare_all_resources() {
    create_cloud_resources

    # Pre-compute TARGET_ROLE_ARN for YAML envsubst (needed by replace_yaml_placeholders)
    if [[ -n "${TARGET_ACCOUNT_ID:-}" ]]; then
        TARGET_ROLE_ARN="acs:ram::${TARGET_ACCOUNT_ID}:role/tc006-cross-account-role-${TEST_SUFFIX}"
        export TARGET_ROLE_ARN
    fi

    replace_yaml_placeholders
}

replace_yaml_placeholders() {
    log_step "Replacing YAML placeholders"
    
    local yaml_files=(
        "fixtures/pod/test-backward-compat-pod.yaml"
        "fixtures/spc/test-backward-compat-spc.yaml"
        "fixtures/pod/test-pod-sa-auth-pod.yaml"
        "fixtures/spc/test-pod-sa-auth-spc.yaml"
        "fixtures/pod/test-cross-account-pod.yaml"
        "fixtures/spc/test-cross-account-spc.yaml"
        "fixtures/pod/test-ak-sk-pod.yaml"
        "fixtures/spc/test-ak-sk-spc.yaml"
        "fixtures/pod/test-ram-role-pod.yaml"
        "fixtures/spc/test-ram-role-spc.yaml"
        "fixtures/pod/test-node-publish-pod.yaml"
        "fixtures/spc/test-node-publish-spc.yaml"
        "fixtures/pod/test-ecs-ram-role-pod.yaml"
        "fixtures/spc/test-ecs-ram-role-spc.yaml"
        "fixtures/pod/test-jmespath-pod.yaml"
        "fixtures/spc/test-jmespath-spc.yaml"
        "fixtures/pod/test-rotation-pod.yaml"
        "fixtures/spc/test-rotation-spc.yaml"
        "fixtures/pod/test-secret-sync-pod.yaml"
        "fixtures/spc/test-secret-sync-spc.yaml"
        "fixtures/pod/test-cleanup-pod.yaml"
        "fixtures/spc/test-cleanup-spc.yaml"
    )
    
    # Backup original files (idempotent: skip if .bak already exists from a previous run)
    for file in "${yaml_files[@]}"; do
        if [ -f "$file" ] && [ ! -f "$file.bak" ]; then
            cp "$file" "$file.bak"
            BACKED_UP_FILES+=("$file")
        elif [ -f "$file.bak" ]; then
            # Restore from previous backup before re-applying substitutions
            cp "$file.bak" "$file"
            if [[ ! " ${BACKED_UP_FILES[*]:-} " =~ " $file " ]]; then
                BACKED_UP_FILES+=("$file")
            fi
        fi
    done
    
    # Export environment variables for envsubst
    export NAMESPACE="${NAMESPACE:-staging}"
    export POD_SA_NAME="tc001-pod-sa-${TEST_SUFFIX}"
    export POD_SA_ROLE_ARN="acs:ram::${SOURCE_ACCOUNT_ID}:role/tc001-pod-sa-role-${TEST_SUFFIX}"
    export TARGET_ACCOUNT_ID="${TARGET_ACCOUNT_ID:-PLACEHOLDER}"
    export TARGET_ROLE_ARN="${TARGET_ROLE_ARN:-}"

    # KMS Secret names (local to this function for envsubst; also defined as local in each TC function)
    local POD_SA_SECRET_NAME="tc001-pod-sa-secret-${TEST_SUFFIX}"
    local RRSA_SECRET_NAME="tc002-rrsa-secret-${TEST_SUFFIX}"
    local RAM_ROLE_SECRET_NAME="tc003-ram-role-secret-${TEST_SUFFIX}"
    local NODE_PUB_SECRET_NAME="tc004-node-pub-secret-${TEST_SUFFIX}"
    local AKSK_SECRET_NAME="tc005-aksk-secret-${TEST_SUFFIX}"
    local CROSS_ACCOUNT_SECRET_NAME="tc006-cross-account-secret-${TEST_SUFFIX}"
    local ECS_ROLE_SECRET_NAME="tc007-ecs-role-secret-${TEST_SUFFIX}"
    local JMESPATH_SECRET_NAME="tc008-jmespath-secret-${TEST_SUFFIX}"
    local ROTATION_SECRET_NAME="tc009-rotation-secret-${TEST_SUFFIX}"
    local SYNC_SECRET_NAME="tc010-sync-secret-${TEST_SUFFIX}"
    local CLEANUP_SECRET_NAME="tc011-cleanup-secret-${TEST_SUFFIX}"
    export POD_SA_SECRET_NAME RRSA_SECRET_NAME RAM_ROLE_SECRET_NAME NODE_PUB_SECRET_NAME AKSK_SECRET_NAME
    export CROSS_ACCOUNT_SECRET_NAME ECS_ROLE_SECRET_NAME JMESPATH_SECRET_NAME
    export ROTATION_SECRET_NAME SYNC_SECRET_NAME CLEANUP_SECRET_NAME

    # Use envsubst to replace environment variables
    for file in "${yaml_files[@]}"; do
        if [ -f "$file" ]; then
            envsubst '${NAMESPACE} ${POD_SA_NAME} ${POD_SA_ROLE_ARN} ${TARGET_ACCOUNT_ID} ${TARGET_ROLE_ARN} ${POD_SA_SECRET_NAME} ${RRSA_SECRET_NAME} ${CROSS_ACCOUNT_SECRET_NAME} ${AKSK_SECRET_NAME} ${RAM_ROLE_SECRET_NAME} ${NODE_PUB_SECRET_NAME} ${ECS_ROLE_SECRET_NAME} ${JMESPATH_SECRET_NAME} ${ROTATION_SECRET_NAME} ${SYNC_SECRET_NAME} ${CLEANUP_SECRET_NAME}' < "$file" > "$file.tmp" && mv "$file.tmp" "$file"
        fi
    done
    
    log_success "YAML placeholder replacement complete"
}

restore_yaml_files() {
    log_info "Restoring YAML files"
    for file in "${BACKED_UP_FILES[@]}"; do
        if [ -f "$file.bak" ]; then
            mv "$file.bak" "$file"
        fi
    done
}

# Prepare test YAML from template files (sed replaces Secret name placeholders + envsubst replaces env vars)
# Usage: prepare_test_yaml <spc_template> <pod_template> <tmp_spc> <tmp_pod> [sed_pair...]
prepare_test_yaml() {
    local spc_template="$1" pod_template="$2"
    local tmp_spc="$3" tmp_pod="$4"
    shift 4

    # Restore templates from .bak (preferred) or git if placeholders were consumed by a previous run
    for _pty_file in "$spc_template" "$pod_template"; do
        if [[ -f "$_pty_file" ]] && ! grep -q '\${' "$_pty_file" 2>/dev/null; then
            if [[ -f "${_pty_file}.bak" ]]; then
                cp "${_pty_file}.bak" "$_pty_file"
            else
                git checkout -- "$_pty_file" 2>/dev/null || {
                    log_error "Failed to restore template: $_pty_file (no .bak or git)"
                    return 1
                }
            fi
        fi
    done

    cp "$spc_template" "$tmp_spc"
    cp "$pod_template" "$tmp_pod"
    # Replace Secret name placeholders
    while [ $# -ge 2 ]; do
        sed -i "s/\${$1}/$2/g" "$tmp_spc" "$tmp_pod"
        shift 2
    done
    # envsubst replaces ${NAMESPACE} and other environment variables
    # Note: secret name placeholders are already handled by sed above
    envsubst '${NAMESPACE} ${POD_SA_NAME} ${POD_SA_ROLE_ARN} ${TARGET_ACCOUNT_ID} ${TARGET_ROLE_ARN}' < "$tmp_spc" > "${tmp_spc}.tmp" && mv "${tmp_spc}.tmp" "$tmp_spc"
    envsubst '${NAMESPACE} ${POD_SA_NAME} ${POD_SA_ROLE_ARN} ${TARGET_ACCOUNT_ID} ${TARGET_ROLE_ARN}' < "$tmp_pod" > "${tmp_pod}.tmp" && mv "${tmp_pod}.tmp" "$tmp_pod"
}

# ============================================================================
# DaemonSet Authentication Configuration Common Functions
# ============================================================================

# Configure Provider DaemonSet authentication mode
# Parameters: $1 - auth mode: rrsa | rrsa_oidc_only | aksk | aksk_role | none
# Auth-related environment variable names managed by test auth functions.
# These are the only variables that should be added/removed by auth patching.
# Non-auth variables (e.g., ALICLOUD_REGION) injected by Helm are preserved.
AUTH_ENV_VARS=(
    "ACCESS_KEY_ID"
    "SECRET_ACCESS_KEY"
    "ALICLOUD_ACCESS_KEY_ID"
    "ALICLOUD_ACCESS_KEY_SECRET"
    "ALICLOUD_ACCOUNT_ID"
    "ALICLOUD_CLUSTER_ID"
    "ALICLOUD_ROLE_ARN"
    "ALICLOUD_OIDC_PROVIDER_ARN"
    "ALICLOUD_USE_CSI_DRIVER"
    "ALICLOUD_ROLE_SESSION_NAME"
    "ALICLOUD_ROLE_SESSION_EXPIRATION"
    "ALICLOUD_CROSS_ACCOUNT_ROLE_ARN"
)

patch_daemonset_for_auth() {
    local auth_mode="$1"
    local ds_name="csi-secrets-store-provider-alibabacloud"
    local ns="kube-system"

    log_info "Configuring Provider DaemonSet auth mode: $auth_mode"

    # 1. GET current DaemonSet spec
    local ds_json
    ds_json=$(kubectl get daemonset "$ds_name" -n "$ns" -o json)

    # 2. Find the provider container index
    local container_idx
    container_idx=$(echo "$ds_json" | jq -r '.spec.template.spec.containers | to_entries[] | select(.value.name == "provider-alibabacloud-installer") | .key')
    if [[ -z "$container_idx" ]]; then
        container_idx=0
    fi

    # 3. Build jq filter to remove auth-related env vars (preserve non-auth vars like ALICLOUD_REGION)
    local jq_filter
    jq_filter="[.[] | select(.name != \"${AUTH_ENV_VARS[0]}\""
    for ((i = 1; i < ${#AUTH_ENV_VARS[@]}; i++)); do
        jq_filter+=" and .name != \"${AUTH_ENV_VARS[i]}\""
    done
    jq_filter+=")]"

    local filtered_env
    filtered_env=$(echo "$ds_json" | jq -c --argjson idx "$container_idx" \
        ".spec.template.spec.containers[\$idx].env // [] | $jq_filter" 2>/dev/null || echo "[]")
    if [[ -z "$filtered_env" || "$filtered_env" == "null" ]]; then
        filtered_env="[]"
    fi

    # 4. Determine new auth vars based on mode
    local new_auth_vars=""
    case "$auth_mode" in
        rrsa)
            new_auth_vars='[{"name":"ALICLOUD_OIDC_PROVIDER_ARN","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"oidcproviderarn"}}},{"name":"ALICLOUD_ROLE_ARN","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"rolearn"}}}]'
            ;;
        rrsa_oidc_only)
            new_auth_vars='[{"name":"ALICLOUD_OIDC_PROVIDER_ARN","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"oidcproviderarn"}}}]'
            ;;
        aksk|node_publish_secret)
            # node_publish_secret uses AK/SK env vars (credentials delivered via CSI nodePublishSecretRef)
            new_auth_vars='[{"name":"ACCESS_KEY_ID","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"id"}}},{"name":"SECRET_ACCESS_KEY","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"secret"}}}]'
            ;;
        aksk_role)
            new_auth_vars='[{"name":"ACCESS_KEY_ID","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"id"}}},{"name":"SECRET_ACCESS_KEY","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"secret"}}},{"name":"ALICLOUD_ROLE_ARN","valueFrom":{"secretKeyRef":{"name":"alibaba-credentials","key":"rolearn"}}}]'
            ;;
        none)
            # No auth env vars — falls through to ECS RAM Role auth
            # Still need to filter out any leftover auth vars from previous mode
            ;;
        *)
            log_error "Unknown auth mode: $auth_mode"
            return 1
            ;;
    esac

    # 5. Merge filtered env with new auth vars
    local final_env
    if [[ -n "$new_auth_vars" ]]; then
        final_env=$(echo "$filtered_env" | jq -c ". + $new_auth_vars" 2>/dev/null || echo "$filtered_env")
    else
        final_env="$filtered_env"
    fi

    # 6. Build updated DaemonSet JSON (replace env + add restart annotation)
    local updated_ds
    updated_ds=$(echo "$ds_json" | jq -c \
        --argjson idx "$container_idx" \
        --argjson new_env "$final_env" \
        --arg restart_time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '.spec.template.spec.containers[$idx].env = $new_env | .spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"] = $restart_time')

    # 7. UPDATE via kubectl replace (PUT semantics — fully replaces the resource)
    echo "$updated_ds" | kubectl replace -f - 2>&1 || {
        log_error "Failed to replace DaemonSet"
        return 1
    }

    # 8. Wait for rollout
    log_info "Waiting for DaemonSet rollout..."
    kubectl rollout status daemonset/"$ds_name" -n "$ns" --timeout=120s || \
        log_warning "Provider DaemonSet rollout did not complete within 120s"

    log_info "DaemonSet auth mode configured: $auth_mode"
}

# Configure Provider authentication - delegates to patch_daemonset_for_auth with dedup
# Skips redundant patches when the auth mode hasn't changed
# Parameters: $1 - auth mode: rrsa | rrsa_oidc_only | aksk | aksk_role | none
configure_provider_auth() {
    local auth_mode="$1"
    if [[ "$auth_mode" == "$_CURRENT_AUTH_MODE" ]]; then
        # Auth mode unchanged, but Secret content may have been updated by the caller.
        # Trigger a rolling restart so the DaemonSet picks up the new Secret values.
        log_info "Provider auth mode unchanged ('$auth_mode'), restarting to apply Secret changes..."
        kubectl rollout restart daemonset/csi-secrets-store-provider-alibabacloud -n kube-system 2>/dev/null || true
        kubectl rollout status daemonset/csi-secrets-store-provider-alibabacloud -n kube-system --timeout=120s 2>/dev/null || \
            log_warning "Provider DaemonSet rollout did not complete within 120s"
        return 0
    fi
    patch_daemonset_for_auth "$auth_mode"
    _CURRENT_AUTH_MODE="$auth_mode"
}

# Read env_auth_mode from SPC YAML file and map to configure_provider_auth mode.
# Usage: auth_mode=$(read_env_auth_mode <spc_yaml_file>)
# Returns: auth mode string (aksk, aksk_role, rrsa, rrsa_oidc_only, none)
read_env_auth_mode() {
    local spc_file="$1"
    local mode
    mode=$(grep 'env_auth_mode:' "$spc_file" | head -1 | awk '{print $2}' | tr -d '"' | tr -d "'")
    if [[ -z "$mode" ]]; then
        log_warning "env_auth_mode not found in $spc_file, defaulting to 'none'"
        echo "none"
        return
    fi
    # Map node_publish_secret to aksk (Node Publish Secret uses AK/SK DaemonSet env vars)
    if [[ "$mode" == "node_publish_secret" ]]; then
        echo "aksk"
    else
        echo "$mode"
    fi
}

# Check if CSI Driver has rotation and sync enabled
check_rotation_enabled() {
    local helm_release
    helm_release=$(helm list -n kube-system -q 2>/dev/null | grep -i csi-secrets-store || echo "")
    if [ -z "$helm_release" ]; then
        return 1
    fi
    local rotation_enabled
    rotation_enabled=$(helm get values "$helm_release" -n kube-system 2>/dev/null | grep -i "enableSecretRotation.*true" || echo "")
    local sync_enabled
    sync_enabled=$(helm get values "$helm_release" -n kube-system 2>/dev/null | grep -A1 "syncSecret" | grep -i "enabled.*true" || echo "")
    if [ -n "$rotation_enabled" ] && [ -n "$sync_enabled" ]; then
        return 0
    fi
    return 1
}

# ============================================================================
# Authentication Clear & Configuration
# ============================================================================

# Clear all authentication configuration from DaemonSet and K8s Secrets.
# Called by TC-001~TC-007 before configuring their own auth.
clear_all_auth() {
    log_info "Clearing all authentication configuration..."

    # 1. Remove auth env vars from DaemonSet and wait for rollout
    patch_daemonset_for_auth "none"

    # 2. Delete alibaba-credentials Secret in both kube-system and test namespace
    kubectl delete secret alibaba-credentials -n kube-system --ignore-not-found=true 2>/dev/null || true
    kubectl delete secret alibaba-credentials -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true

    _AUTH_CONFIGURED=false
    _CURRENT_AUTH_MODE=""
}

# Ensure auth is configured for feature tests (TC-008~TC-011).
# If _AUTH_CONFIGURED is true, reuse existing auth; otherwise configure minimal AK/SK auth.
ensure_auth_for_feature_tests() {
    if [ "$_AUTH_CONFIGURED" = "true" ] && [ "$_CURRENT_AUTH_MODE" != "none" ]; then
        log_info "Auth already configured, reusing existing auth"
        return 0
    fi
    log_info "No auth configured, setting up minimal AK/SK auth for feature tests"
    # Create a minimal RAM User with KMS policy
    FALLBACK_USER="tc-fallback-user-${TEST_SUFFIX}"
    if ! create_test_ram_user "$FALLBACK_USER" "$KMS_POLICY_NAME"; then
        log_error "Failed to create fallback RAM User for feature tests"
        return 1
    fi
    FALLBACK_AK="$_CREATED_USER_AK"
    local fallback_sk="$_CREATED_USER_SK"
    kubectl create secret generic alibaba-credentials \
        --from-literal=id="$FALLBACK_AK" \
        --from-literal=secret="$fallback_sk" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
    configure_provider_auth "aksk"
    _AUTH_CONFIGURED=true
    log_success "Fallback AK/SK auth configured for feature tests"
    return 0
}

# ============================================================================
# Test Cases (~300 lines)
# ============================================================================

# TC-002: Provider RRSA Authentication
run_tc002_provider_rrsa() {
    local test_name="TC-002: Provider RRSA Authentication"
    local RRSA_SECRET_NAME="tc002-rrsa-secret-${TEST_SUFFIX}"
    local PROVIDER_ROLE_NAME="tc002-provider-rrsa-role-${TEST_SUFFIX}"
    log_step "Starting $test_name"
    
    if should_skip_test "TC-002"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local cleanup function for TC-002 resources
    _tc002_cleanup() {
        log_info "TC-002: Cleaning up resources..."
        cleanup_ram_role "$PROVIDER_ROLE_NAME" || true
        delete_kms_secret "$RRSA_SECRET_NAME"
        cleanup_test_resources "TC-002"
        log_info "TC-002: Cleanup complete"
    }
    
    # Clear all auth and configure fresh for this test
    clear_all_auth

    # Create KMS Secret for this test
    create_kms_secret "$RRSA_SECRET_NAME" "rrsa-test-value"

    # Create Provider Role with OIDC trust policy (only used by TC-002)
    log_info "TC-002: Creating Provider Role..."
    local provider_trust_policy
    provider_trust_policy=$(generate_trust_policy "oidc" "$OIDC_PROVIDER_ARN" "kube-system" "csi-secrets-store-provider-alibabacloud")
    if ! create_ram_role "$PROVIDER_ROLE_NAME" "$provider_trust_policy" "CSI Provider RRSA Role" "$KMS_POLICY_NAME"; then
        log_error "TC-002: Provider Role creation failed: $PROVIDER_ROLE_NAME"
        record_result "$test_name" "FAIL" "Provider Role creation failed"
        _tc002_cleanup
        return 1
    fi

    local trust_policy
    trust_policy=$(retry_aliyun 3 2 aliyun ram GetRole --RoleName "${PROVIDER_ROLE_NAME}" | \
        jq -r '.Role.AssumeRolePolicyDocument' 2>/dev/null || true)

    local expected_sub="system:serviceaccount:kube-system:csi-secrets-store-provider-alibabacloud"
    local expected_aud="sts.aliyuncs.com"
    local needs_fix=false

    if [[ -n "$trust_policy" ]]; then
        if ! echo "$trust_policy" | grep -q "$expected_sub" 2>/dev/null; then
            log_warning "TC-002: Trust policy missing oidc:sub '$expected_sub', will fix..."
            needs_fix=true
        fi
        if ! echo "$trust_policy" | grep -q "$expected_aud" 2>/dev/null; then
            log_warning "TC-002: Trust policy missing oidc:aud '$expected_aud', will fix..."
            needs_fix=true
        fi
    else
        log_warning "TC-002: Could not retrieve trust policy, will attempt fix..."
        needs_fix=true
    fi

    if [[ "$needs_fix" = "true" ]]; then
        log_info "TC-002: Fixing Provider Role trust policy..."
        local fixed_trust_policy
        # 构造完整的 OIDC 信任策略 JSON，包含 oidc:sub/oidc:aud/oidc:iss 条件和 Federated Principal
        fixed_trust_policy=$(generate_trust_policy "oidc" "$OIDC_PROVIDER_ARN" "kube-system" "csi-secrets-store-provider-alibabacloud")
        # update_ram_role_trust_policy 内部使用 --NewAssumeRolePolicyDocument 参数调用 aliyun ram UpdateRole
        update_ram_role_trust_policy "${PROVIDER_ROLE_NAME}" "$fixed_trust_policy" || \
            log_warning "TC-002: Failed to update trust policy, tests may fail"
    else
        log_success "TC-002: Provider Role trust policy verified"
    fi

    # Configure Provider RRSA
    # Recreate alibaba-credentials Secret (clear_all_auth deleted it)
    local provider_role_arn="acs:ram::${SOURCE_ACCOUNT_ID}:role/${PROVIDER_ROLE_NAME}"
    kubectl create secret generic alibaba-credentials \
        --from-literal=oidcproviderarn="${OIDC_PROVIDER_ARN}" \
        --from-literal=rolearn="${provider_role_arn}" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
    configure_provider_auth "rrsa"
    _AUTH_CONFIGURED=true
    
    # Apply SPC and Pod
    kubectl apply -f fixtures/spc/test-backward-compat-spc.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "SPC creation failed"; _tc002_cleanup; return 1; }
    kubectl apply -f fixtures/pod/test-backward-compat-pod.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "Pod creation failed"; _tc002_cleanup; return 1; }
    
    # Wait and verify
    if ! wait_for_pod_ready "backward-compat-test" 120; then
        log_error "[DIAG] TC-002: Provider Pod logs (last 50 lines):"
        kubectl logs -l app=csi-secrets-store-provider-alibabacloud -n kube-system --tail=50 2>/dev/null || true
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc002_cleanup
        return 1
    fi
    
    if ! verify_mount "backward-compat-test" "$RRSA_SECRET_NAME" "rrsa-test-value"; then
        log_error "[DIAG] TC-002: Provider Pod logs (last 50 lines):"
        kubectl logs -l app=csi-secrets-store-provider-alibabacloud -n kube-system --tail=50 2>/dev/null || true
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc002_cleanup
        return 1
    fi
    
    record_result "$test_name" "PASS" "Provider RRSA authentication successful"
    _tc002_cleanup
    log_step "$test_name complete"
}

# TC-001: Pod SA RRSA Authentication
run_tc001_pod_sa_rrsa() {
    local test_name="TC-001: Pod SA RRSA Authentication"
    local POD_SA_SECRET_NAME="tc001-pod-sa-secret-${TEST_SUFFIX}"
    local POD_SA_ROLE_NAME="tc001-pod-sa-role-${TEST_SUFFIX}"
    local POD_SA_SA_NAME="tc001-pod-sa-${TEST_SUFFIX}"
    log_step "Starting $test_name"
    
    if should_skip_test "TC-001"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local cleanup function for TC-001 resources
    _tc001_cleanup() {
        log_info "TC-001: Cleaning up resources..."
        cleanup_ram_role "$POD_SA_ROLE_NAME" || true
        kubectl delete serviceaccount "$POD_SA_SA_NAME" -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
        kubectl delete secret alibaba-credentials -n kube-system --ignore-not-found=true 2>/dev/null || true
        delete_kms_secret "$POD_SA_SECRET_NAME"
        cleanup_test_resources "TC-001"
        log_info "TC-001: Cleanup complete"
    }
    
    # Clear all auth and configure fresh for this test
    clear_all_auth
    
    # Create KMS Secret for this test
    create_kms_secret "$POD_SA_SECRET_NAME" "pod-sa-test-value"

    # Step 1: Create Pod SA RAM Role with OIDC trust policy
    log_info "TC-001: Creating Pod SA Role with OIDC trust policy..."
    local pod_sa_trust_policy
    pod_sa_trust_policy=$(generate_trust_policy "oidc" "$OIDC_PROVIDER_ARN" "$NAMESPACE" "$POD_SA_SA_NAME")
    if ! create_ram_role "$POD_SA_ROLE_NAME" "$pod_sa_trust_policy" "Pod SA RRSA Role" "$KMS_POLICY_NAME"; then
        log_error "TC-001: Failed to create Pod SA Role: $POD_SA_ROLE_NAME"
        record_result "$test_name" "FAIL" "Pod SA Role creation failed"
        _tc001_cleanup
        return 1
    fi

    # Verify KMS Policy binding on Pod SA Role
    if ! verify_role_policy_attachment "$POD_SA_ROLE_NAME" "$KMS_POLICY_NAME"; then
        log_error "TC-001: KMS Policy $KMS_POLICY_NAME could not be verified on role $POD_SA_ROLE_NAME"
        record_result "$test_name" "FAIL" "KMS Policy verification failed"
        _tc001_cleanup
        return 1
    fi
    log_success "TC-001: KMS Policy verified on Pod SA Role"
    sleep 5  # Wait for IAM permission propagation

    # Step 2: Create K8s ServiceAccount with RRSA role ARN annotation
    log_info "TC-001: Creating ServiceAccount with role ARN annotation..."
    local pod_sa_role_arn="acs:ram::${SOURCE_ACCOUNT_ID}:role/${POD_SA_ROLE_NAME}"
    kubectl create serviceaccount "$POD_SA_SA_NAME" -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
    kubectl annotate serviceaccount "$POD_SA_SA_NAME" -n "$NAMESPACE" \
        "ack.alibabacloud.com/role-arn=${pod_sa_role_arn}" --overwrite 2>&1

    # Step 3: Configure DaemonSet with OIDC provider ARN only (rrsa_oidc_only mode)
    log_info "TC-001: Configuring DaemonSet with OIDC provider ARN (rrsa_oidc_only)..."
    kubectl create secret generic alibaba-credentials \
        --from-literal=oidcproviderarn="${OIDC_PROVIDER_ARN}" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
    configure_provider_auth "rrsa_oidc_only"
    _AUTH_CONFIGURED=true

    # Step 4: Apply SPC (usePodServiceAccountToken: "true") and Pod (serviceAccountName)
    kubectl apply -f fixtures/spc/test-pod-sa-auth-spc.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "SPC creation failed"; _tc001_cleanup; return 1; }
    kubectl apply -f fixtures/pod/test-pod-sa-auth-pod.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "Pod creation failed"; _tc001_cleanup; return 1; }
    
    # Wait and verify
    if ! wait_for_pod_ready "pod-sa-test" 120; then
        log_error "[DIAG] TC-001: Provider Pod logs (last 50 lines):"
        kubectl logs -l app=csi-secrets-store-provider-alibabacloud -n kube-system --tail=50 2>/dev/null || true
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc001_cleanup
        return 1
    fi
    
    if ! verify_mount "pod-sa-test" "$POD_SA_SECRET_NAME" "pod-sa-test-value"; then
        log_error "[DIAG] TC-001: Provider Pod logs (last 50 lines):"
        kubectl logs -l app=csi-secrets-store-provider-alibabacloud -n kube-system --tail=50 2>/dev/null || true
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc001_cleanup
        return 1
    fi
    
    record_result "$test_name" "PASS" "Pod SA RRSA authentication successful"
    _tc001_cleanup
    log_step "$test_name complete"
}

# TC-005: AK/SK Authentication
run_tc005_aksk() {
    local test_name="TC-005: AK/SK Authentication"
    local AKSK_SECRET_NAME="tc005-aksk-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"
    
    if should_skip_test "TC-005"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local variables for TC-005 resources (declared early for cleanup function)
    local tc005_user="tc005-user-${TEST_SUFFIX}"
    local tc005_ak=""

    # Local cleanup function for TC-005 resources
    _tc005_cleanup() {
        log_info "TC-005: Cleaning up resources..."
        cleanup_test_ram_user "$tc005_user" "${tc005_ak}"
        delete_kms_secret "$AKSK_SECRET_NAME"
        cleanup_test_resources "TC-005"
        log_info "TC-005: Cleanup complete"
    }

    # Clear all auth and configure fresh for this test
    clear_all_auth

    # Create KMS Secret for this test
    create_kms_secret "$AKSK_SECRET_NAME" "aksk-test-value"

    # Step 1: Create RAM User, grant unified KMS Policy
    if ! create_test_ram_user "$tc005_user" "$KMS_POLICY_NAME"; then
        record_result "$test_name" "FAIL" "Failed to create test RAM User"
        _tc005_cleanup
        return 1
    fi
    tc005_ak="$_CREATED_USER_AK"
    local tc005_sk="$_CREATED_USER_SK"

    # Verify KMS Policy binding
    if ! verify_user_policy_attachment "$tc005_user" "$KMS_POLICY_NAME"; then
        log_error "TC-005: KMS Policy $KMS_POLICY_NAME could not be verified on user $tc005_user"
        record_result "$test_name" "FAIL" "KMS policy binding verification failed"
        _tc005_cleanup
        return 1
    fi
    log_success "TC-005: KMS Policy verified"
    sleep 5  # Wait for IAM permission propagation

    # Step 2: DaemonSet env references the Secret
    kubectl create secret generic alibaba-credentials \
        --from-literal=id="$tc005_ak" \
        --from-literal=secret="$tc005_sk" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1

    # Read auth mode from SPC fixture and configure DaemonSet
    local tc005_auth_mode
    tc005_auth_mode=$(read_env_auth_mode fixtures/spc/test-ak-sk-spc.yaml)
    configure_provider_auth "$tc005_auth_mode"
    _AUTH_CONFIGURED=true

    # Create AK/SK Secret for Pod credential passing
    kubectl create secret generic alibaba-credentials \
        --from-literal=id="$tc005_ak" \
        --from-literal=secret="$tc005_sk" \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
    
    # Prepare AK/SK SPC and Pod from template files
    local aksk_spc="/tmp/test-aksk-spc-${TEST_SUFFIX}.yaml"
    local aksk_pod="/tmp/test-aksk-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-ak-sk-spc.yaml fixtures/pod/test-ak-sk-pod.yaml \
        "$aksk_spc" "$aksk_pod" \
        "AKSK_SECRET_NAME" "$AKSK_SECRET_NAME"
    
    kubectl apply -f "$aksk_spc" -n "$NAMESPACE" 2>&1 || { rm -f "$aksk_spc" "$aksk_pod"; record_result "$test_name" "FAIL" "SPC creation failed"; _tc005_cleanup; return 1; }
    kubectl apply -f "$aksk_pod" -n "$NAMESPACE" 2>&1 || { rm -f "$aksk_spc" "$aksk_pod"; record_result "$test_name" "FAIL" "Pod creation failed"; _tc005_cleanup; return 1; }
    
    if ! wait_for_pod_ready "aksk-test" 120; then
        rm -f "$aksk_spc" "$aksk_pod"
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc005_cleanup
        return 1
    fi
    
    if ! verify_mount "aksk-test" "$AKSK_SECRET_NAME" "aksk-test-value"; then
        rm -f "$aksk_spc" "$aksk_pod"
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc005_cleanup
        return 1
    fi
    
    rm -f "$aksk_spc" "$aksk_pod"
    record_result "$test_name" "PASS" "AK/SK authentication successful"
    _tc005_cleanup
    log_step "$test_name complete"
}

# TC-006: Cross-account Authentication
run_tc006_cross_account() {
    local test_name="TC-006: Cross-account Authentication"
    local CROSS_ACCOUNT_SECRET_NAME="tc006-cross-account-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"
    
    if should_skip_test "TC-006"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi
    
    if [ -z "${TARGET_ACCOUNT_ID:-}" ]; then
        log_warning "TC-006: TARGET_ACCOUNT_ID not configured, skipping this test"
        record_result "$test_name" "SKIP" "Target account not configured"
        return 0
    fi
    
    if [ "${SKIP_CROSS_ACCOUNT:-false}" = "true" ]; then
        log_warning "TC-006: Cross-account test not configured or resources not prepared"
        record_result "$test_name" "SKIP" "Cross-account resources not ready"
        return 0
    fi

    # Local variables for TC-006 resources (declared early for cleanup function)
    local target_role_name="tc006-cross-account-role-${TEST_SUFFIX}"
    local target_policy_name="tc006-cross-account-kms-policy-${TEST_SUFFIX}"
    local tc006_user="tc006-user-${TEST_SUFFIX}"
    local tc006_sts_policy="tc006-sts-policy-${TEST_SUFFIX}"
    local tc006_ak=""

    # Local cleanup function for TC-006 resources
    _tc006_cleanup() {
        log_info "TC-006: Cleaning up resources..."
        # Clean up target account resources (using target credentials)
        if [[ -n "${TARGET_ACCOUNT_ACCESS_KEY_ID:-}" && -n "${TARGET_ACCOUNT_ACCESS_KEY_SECRET:-}" ]]; then
            delete_kms_secret "$CROSS_ACCOUNT_SECRET_NAME" "$TARGET_ACCOUNT_ACCESS_KEY_ID" "$TARGET_ACCOUNT_ACCESS_KEY_SECRET"
            # Detach policy from role in target account with retry
            local detach_retry=0 detach_max=3 detach_delay=2
            while [ $detach_retry -lt $detach_max ]; do
                if aliyun ram DetachPolicyFromRole --PolicyType Custom --PolicyName "$target_policy_name" --RoleName "$target_role_name" \
                    --access-key-id "$TARGET_ACCOUNT_ACCESS_KEY_ID" --access-key-secret "$TARGET_ACCOUNT_ACCESS_KEY_SECRET" 2>&1; then
                    break
                fi
                detach_retry=$((detach_retry + 1))
                if [ $detach_retry -lt $detach_max ]; then
                    sleep "$detach_delay"
                fi
            done
            # Delete role in target account with retry
            local role_retry=0 role_max=3 role_delay=2
            while [ $role_retry -lt $role_max ]; do
                if aliyun ram DeleteRole --RoleName "$target_role_name" \
                    --access-key-id "$TARGET_ACCOUNT_ACCESS_KEY_ID" --access-key-secret "$TARGET_ACCOUNT_ACCESS_KEY_SECRET" 2>&1; then
                    break
                fi
                role_retry=$((role_retry + 1))
                if [ $role_retry -lt $role_max ]; then
                    sleep "$role_delay"
                fi
            done
            # Delete policy in target account
            delete_ram_policy "$target_policy_name" "$TARGET_ACCOUNT_ACCESS_KEY_ID" "$TARGET_ACCOUNT_ACCESS_KEY_SECRET" || true
        fi
        # Clean up source account resources (顺序很重要:先清理 User 解绑策略,再删除策略)
        cleanup_test_ram_user "$tc006_user" "${tc006_ak}" || true
        delete_ram_policy "$tc006_sts_policy" || true
        cleanup_test_resources "TC-006"
        log_info "TC-006: Cleanup complete"
    }

    # Clear all auth and configure fresh for this test
    clear_all_auth

    # --- Create target account resources (KMS Secret, RAM Policy, RAM Role) ---
    if [ -z "${TARGET_ACCOUNT_ACCESS_KEY_ID:-}" ] || [ -z "${TARGET_ACCOUNT_ACCESS_KEY_SECRET:-}" ]; then
        log_error "TC-006: TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET not set, cannot create target account resources"
        record_result "$test_name" "SKIP" "Target account credentials not configured"
        return 0
    fi

    log_step "Creating target account resources (Account: $TARGET_ACCOUNT_ID)"

    # Resolve target account DKMS/region params (must NOT fall back to source account values)
    local target_key_id="${TARGET_ACCOUNT_ENCRYPTION_KEY_ID:-}"
    local target_dkms_id="${TARGET_ACCOUNT_DKMS_INSTANCE_ID:-}"
    local target_region="${TARGET_ACCOUNT_REGION_ID:-cn-hangzhou}"

    if [ -z "$target_key_id" ] || [ -z "$target_dkms_id" ]; then
        log_error "TC-006: TARGET_ACCOUNT_ENCRYPTION_KEY_ID and TARGET_ACCOUNT_DKMS_INSTANCE_ID must be set for cross-account KMS"
        record_result "$test_name" "FAIL" "Missing target account DKMS configuration"
        return 1
    fi

    # Step 1: Create KMS Secret in target account (with target DKMS params)
    if ! create_kms_secret "$CROSS_ACCOUNT_SECRET_NAME" "cross-account-value" \
        "$target_key_id" "$target_dkms_id" "$target_region" \
        "$TARGET_ACCOUNT_ACCESS_KEY_ID" "$TARGET_ACCOUNT_ACCESS_KEY_SECRET"; then
        log_error "TC-006: Failed to create KMS Secret in target account"
        record_result "$test_name" "FAIL" "Failed to create KMS Secret in target account"
        _tc006_cleanup
        return 1
    fi

    # Step 2: Create RAM Policy for KMS access in target account
    local target_policy_doc='{"Version":"1","Statement":[{"Effect":"Allow","Action":["kms:GetSecretValue","kms:Decrypt"],"Resource":"*"}]}'
    if ! create_ram_policy "$target_policy_name" "$target_policy_doc" "Cross-account test KMS policy" \
        "$TARGET_ACCOUNT_ACCESS_KEY_ID" "$TARGET_ACCOUNT_ACCESS_KEY_SECRET"; then
        log_error "TC-006: Failed to create RAM Policy in target account"
        record_result "$test_name" "FAIL" "Failed to create RAM Policy in target account"
        _tc006_cleanup
        return 1
    fi

    # Step 3: Create RAM Role with trust policy (trusts source account) and attach policy
    local target_trust_policy
    target_trust_policy=$(generate_trust_policy "ram" "$SOURCE_ACCOUNT_ID")
    if ! create_ram_role "$target_role_name" "$target_trust_policy" \
        "Cross-account test role (trusts source account $SOURCE_ACCOUNT_ID)" \
        "$target_policy_name" \
        "$TARGET_ACCOUNT_ACCESS_KEY_ID" "$TARGET_ACCOUNT_ACCESS_KEY_SECRET"; then
        log_error "TC-006: Failed to create RAM Role in target account"
        record_result "$test_name" "FAIL" "Failed to create RAM Role in target account"
        _tc006_cleanup
        return 1
    fi

    # Update TARGET_ROLE_ARN with actual role name (already pre-computed in prepare_all_resources)
    export TARGET_ROLE_ARN="acs:ram::${TARGET_ACCOUNT_ID}:role/${target_role_name}"
    log_success "Target account resources setup complete"

    # Create dedicated RAM User for TC-006 cross-account test
    # Source account RAM User only needs sts:AssumeRole permission (NOT KMS)
    # The STS AssumeRole policy is attached separately below
    if ! create_test_ram_user "$tc006_user" ""; then
        record_result "$test_name" "FAIL" "Failed to create test RAM User"
        _tc006_cleanup
        return 1
    fi
    tc006_ak="$_CREATED_USER_AK"
    local tc006_sk="$_CREATED_USER_SK"

    # Attach STS AssumeRole policy scoped to target account Role
    local tc006_sts_policy_doc
    tc006_sts_policy_doc="{\"Version\":\"1\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"sts:AssumeRole\",\"Resource\":\"${TARGET_ROLE_ARN}\"}]}"
    log_info "TC-006: Creating STS AssumeRole policy"
    create_ram_policy "$tc006_sts_policy" "$tc006_sts_policy_doc" "TC-006 STS AssumeRole policy" || \
        log_warning "TC-006: Failed to create STS AssumeRole policy, cross-account assume may fail"
    
    # Attach STS policy with retry
    local attach_retry=0 attach_max=3 attach_delay=3
    while [ $attach_retry -lt $attach_max ]; do
        if aliyun ram AttachPolicyToUser --PolicyType Custom --PolicyName "$tc006_sts_policy" --UserName "$tc006_user" 2>&1; then
            log_success "TC-006: Attached STS AssumeRole policy to user $tc006_user"
            break
        fi
        attach_retry=$((attach_retry + 1))
        if [ $attach_retry -lt $attach_max ]; then
            log_warning "TC-006: AttachPolicyToUser attempt $attach_retry/$attach_max failed, retrying in ${attach_delay}s..."
            sleep "$attach_delay"
            attach_delay=$((attach_delay * 2))
        else
            log_warning "TC-006: Failed to attach STS AssumeRole policy to user $tc006_user after $attach_max attempts"
        fi
    done

    # Validate AK/SK credentials are functional
    local validate_result
    validate_result=$(ALICLOUD_ACCESS_KEY_ID="${tc006_ak}" ALICLOUD_ACCESS_KEY_SECRET="${tc006_sk}" \
        retry_aliyun 3 3 aliyun sts GetCallerIdentity) || {
        log_warning "TC-006: AK/SK invalid, will wait for IAM propagation before retry..."
        sleep 5  # Wait for IAM policy propagation
        
        # Try validation again before recreating user
        validate_result=$(ALICLOUD_ACCESS_KEY_ID="${tc006_ak}" ALICLOUD_ACCESS_KEY_SECRET="${tc006_sk}" \
            retry_aliyun 3 3 aliyun sts GetCallerIdentity) || {
            log_warning "TC-006: AK/SK still invalid after IAM wait, recreating tc006-user..."
            # Ensure cleanup is thorough: detach policies explicitly before deletion
            local _tc006_list_output
            if _tc006_list_output=$(retry_aliyun 3 2 aliyun ram ListPoliciesForUser --UserName "$tc006_user"); then
                echo "$_tc006_list_output" | \
                    jq -r '.Policies.Policy[] | "\(.PolicyType) \(.PolicyName)"' 2>/dev/null | \
                    while IFS=' ' read -r ptype pname; do
                        [ -z "$pname" ] && continue
                        retry_aliyun 3 2 aliyun ram DetachPolicyFromUser --PolicyType "$ptype" --PolicyName "$pname" --UserName "$tc006_user" || true
                    done
            fi
            sleep 5  # Wait for detachment

            cleanup_test_ram_user "$tc006_user" "$tc006_ak" 2>&1 || true
            if ! create_test_ram_user "$tc006_user" ""; then
                record_result "$test_name" "FAIL" "Failed to recreate test RAM User"
                _tc006_cleanup
                return 1
            fi
            tc006_ak="$_CREATED_USER_AK"
            tc006_sk="$_CREATED_USER_SK"
            
            # Wait for new AK/SK to propagate
            sleep 5
        }
    }
    log_success "TC-006: AK/SK credentials validated"

    # Verify AssumeRole path with polling (IAM eventual consistency).
    # GetCallerIdentity only validates AK/SK identity (simple/local), but does NOT
    # verify the AK/SK can call AssumeRole for cross-account access. Newly created
    # RAM user AK/SK may pass GetCallerIdentity immediately but fail AssumeRole
    # due to IAM eventual consistency (InvalidAccessKeyId.NotFound).
    local assume_role_verified=false
    for i in $(seq 1 36); do
        if aliyun sts AssumeRole \
            --RoleArn "$TARGET_ROLE_ARN" \
            --RoleSessionName "tc006-verify" \
            --DurationSeconds 900 \
            --access-key-id "$tc006_ak" \
            --access-key-secret "$tc006_sk" \
            --region "$KMS_REGION" > /dev/null 2>&1; then
            assume_role_verified=true
            log_info "TC-006: AssumeRole verified after $i attempts"
            break
        fi
        log_info "TC-006: AssumeRole not yet available, retrying ($i/36)..."
        sleep 5
    done

    if [ "$assume_role_verified" != "true" ]; then
        log_error "TC-006: AssumeRole verification failed after 180s"
        record_result "$test_name" "FAIL" "AssumeRole verification failed after 180s"
        _tc006_cleanup
        return 1
    fi

    # Additional buffer wait for KMS-level IAM propagation.
    # Even after AssumeRole succeeds, KMS endpoints may have additional
    # eventual consistency delay for the newly assumed role credentials.
    log_info "TC-006: Waiting 15s for KMS IAM propagation..."
    sleep 15

    # Configure Provider with pure AK/SK for cross-account (crossAccountRoleArn in SPC handles AssumeRole)
    kubectl create secret generic alibaba-credentials \
        --from-literal=id="$tc006_ak" \
        --from-literal=secret="$tc006_sk" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1

    # Read auth mode from SPC fixture and configure DaemonSet
    local tc006_auth_mode
    tc006_auth_mode=$(read_env_auth_mode fixtures/spc/test-cross-account-spc.yaml)
    configure_provider_auth "$tc006_auth_mode"
    _AUTH_CONFIGURED=true

    # Apply SPC and Pod
    kubectl apply -f fixtures/spc/test-cross-account-spc.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "SPC creation failed"; _tc006_cleanup; return 1; }
    kubectl apply -f fixtures/pod/test-cross-account-pod.yaml -n "$NAMESPACE" 2>&1 || { record_result "$test_name" "FAIL" "Pod creation failed"; _tc006_cleanup; return 1; }
    
    if ! wait_for_pod_ready "cross-account-test" 120; then
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc006_cleanup
        return 1
    fi
    
    if ! verify_mount "cross-account-test" "$CROSS_ACCOUNT_SECRET_NAME" "cross-account-value"; then
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc006_cleanup
        return 1
    fi
    
    record_result "$test_name" "PASS" "Cross-account authentication successful"
    _tc006_cleanup

    log_step "$test_name complete"
}

# TC-008: JMESPath JSON Parsing
run_tc008_jmespath() {
    local test_name="TC-008: JMESPath JSON Parsing"
    local JMESPATH_SECRET_NAME="tc008-jmespath-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    # should_skip_test 必须在任何副作用之前检查
    if should_skip_test "TC-008"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Ensure auth is configured (reuse or setup minimal)
    if ! ensure_auth_for_feature_tests; then
        record_result "$test_name" "FAIL" "Failed to configure auth"
        return 1
    fi

    if ! check_rotation_enabled; then
        log_warning "CSI Driver rotation/sync not enabled, skipping: $test_name"
        log_warning "TC-009: Please install CSI Driver with --set secrets-store-csi-driver.enableSecretRotation=true --set secrets-store-csi-driver.syncSecret.enabled=true"
        record_result "$test_name" "SKIP" "rotation/sync not enabled"
        return 0
    fi

    # Create KMS Secret for this test
    create_kms_secret "$JMESPATH_SECRET_NAME" '{"username":"testUser","password":"testPassword"}'

    local json_secret_name="$JMESPATH_SECRET_NAME"

    # Prepare SPC (with jmesPath config) and Pod from template files
    local spc_file="/tmp/test-jmespath-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-jmespath-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-jmespath-spc.yaml fixtures/pod/test-jmespath-pod.yaml \
        "$spc_file" "$pod_file" \
        "JMESPATH_SECRET_NAME" "$json_secret_name"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; delete_kms_secret "$JMESPATH_SECRET_NAME"; cleanup_test_resources "TC-008"; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; delete_kms_secret "$JMESPATH_SECRET_NAME"; cleanup_test_resources "TC-008"; return 1; }

    if ! wait_for_pod_ready "jmespath-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        delete_kms_secret "$JMESPATH_SECRET_NAME"
        cleanup_test_resources "TC-008"
        return 1
    fi

    # Verify JMESPath parsing results (mounted volume)
    local username_val
    username_val=$(kubectl exec jmespath-test -n "$NAMESPACE" -- cat /mnt/secrets-store/myUsername 2>&1)
    if [ "${username_val//$'\r'}" != "testUser" ]; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-008: JMESPath parsing failed: expected 'testUser', actual '$username_val'"
        record_result "$test_name" "FAIL" "JMESPath parsing mismatch"
        delete_kms_secret "$JMESPATH_SECRET_NAME"
        cleanup_test_resources "TC-008"
        return 1
    fi

    # Verify synced K8s Secret exists and has correct content (optional, only if secretObjects configured)
    log_info "TC-008: Checking synced K8s Secret..."
    sleep 10
    local synced_k8s_secret="jmespath-synced-secret-${TEST_SUFFIX}"
    if kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" &>/dev/null; then
        local synced_username
        synced_username=$(kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" -o jsonpath='{.data.username}' 2>/dev/null | base64 -d || echo "")
        if [ "${synced_username//$'\r'}" != "testUser" ]; then
            log_warning "TC-008: Synced Secret username mismatch: expected 'testUser', actual '$synced_username'"
        else
            log_success "TC-008: Synced K8s Secret verified"
        fi
    else
        log_info "TC-008: Synced K8s Secret not configured in SPC (skipping synced Secret verification)"
    fi

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "JMESPath JSON parsing correct"
    delete_kms_secret "$JMESPATH_SECRET_NAME"
    cleanup_test_resources "TC-008"
    log_step "$test_name complete"
}

# TC-009: Secret Rotation
run_tc009_rotation() {
    local test_name="TC-009: Secret Rotation"
    local ROTATION_SECRET_NAME="tc009-rotation-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-009"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Ensure auth is configured
    if ! ensure_auth_for_feature_tests; then
        record_result "$test_name" "FAIL" "Failed to configure auth"
        return 1
    fi

    if ! check_rotation_enabled; then
        log_warning "CSI Driver rotation not enabled, skipping: $test_name"
        record_result "$test_name" "SKIP" "rotation not enabled"
        return 0
    fi

    # Create KMS Secret for this test
    create_kms_secret "$ROTATION_SECRET_NAME" "before-rotation"

    local rotation_secret="$ROTATION_SECRET_NAME"

    # Prepare SPC and Pod from template files
    local spc_file="/tmp/test-rotation-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-rotation-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-rotation-spc.yaml fixtures/pod/test-rotation-pod.yaml \
        "$spc_file" "$pod_file" \
        "ROTATION_SECRET_NAME" "$rotation_secret"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; delete_kms_secret "$ROTATION_SECRET_NAME"; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; delete_kms_secret "$ROTATION_SECRET_NAME"; return 1; }

    if ! wait_for_pod_ready "rotation-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        delete_kms_secret "$ROTATION_SECRET_NAME"
        cleanup_test_resources "TC-009"
        return 1
    fi

    # Verify pre-rotation value (mounted volume)
    if ! verify_mount "rotation-test" "$rotation_secret" "before-rotation"; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Initial mount failed"
        delete_kms_secret "$ROTATION_SECRET_NAME"
        cleanup_test_resources "TC-009"
        return 1
    fi

    # Verify synced K8s Secret before rotation (optional, only if secretObjects configured)
    log_info "TC-009: Checking synced K8s Secret before rotation..."
    sleep 10
    local synced_k8s_secret="rotation-synced-secret-${TEST_SUFFIX}"
    if kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" &>/dev/null; then
        local synced_before
        synced_before=$(kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" -o jsonpath='{.data.value}' 2>/dev/null | base64 -d || echo "")
        if [ "${synced_before//$'\r'}" = "before-rotation" ]; then
            log_success "TC-009: Synced K8s Secret verified before rotation"
        else
            log_warning "TC-009: Synced Secret value mismatch before rotation: expected 'before-rotation', actual '$synced_before'"
        fi
    else
        log_info "TC-009: Synced K8s Secret not configured in SPC (skipping synced Secret verification)"
    fi

    # Update KMS Secret to trigger rotation
    log_info "TC-009: Updating KMS Secret to trigger rotation..."
    local put_result
    if ! put_result=$(retry_aliyun 3 3 aliyun kms PutSecretValue --SecretName "$rotation_secret" --SecretData "after-rotation" --VersionId "v2" --RegionId "${KMS_REGION}" \
        --access-key-id "${ALIBABA_ACCESS_KEY_ID}" --access-key-secret "${ALIBABA_ACCESS_KEY_SECRET}"); then
        log_error "TC-009: Failed to update KMS Secret: $put_result"
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Failed to update KMS Secret"
        delete_kms_secret "$ROTATION_SECRET_NAME"
        cleanup_test_resources "TC-009"
        return 1
    fi
    log_success "TC-009: KMS Secret updated to 'after-rotation'"
    
    # Verify rotation with polling (CSI rotationPollPeriod is typically 2m)
    log_info "TC-009: Waiting for CSI Driver to detect and rotate Secret..."
    local rotation_max_wait=300
    local rotation_interval=10
    local rotation_elapsed=0
    local rotated_value=""
    local rotation_detected=false
    
    while [ $rotation_elapsed -lt $rotation_max_wait ]; do
        rotated_value=$(kubectl exec rotation-test -n "$NAMESPACE" -- cat "/mnt/secrets-store/${rotation_secret}" 2>/dev/null || echo "")
        rotated_value="${rotated_value//$'\r'/}"
        
        if [ "$rotated_value" = "after-rotation" ]; then
            log_success "TC-009: Secret rotation detected after ${rotation_elapsed}s"
            rotation_detected=true
            break
        fi
        
        sleep $rotation_interval
        rotation_elapsed=$((rotation_elapsed + rotation_interval))
        
        # Log progress only if value hasn't changed yet
        if [ "$rotated_value" != "after-rotation" ]; then
            log_info "TC-009: Waiting for rotation... (${rotation_elapsed}s/${rotation_max_wait}s), current value: '$rotated_value' (expected: 'after-rotation')"
        fi
    done
    
    # Final verification: ensure rotation actually happened
    if [ "$rotation_detected" = false ] || [ "$rotated_value" != "after-rotation" ]; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-009: Secret rotation failed after ${rotation_max_wait}s"
        log_error "TC-009: Expected 'after-rotation', but got: '$rotated_value'"
        log_error "TC-009: Possible causes:"
        log_error "TC-009:   1. CSI Driver rotation not enabled (enableSecretRotation=true)"
        log_error "TC-009:   2. rotationPollPeriod too long (default 2m)"
        log_error "TC-009:   3. Provider failed to fetch updated Secret"
        record_result "$test_name" "FAIL" "Secret rotation not detected"
        delete_kms_secret "$ROTATION_SECRET_NAME"
        cleanup_test_resources "TC-009"
        return 1
    fi
    
    # Double-check: verify the value is stable (not a transient read)
    sleep 5
    local final_check
    final_check=$(kubectl exec rotation-test -n "$NAMESPACE" -- cat "/mnt/secrets-store/${rotation_secret}" 2>/dev/null || echo "")
    final_check="${final_check//$'\r'/}"
    
    if [ "$final_check" != "after-rotation" ]; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-009: Rotation value not stable: '$final_check' (expected: 'after-rotation')"
        record_result "$test_name" "FAIL" "Rotation value unstable"
        delete_kms_secret "$ROTATION_SECRET_NAME"
        cleanup_test_resources "TC-009"
        return 1
    fi
    
    log_success "TC-009: Secret rotation verified and stable"

    # Verify synced K8s Secret after rotation (optional, only if secretObjects configured)
    log_info "TC-009: Checking synced K8s Secret after rotation..."
    if kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" &>/dev/null; then
        local synced_after
        synced_after=$(kubectl get secret "$synced_k8s_secret" -n "$NAMESPACE" -o jsonpath='{.data.value}' 2>/dev/null | base64 -d || echo "")
        if [ "${synced_after//$'\r'}" = "after-rotation" ]; then
            log_success "TC-009: Synced K8s Secret rotated successfully"
        else
            log_warning "TC-009: Synced Secret not yet rotated: expected 'after-rotation', actual '$synced_after'"
        fi
    else
        log_info "TC-009: Synced K8s Secret not configured in SPC (rotation only verified on mounted volume)"
    fi

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "Secret rotation verified"
    delete_kms_secret "$ROTATION_SECRET_NAME"
    cleanup_test_resources "TC-009"
    log_step "$test_name complete"
}

# TC-010: K8s Secret Sync (secretObjects)
run_tc010_secret_sync() {
    local test_name="TC-010: K8s Secret Sync"
    local SYNC_SECRET_NAME="tc010-sync-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-010"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Ensure auth is configured
    if ! ensure_auth_for_feature_tests; then
        record_result "$test_name" "FAIL" "Failed to configure auth"
        return 1
    fi

    if ! check_rotation_enabled; then
        log_warning "CSI Driver syncSecret not enabled, skipping: $test_name"
        record_result "$test_name" "SKIP" "syncSecret not enabled"
        return 0
    fi

    # Create KMS Secret for this test
    create_kms_secret "$SYNC_SECRET_NAME" "sync-test-value"

    local sync_secret="$SYNC_SECRET_NAME"

    local k8s_secret_name="synced-secret-${TEST_SUFFIX}"

    # Prepare SPC (with secretObjects) and Pod from template files
    local spc_file="/tmp/test-sync-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-sync-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-secret-sync-spc.yaml fixtures/pod/test-secret-sync-pod.yaml \
        "$spc_file" "$pod_file" \
        "SYNC_SECRET_NAME" "$sync_secret" \
        "K8S_SYNC_SECRET_NAME" "$k8s_secret_name"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; delete_kms_secret "$SYNC_SECRET_NAME"; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; delete_kms_secret "$SYNC_SECRET_NAME"; return 1; }

    if ! wait_for_pod_ready "sync-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        delete_kms_secret "$SYNC_SECRET_NAME"
        cleanup_test_resources "TC-010"
        return 1
    fi

    # Verify K8s Secret is synced
    log_info "TC-010: Verifying K8s Secret sync..."
    sleep 10
    if ! kubectl get secret "$k8s_secret_name" -n "$NAMESPACE" &>/dev/null; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-010: K8s Secret not synced: $k8s_secret_name"
        record_result "$test_name" "FAIL" "K8s Secret not created"
        delete_kms_secret "$SYNC_SECRET_NAME"
        cleanup_test_resources "TC-010"
        return 1
    fi

    local synced_val
    synced_val=$(kubectl get secret "$k8s_secret_name" -n "$NAMESPACE" -o jsonpath='{.data.value}' 2>/dev/null | base64 -d)
    if [ "$synced_val" != "sync-test-value" ]; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-010: Sync value mismatch: expected 'sync-test-value', actual '$synced_val'"
        record_result "$test_name" "FAIL" "sync value mismatch"
        delete_kms_secret "$SYNC_SECRET_NAME"
        cleanup_test_resources "TC-010"
        return 1
    fi
    log_success "TC-010: K8s Secret sync verified"

    # Verify mounted volume also has correct content
    log_info "TC-010: Verifying mounted volume..."
    local mounted_val
    mounted_val=$(kubectl exec sync-test -n "$NAMESPACE" -- cat "/mnt/secrets-store/${sync_secret}" 2>/dev/null || echo "")
    mounted_val="${mounted_val//$'\r'/}"
    if [ "$mounted_val" != "sync-test-value" ]; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-010: Mounted volume value mismatch: expected 'sync-test-value', actual '$mounted_val'"
        record_result "$test_name" "FAIL" "mounted volume value mismatch"
        delete_kms_secret "$SYNC_SECRET_NAME"
        cleanup_test_resources "TC-010"
        return 1
    fi
    log_success "TC-010: Mounted volume verified"

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "K8s Secret sync verified"
    delete_kms_secret "$SYNC_SECRET_NAME"
    cleanup_test_resources "TC-010"
    log_step "$test_name complete"
}

# TC-011: Post-Deletion Secret Cleanup
run_tc011_cleanup() {
    local test_name="TC-011: Post-Deletion Secret Cleanup"
    local CLEANUP_SECRET_NAME="tc011-cleanup-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-011"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Ensure auth is configured
    if ! ensure_auth_for_feature_tests; then
        record_result "$test_name" "FAIL" "Failed to configure auth"
        return 1
    fi

    if ! check_rotation_enabled; then
        log_warning "CSI Driver syncSecret not enabled, skipping: $test_name"
        record_result "$test_name" "SKIP" "syncSecret not enabled"
        return 0
    fi

    # Create KMS Secret for this test
    create_kms_secret "$CLEANUP_SECRET_NAME" "cleanup-test-value"

    local cleanup_secret="$CLEANUP_SECRET_NAME"

    local k8s_secret_name="cleanup-synced-${TEST_SUFFIX}"

    # Prepare SPC (with secretObjects) and Pod from template files
    local spc_file="/tmp/test-cleanup-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-cleanup-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-cleanup-spc.yaml fixtures/pod/test-cleanup-pod.yaml \
        "$spc_file" "$pod_file" \
        "CLEANUP_SECRET_NAME" "$cleanup_secret" \
        "K8S_CLEANUP_SECRET_NAME" "$k8s_secret_name"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; delete_kms_secret "$CLEANUP_SECRET_NAME"; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; delete_kms_secret "$CLEANUP_SECRET_NAME"; return 1; }

    if ! wait_for_pod_ready "cleanup-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        delete_kms_secret "$CLEANUP_SECRET_NAME"
        cleanup_test_resources "TC-011"
        return 1
    fi

    # Confirm K8s Secret is synced and verify content
    sleep 10
    if ! kubectl get secret "$k8s_secret_name" -n "$NAMESPACE" &>/dev/null; then
        rm -f "$spc_file" "$pod_file"
        log_error "TC-011: K8s Secret not synced, cannot test cleanup"
        record_result "$test_name" "FAIL" "Prerequisite failed"
        delete_kms_secret "$CLEANUP_SECRET_NAME"
        cleanup_test_resources "TC-011"
        return 1
    fi

    # Verify synced Secret content before cleanup
    local synced_before_cleanup
    synced_before_cleanup=$(kubectl get secret "$k8s_secret_name" -n "$NAMESPACE" -o jsonpath='{.data.value}' 2>/dev/null | base64 -d || echo "")
    if [ "${synced_before_cleanup//$'\r'}" = "cleanup-test-value" ]; then
        log_success "TC-011: Synced K8s Secret content verified before cleanup"
    else
        log_warning "TC-011: Synced Secret value mismatch: expected 'cleanup-test-value', actual '$synced_before_cleanup'"
    fi

    # Verify mounted volume content before cleanup
    log_info "TC-011: Verifying mounted volume before cleanup..."
    local mounted_before_cleanup
    mounted_before_cleanup=$(kubectl exec cleanup-test -n "$NAMESPACE" -- cat "/mnt/secrets-store/${cleanup_secret}" 2>/dev/null || echo "")
    mounted_before_cleanup="${mounted_before_cleanup//$'\r'/}"
    if [ "$mounted_before_cleanup" = "cleanup-test-value" ]; then
        log_success "TC-011: Mounted volume content verified before cleanup"
    else
        log_warning "TC-011: Mounted volume value mismatch: expected 'cleanup-test-value', actual '$mounted_before_cleanup'"
    fi

    # Delete Pod
    log_info "TC-011: Deleting Pod to trigger Secret cleanup..."
    kubectl delete pod cleanup-test -n "$NAMESPACE" --ignore-not-found=true 2>&1
    sleep 5

    # Verify K8s Secret is cleaned up
    log_info "TC-011: Waiting for K8s Secret cleanup (timeout 60s)..."
    local elapsed=0
    while [ $elapsed -lt 60 ]; do
        if ! kubectl get secret "$k8s_secret_name" -n "$NAMESPACE" &>/dev/null; then
            rm -f "$spc_file" "$pod_file"
            record_result "$test_name" "PASS" "K8s Secret cleaned up after Pod deletion"
            delete_kms_secret "$CLEANUP_SECRET_NAME"
            cleanup_test_resources "TC-011"
            log_step "$test_name complete"
            return 0
        fi
        sleep 5
        elapsed=$((elapsed + 5))
    done

    rm -f "$spc_file" "$pod_file"
    log_error "TC-011: K8s Secret not cleaned up: $k8s_secret_name"
    record_result "$test_name" "FAIL" "Secret not cleaned up"
    delete_kms_secret "$CLEANUP_SECRET_NAME"
    cleanup_test_resources "TC-011"
    return 1
}

# ============================================================================
# Report Generation and Cleanup (~100 lines)
# ============================================================================

# Create Node Publish Secret (K8s Secret containing AK/SK)
create_node_publish_secret() {
    local secret_name="$1"
    local ns="$2"
    local ak="$3"
    local sk="$4"
    log_info "Creating nodePublishSecret: $secret_name"
    kubectl create secret generic "$secret_name" \
        --from-literal=access_key="$ak" \
        --from-literal=access_secret="$sk" \
        -n "$ns" \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1
}

# TC-003: RAM Role Authentication (AK/SK + RoleArn)
run_tc003_ram_role() {
    local test_name="TC-003: RAM Role Authentication"
    local RAM_ROLE_SECRET_NAME="tc003-ram-role-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-003"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local variables for TC-003 resources (declared early for cleanup function)
    local tc003_role_name="tc003-role-${TEST_SUFFIX}"
    local tc003_assume_policy_name="tc003-assume-role-${TEST_SUFFIX}"
    local ram_user="tc003-user-${TEST_SUFFIX}"
    local provider_ak=""

    # Local cleanup function for TC-003 resources
    _tc003_cleanup() {
        log_info "TC-003: Cleaning up resources..."
        cleanup_test_ram_user "$ram_user" "${provider_ak}"
        cleanup_ram_role "$tc003_role_name" || true
        delete_ram_policy "$tc003_assume_policy_name" || true
        delete_kms_secret "$RAM_ROLE_SECRET_NAME"
        cleanup_test_resources "TC-003"
        log_info "TC-003: Cleanup complete"
    }

    # Clear all auth and configure fresh for this test
    clear_all_auth

    # Create KMS Secret for this test
    create_kms_secret "$RAM_ROLE_SECRET_NAME" "ram-role-test-value"

    # Step 1: Create RAM Role with unified KMS Policy (trust policy updated after user creation)
    local tc003_role_arn="acs:ram::${SOURCE_ACCOUNT_ID}:role/${tc003_role_name}"
    local tc003_tmp_trust
    tc003_tmp_trust=$(generate_trust_policy "ram" "$SOURCE_ACCOUNT_ID")
    if ! create_ram_role "$tc003_role_name" "$tc003_tmp_trust" "TC-003 dedicated role" "$KMS_POLICY_NAME"; then
        log_error "TC-003: Failed to create dedicated RAM Role"
        record_result "$test_name" "FAIL" "Failed to create TC-003 RAM Role"
        _tc003_cleanup
        return 1
    fi
    # Verify KMS policy attachment
    if ! verify_role_policy_attachment "$tc003_role_name" "$KMS_POLICY_NAME"; then
        log_error "TC-003: KMS policy $KMS_POLICY_NAME not verified on role $tc003_role_name"
        record_result "$test_name" "FAIL" "KMS policy attachment verification failed"
        _tc003_cleanup
        return 1
    fi

    # Step 2: Create STS AssumeRole policy scoped to specific Role ARN
    local tc003_assume_policy_doc
    tc003_assume_policy_doc="{\"Statement\":[{\"Action\":\"sts:AssumeRole\",\"Effect\":\"Allow\",\"Resource\":\"acs:ram:*:${SOURCE_ACCOUNT_ID}:role/${tc003_role_name}\"}],\"Version\":\"1\"}"
    if ! create_ram_policy "$tc003_assume_policy_name" "$tc003_assume_policy_doc" "TC-003 AssumeRole Policy"; then
        record_result "$test_name" "FAIL" "RAM policy creation failed"
        _tc003_cleanup
        return 1
    fi

    # Step 3: Create RAM User, grant STS AssumeRole policy (NOT KMS Policy)
    if ! create_test_ram_user "$ram_user" "$tc003_assume_policy_name"; then
        record_result "$test_name" "FAIL" "Failed to create test RAM User"
        _tc003_cleanup
        return 1
    fi
    provider_ak="$_CREATED_USER_AK"
    local provider_sk="$_CREATED_USER_SK"

    # Step 4: Update RAM Role trust policy to reference specific RAM User
    local tc003_trust_policy
    tc003_trust_policy="{\"Statement\":[{\"Action\":\"sts:AssumeRole\",\"Effect\":\"Allow\",\"Principal\":{\"RAM\":[\"acs:ram::${SOURCE_ACCOUNT_ID}:user/${ram_user}\"]}}],\"Version\":\"1\"}"
    update_ram_role_trust_policy "$tc003_role_name" "$tc003_trust_policy" || \
        log_warning "TC-003: Failed to update trust policy, tests may fail"

    # Step 5: Create K8s Secret with AK/SK/RoleArn
    kubectl create secret generic alibaba-credentials \
        --from-literal=id="${provider_ak}" \
        --from-literal=secret="${provider_sk}" \
        --from-literal=rolearn="$tc003_role_arn" \
        -n kube-system \
        --dry-run=client -o yaml | kubectl apply -f - 2>&1

    configure_provider_auth "aksk_role"
    _AUTH_CONFIGURED=true

    # Wait for IAM propagation (verify AssumeRole path, not just GetCallerIdentity)
    # GetCallerIdentity only verifies AK/SK identity (a local/simple check),
    # but does NOT verify the AK/SK can call AssumeRole for the target role.
    # Newly created RAM user AK/SK may pass GetCallerIdentity immediately but fail
    # AssumeRole due to IAM eventual consistency (InvalidAccessKeyId.NotFound).
    # We must verify the actual AssumeRole path that the Provider will use.
    log_info "TC-003: Waiting for IAM propagation (AssumeRole verification)..."
    local _assume_role_ready=false
    for _iam_i in $(seq 1 36); do
        if aliyun sts AssumeRole \
            --access-key-id "${provider_ak}" \
            --access-key-secret "${provider_sk}" \
            --RoleArn "${tc003_role_arn}" \
            --RoleSessionName "tc003-propagation-check" &>/dev/null; then
            log_success "TC-003: AssumeRole propagation verified after $_iam_i attempts"
            _assume_role_ready=true
            break
        fi
        sleep 5
    done
    if [ "$_assume_role_ready" != "true" ]; then
        log_warning "TC-003: AssumeRole propagation timeout after 180s, tests may fail"
    fi

    # Additional buffer wait for KMS-level IAM propagation.
    # Even after AssumeRole succeeds, KMS endpoints may have additional
    # eventual consistency delay for the newly assumed role credentials.
    if [ "$_assume_role_ready" = "true" ]; then
        log_info "TC-003: waiting 15s for KMS-level IAM propagation buffer"
        sleep 15
    fi

    # Prepare SPC and Pod from template files
    local spc_file="/tmp/test-ram-role-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-ram-role-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-ram-role-spc.yaml fixtures/pod/test-ram-role-pod.yaml \
        "$spc_file" "$pod_file" \
        "RAM_ROLE_SECRET_NAME" "$RAM_ROLE_SECRET_NAME"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; _tc003_cleanup; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; _tc003_cleanup; return 1; }

    if ! wait_for_pod_ready "ram-role-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc003_cleanup
        return 1
    fi

    if ! verify_mount "ram-role-test" "$RAM_ROLE_SECRET_NAME" "ram-role-test-value"; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc003_cleanup
        return 1
    fi

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "RAM Role authentication successful"
    _tc003_cleanup
    log_step "$test_name complete"
}

# TC-004: Node Publish Secret Authentication
run_tc004_node_publish_secret() {
    local test_name="TC-004: Node Publish Secret Authentication"
    local NODE_PUB_SECRET_NAME="tc004-node-pub-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-004"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local variables for TC-004 resources (declared early for cleanup function)
    local tc004_user="tc004-user-${TEST_SUFFIX}"
    local tc004_ak=""

    # Local cleanup function for TC-004 resources
    _tc004_cleanup() {
        log_info "TC-004: Cleaning up resources..."
        cleanup_test_ram_user "$tc004_user" "${tc004_ak}"
        kubectl delete secret node-publish-credentials -n "$NAMESPACE" --ignore-not-found=true 2>/dev/null || true
        delete_kms_secret "$NODE_PUB_SECRET_NAME"
        cleanup_test_resources "TC-004"
        log_info "TC-004: Cleanup complete"
    }

    # Clear all auth and configure fresh for this test
    clear_all_auth

    # Create KMS Secret for this test
    create_kms_secret "$NODE_PUB_SECRET_NAME" "node-pub-test-value"

    # Step 1: Create RAM User, grant unified KMS Policy
    if ! create_test_ram_user "$tc004_user" "$KMS_POLICY_NAME"; then
        record_result "$test_name" "FAIL" "Failed to create test RAM User"
        _tc004_cleanup
        return 1
    fi
    tc004_ak="$_CREATED_USER_AK"
    local tc004_sk="$_CREATED_USER_SK"

    # Verify KMS Policy binding
    if ! verify_user_policy_attachment "$tc004_user" "$KMS_POLICY_NAME"; then
        log_error "TC-004: KMS Policy $KMS_POLICY_NAME could not be verified on user $tc004_user"
        record_result "$test_name" "FAIL" "KMS policy binding verification failed"
        _tc004_cleanup
        return 1
    fi
    log_success "TC-004: KMS Policy verified"
    sleep 5  # Wait for IAM permission propagation

    # Step 2: Create nodePublishSecret with AK/SK for Pod-level auth
    create_node_publish_secret "node-publish-credentials" "$NAMESPACE" "$tc004_ak" "$tc004_sk"

    # DaemonSet stays in "none" mode - authentication is done via nodePublishSecret only
    log_info "TC-004: DaemonSet remains in none mode; using nodePublishSecret for auth"

    # Prepare SPC and Pod from template files
    local spc_file="/tmp/test-node-publish-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-node-publish-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-node-publish-spc.yaml fixtures/pod/test-node-publish-pod.yaml \
        "$spc_file" "$pod_file" \
        "NODE_PUB_SECRET_NAME" "$NODE_PUB_SECRET_NAME"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; _tc004_cleanup; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; _tc004_cleanup; return 1; }

    if ! wait_for_pod_ready "node-publish-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc004_cleanup
        return 1
    fi

    if ! verify_mount "node-publish-test" "$NODE_PUB_SECRET_NAME" "node-pub-test-value"; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc004_cleanup
        return 1
    fi

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "Node Publish Secret authentication successful"
    _tc004_cleanup
    log_step "$test_name complete"
}

# TC-007: ECS RAM Role Authentication
run_tc007_ecs_ram_role() {
    local test_name="TC-007: ECS RAM Role Authentication"
    local ECS_ROLE_SECRET_NAME="tc007-ecs-role-secret-${TEST_SUFFIX}"
    log_step "Starting $test_name"

    if should_skip_test "TC-007"; then
        log_warning "Skipping: $test_name"
        record_result "$test_name" "SKIP" "Skipped by user"
        return 0
    fi

    # Local variables for TC-007 resources (declared early for cleanup function)
    local worker_role_name=""

    # Local cleanup function for TC-007 resources
    _tc007_cleanup() {
        log_info "TC-007: Cleaning up resources..."
        if [[ -n "$worker_role_name" ]]; then
            # Detach policy with retry
            local detach_retry=0 detach_max=3 detach_delay=2
            while [ $detach_retry -lt $detach_max ]; do
                if aliyun ram DetachPolicyFromRole --PolicyType Custom --PolicyName "$KMS_POLICY_NAME" --RoleName "$worker_role_name" 2>&1; then
                    break
                fi
                detach_retry=$((detach_retry + 1))
                if [ $detach_retry -lt $detach_max ]; then
                    sleep "$detach_delay"
                fi
            done
        fi
        delete_kms_secret "$ECS_ROLE_SECRET_NAME"
        cleanup_test_resources "TC-007"
        log_info "TC-007: Cleanup complete"
    }

    # Clear all auth and configure fresh for this test
    clear_all_auth

    # Create KMS Secret for this test
    create_kms_secret "$ECS_ROLE_SECRET_NAME" "ecs-role-test-value"

    local -a extra_args=()

    # Step 1: Fetch WorkerRole via cluster API (DescribeClusterDetail) with retry for DNS resilience
    log_info "TC-007: Fetching WorkerRole..."
    local cluster_detail
    local tc007_query_max_retries=3 tc007_query_delay=5
    for ((tc007_qry=1; tc007_qry<=tc007_query_max_retries; tc007_qry++)); do
        if cluster_detail=$(aliyun cs DescribeClusterDetail --ClusterId "${CLUSTER_ID}" 2>/dev/null); then
            break
        fi
        if [[ $tc007_qry -lt $tc007_query_max_retries ]]; then
            log_warning "TC-007: DescribeClusterDetail failed (attempt $tc007_qry/$tc007_query_max_retries), retrying in ${tc007_query_delay}s..."
            sleep "$tc007_query_delay"
        else
            log_error "TC-007: Failed to get cluster details for cluster ${CLUSTER_ID} after $tc007_query_max_retries attempts"
            record_result "$test_name" "FAIL" "Failed to get cluster details"
            return 1
        fi
    done

    worker_role_name=$(echo "$cluster_detail" | jq -r '.worker_ram_role_name // empty')
    if [[ -z "$worker_role_name" ]]; then
        log_error "TC-007: WorkerRole not found in cluster details (field: worker_ram_role_name)"
        record_result "$test_name" "FAIL" "WorkerRole not found"
        return 1
    fi

    # Step 2: Attach KMS Policy to WorkerRole (idempotent, with retry)
    log_info "TC-007: Attaching KMS Policy to WorkerRole..."
    local attach_retry=0 attach_max=3 attach_delay=3
    local attach_output
    while [ $attach_retry -lt $attach_max ]; do
        if attach_output=$(aliyun ram AttachPolicyToRole \
            --PolicyType Custom \
            --PolicyName "${KMS_POLICY_NAME}" \
            --RoleName "${worker_role_name}" 2>&1); then
            log_success "TC-007: KMS Policy attached to WorkerRole"
            break
        else
            # Already attached is fine (idempotent)
            if echo "$attach_output" | grep -qi "EntityAlreadyExists\|already attached\|already exist"; then
                log_success "TC-007: KMS Policy already attached to WorkerRole"
                break
            fi
            attach_retry=$((attach_retry + 1))
            if [ $attach_retry -lt $attach_max ]; then
                log_warning "TC-007: AttachPolicyToRole attempt $attach_retry/$attach_max failed, retrying in ${attach_delay}s..."
                sleep "$attach_delay"
                attach_delay=$((attach_delay * 2))
            else
                log_error "TC-007: AttachPolicyToRole failed after $attach_max attempts: $attach_output"
                record_result "$test_name" "FAIL" "AttachPolicyToRole failed"
                return 1
            fi
        fi
    done

    # Wait for IAM permission propagation
    sleep 5

    # DaemonSet doesn't need auth env vars — ECS RAM Role is fallback auth
    configure_provider_auth "none"
    _AUTH_CONFIGURED=true

    # Prepare SPC and Pod from template files
    local spc_file="/tmp/test-ecs-ram-role-spc-${TEST_SUFFIX}.yaml"
    local pod_file="/tmp/test-ecs-ram-role-pod-${TEST_SUFFIX}.yaml"
    prepare_test_yaml fixtures/spc/test-ecs-ram-role-spc.yaml fixtures/pod/test-ecs-ram-role-pod.yaml \
        "$spc_file" "$pod_file" \
        "ECS_ROLE_SECRET_NAME" "$ECS_ROLE_SECRET_NAME"

    kubectl apply -f "$spc_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "SPC creation failed"; _tc007_cleanup; return 1; }
    kubectl apply -f "$pod_file" -n "$NAMESPACE" 2>&1 || { rm -f "$spc_file" "$pod_file"; record_result "$test_name" "FAIL" "Pod creation failed"; _tc007_cleanup; return 1; }

    if ! wait_for_pod_ready "ecs-ram-role-test" 120; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Pod not ready"
        _tc007_cleanup
        return 1
    fi

    if ! verify_mount "ecs-ram-role-test" "$ECS_ROLE_SECRET_NAME" "ecs-role-test-value"; then
        rm -f "$spc_file" "$pod_file"
        record_result "$test_name" "FAIL" "Secret mount failed"
        _tc007_cleanup
        return 1
    fi

    rm -f "$spc_file" "$pod_file"
    record_result "$test_name" "PASS" "ECS RAM Role authentication successful"
    _tc007_cleanup
    log_step "$test_name complete"
}

cleanup_all() {
    if [ "$RESOURCES_CLEANED" = "true" ]; then
        return 0
    fi
    RESOURCES_CLEANED=true
    
    log_step "Cleaning up all resources"
    
    # Only collect diagnostics if not already collected by handle_error
    if [ -z "${_DIAGNOSTICS_COLLECTED:-}" ]; then
        collect_diagnostics "pre-cleanup"
        _DIAGNOSTICS_COLLECTED=true
    fi
    
    # ========================================================================
    # 1. Clean up Kubernetes resources
    # ========================================================================
    log_info "Cleaning up Kubernetes resources..."
    
    # Delete test pods and SPCs by label
    kubectl delete pod -l test-case -n "$NAMESPACE" --ignore-not-found=true --timeout=60s 2>/dev/null || true
    kubectl delete secretproviderclass -l test-case -n "$NAMESPACE" --ignore-not-found=true --timeout=60s 2>/dev/null || true
    
    # Delete alibaba-credentials Secret in both namespaces
    kubectl delete secret alibaba-credentials -n kube-system --ignore-not-found=true --timeout=30s 2>/dev/null || true
    kubectl delete secret alibaba-credentials -n "$NAMESPACE" --ignore-not-found=true --timeout=30s 2>/dev/null || true
    
    # Delete any remaining resources with test-case label
    kubectl delete all -l test-case -n "$NAMESPACE" --ignore-not-found=true --timeout=60s 2>/dev/null || true
    
    sleep 5
    log_success "Kubernetes resources cleanup complete"
    
    # ========================================================================
    # 2. Uninstall Helm chart and clean up hook resources
    # ========================================================================
    log_info "Uninstalling Helm chart..."
    helm uninstall csi-secrets-store-provider-alibabacloud -n kube-system 2>/dev/null || true
    # Clean up leftover pre-install hook Jobs (not deleted on hook failure)
    kubectl delete job -n kube-system secrets-store-csi-driver-upgrade-crds --ignore-not-found=true 2>/dev/null || true
    log_success "Helm chart and hook resources cleaned up"

    # ========================================================================
    # 3. Restore YAML files
    # ========================================================================
    log_info "Restoring YAML files..."
    restore_yaml_files

    # Clean up temporary YAML files
    rm -f /tmp/test-*-spc-*.yaml /tmp/test-*-pod-*.yaml /tmp/test-*-temp.yaml 2>/dev/null || true

    # ========================================================================
    # 4. Delete global shared KMS Policy
    # ========================================================================
    log_info "Deleting global KMS Policy: $KMS_POLICY_NAME"
    delete_ram_policy "$KMS_POLICY_NAME" || true
    
    # ========================================================================
    # 5. Clean up fallback user (created by ensure_auth_for_feature_tests)
    # ========================================================================
    if [[ -n "$FALLBACK_USER" ]]; then
        log_info "Cleaning up fallback RAM User: $FALLBACK_USER"
        cleanup_test_ram_user "$FALLBACK_USER" "$FALLBACK_AK" || true
    fi
    
    # ========================================================================
    # 6. Final verification
    # ========================================================================
    log_info "Verifying cleanup completion..."
    local remaining_pods
    remaining_pods=$(kubectl get pod -l test-case -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    if [ "$remaining_pods" -gt 0 ]; then
        log_warning "$remaining_pods test pods still remain (may be terminating)"
    fi
    
    # Verify RAM resources cleanup
    log_info "Verifying RAM resource cleanup..."
    local remaining_roles
    local list_retry=0 list_max=3 list_delay=2
    while [ $list_retry -lt $list_max ]; do
        remaining_roles=$(aliyun ram ListRoles --RolePrefix "tc" 2>/dev/null | jq -r ".Roles.Role[] | select(.RoleName | test(\"-${TEST_SUFFIX}$\")) | .RoleName" 2>/dev/null | wc -l)
        if [ -n "$remaining_roles" ]; then
            break
        fi
        list_retry=$((list_retry + 1))
        if [ $list_retry -lt $list_max ]; then
            sleep "$list_delay"
        fi
    done
    if [ "$remaining_roles" -gt 0 ]; then
        log_warning "$remaining_roles test RAM Roles still exist"
        retry_aliyun 2 2 aliyun ram ListRoles --RolePrefix "tc" 2>/dev/null | jq -r ".Roles.Role[] | select(.RoleName | test(\"-${TEST_SUFFIX}$\")) | .RoleName" 2>/dev/null | while IFS= read -r role; do
            log_warning "  Remaining role: $role"
        done
    fi
    
    local remaining_users
    list_retry=0
    while [ $list_retry -lt $list_max ]; do
        remaining_users=$(aliyun ram ListUsers 2>/dev/null | jq -r ".Users.User[] | select(.UserName | test(\"-${TEST_SUFFIX}$\")) | .UserName" 2>/dev/null | wc -l)
        if [ -n "$remaining_users" ]; then
            break
        fi
        list_retry=$((list_retry + 1))
        if [ $list_retry -lt $list_max ]; then
            sleep "$list_delay"
        fi
    done
    if [ "$remaining_users" -gt 0 ]; then
        log_warning "$remaining_users test RAM Users still exist"
        retry_aliyun 2 2 aliyun ram ListUsers 2>/dev/null | jq -r ".Users.User[] | select(.UserName | test(\"-${TEST_SUFFIX}$\")) | .UserName" 2>/dev/null | while IFS= read -r user; do
            log_warning "  Remaining user: $user"
        done
    fi
    
    local remaining_policies
    list_retry=0
    while [ $list_retry -lt $list_max ]; do
        remaining_policies=$(aliyun ram ListPolicy --PolicyType Custom 2>/dev/null | jq -r ".Policies.Policy[] | select(.PolicyName | test(\"-${TEST_SUFFIX}$\")) | .PolicyName" 2>/dev/null | wc -l)
        if [ -n "$remaining_policies" ]; then
            break
        fi
        list_retry=$((list_retry + 1))
        if [ $list_retry -lt $list_max ]; then
            sleep "$list_delay"
        fi
    done
    if [ "$remaining_policies" -gt 0 ]; then
        log_warning "$remaining_policies test RAM Policies still exist"
        retry_aliyun 2 2 aliyun ram ListPolicy --PolicyType Custom 2>/dev/null | jq -r ".Policies.Policy[] | select(.PolicyName | test(\"-${TEST_SUFFIX}$\")) | .PolicyName" 2>/dev/null | while IFS= read -r policy; do
            log_warning "  Remaining policy: $policy"
        done
    fi
    
    log_success "Resource cleanup completed successfully"
}

collect_diagnostics() {
    local trigger="${1:-unknown}"
    local diag_dir="${LOG_DIR}/diagnostics-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$diag_dir"

    log_step "Collecting diagnostics information"
    log_info "Trigger: $trigger, output: $diag_dir"
    
    # 1. Pod status (use --all-containers to avoid container-name mismatch across pod types)
    kubectl get pods -n kube-system -l app=secrets-store-csi-driver -o wide > "$diag_dir/pods.txt" 2>&1 || true
    kubectl get pods -n "$NAMESPACE" -o wide > "$diag_dir/test-pods.txt" 2>&1 || true
    
    # 2. DaemonSet status
    kubectl get daemonset -n kube-system secrets-store-csi-driver -o yaml > "$diag_dir/daemonset.yaml" 2>&1 || true
    
    # 3. CSI Driver logs (omit -c to avoid container name mismatch; --all-containers is safe)
    kubectl logs -n kube-system -l app=secrets-store-csi-driver --all-containers --tail=100 > "$diag_dir/provider-logs.txt" 2>&1 || true
    
    # 4. SecretProviderClass
    kubectl get secretproviderclass -A -o yaml > "$diag_dir/spc.yaml" 2>&1 || true
    
    # 5. Events
    kubectl get events -n kube-system --sort-by='.lastTimestamp' | tail -50 > "$diag_dir/events.txt" 2>&1 || true
    kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' | tail -50 > "$diag_dir/test-events.txt" 2>&1 || true
    
    # 6. Secrets
    kubectl get secrets -n "$NAMESPACE" > "$diag_dir/secrets.txt" 2>&1 || true
    
    log_info "Diagnostics collected to $diag_dir"
}

generate_report() {
    log_step "Generating test report"
    
    # Debug: Log report generation status
    log_info "Test results count: ${#TEST_RESULTS[@]}"
    log_info "TOTAL=$TOTAL, PASSED=$PASSED, FAILED=$FAILED, SKIPPED=$SKIPPED"

    local end_time=$(date +%s)
    local duration=$((end_time - START_TIME))
    local minutes=$((duration / 60))
    local seconds=$((duration % 60))
    
    local pass_rate=0
    if [ $((TOTAL - SKIPPED)) -gt 0 ]; then
        pass_rate=$((PASSED * 100 / (TOTAL - SKIPPED)))
    fi
    
    local report_file="$LOG_DIR/test-report-$(date +%Y%m%d-%H%M%S).md"
    
    cat > "$report_file" <<EOF
# Test Report

## Basic Information
- **Test Time**: $(date '+%Y-%m-%d %H:%M:%S')
- **Cluster ID**: $CLUSTER_ID
- **Source Account**: $SOURCE_ACCOUNT_ID
- **Target Account**: ${TARGET_ACCOUNT_ID:-Not configured}
- **Test Namespace**: $NAMESPACE
- **Total Duration**: ${minutes}m ${seconds}s

## Test Results

| Test Case | Status | Description |
|---------|------|------|
EOF
    
    for result in "${TEST_RESULTS[@]}"; do
        IFS='|' read -r timestamp test_name status message <<< "$result"
        local icon="⏳"
        if [ "$status" = "PASS" ]; then icon="✅"; fi
        if [ "$status" = "FAIL" ]; then icon="❌"; fi
        if [ "$status" = "SKIP" ]; then icon="⏭️"; fi
        echo "| $test_name | $icon $status | $message |" >> "$report_file"
    done
    
    cat >> "$report_file" <<EOF

## Statistics
- **Total**: $TOTAL
- **Passed**: $PASSED
- **Failed**: $FAILED
- **Skipped**: $SKIPPED
- **Pass Rate**: ${pass_rate}%

## Conclusion
EOF
    
    if [ $FAILED -eq 0 ]; then
        echo "✅ All tests passed" >> "$report_file"
    else
        echo "❌ Some tests failed, need to fix and re-run" >> "$report_file"
    fi
    
    # Output to console
    echo ""
    echo "============================================================================"
    echo -e "${BOLD}                         Test Report${NC}"
    echo "============================================================================"
    echo ""
    echo "Test Time: $(date '+%Y-%m-%d %H:%M:%S')"
    echo "Cluster ID: $CLUSTER_ID"
    echo "Test Namespace: $NAMESPACE"
    echo "Total Duration: ${minutes}m ${seconds}s"
    echo ""
    echo "----------------------------------------------------------------------------"
    echo "Test Results Summary:"
    echo "----------------------------------------------------------------------------"
    echo -e "  Total:    $TOTAL"
    echo -e "  ${GREEN}Passed:   $PASSED${NC}"
    echo -e "  ${RED}Failed:   $FAILED${NC}"
    echo -e "  ${YELLOW}Skipped:  $SKIPPED${NC}"
    echo ""
    echo "Pass Rate: ${pass_rate}%"
    echo ""
    
    # Print individual test results
    if [ ${#TEST_RESULTS[@]} -gt 0 ]; then
        echo "----------------------------------------------------------------------------"
        printf "%-40s | %-8s | %s\n" "Test Case" "Status" "Notes"
        echo "----------------------------------------------------------------------------"
        
        for result in "${TEST_RESULTS[@]}"; do
            IFS='|' read -r timestamp test_name status message <<< "$result"
            local status_color="$NC"
            if [ "$status" = "PASS" ]; then status_color="$GREEN"; fi
            if [ "$status" = "FAIL" ]; then status_color="$RED"; fi
            if [ "$status" = "SKIP" ]; then status_color="$YELLOW"; fi
            printf "%-40s | ${status_color}%-8s${NC} | %s\n" "$test_name" "$status" "$message"
        done
    else
        echo "----------------------------------------------------------------------------"
        echo -e "${YELLOW}WARNING: No test results recorded!${NC}"
        echo "----------------------------------------------------------------------------"
    fi
    
    echo ""
    echo "----------------------------------------------------------------------------"
    if [ $FAILED -eq 0 ] && [ $PASSED -gt 0 ]; then
        echo -e "${GREEN}✓ All tests passed${NC}"
    elif [ $PASSED -eq 0 ] && [ $FAILED -eq 0 ]; then
        echo -e "${YELLOW}⚠ No tests were executed${NC}"
    else
        echo -e "${RED}✗ Some tests failed${NC}"
        if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
            echo ""
            echo "Failed tests:"
            for failed_test in "${FAILED_TESTS[@]}"; do
                echo -e "  ${RED}✗ $failed_test${NC}"
            done
        fi
    fi
    echo "============================================================================"
    
    log_success "Test report generated: $report_file"
}

# ============================================================================
# Test Orchestration
# ============================================================================

run_all_tests() {
    run_tc001_pod_sa_rrsa || true
    run_tc002_provider_rrsa || true
    run_tc003_ram_role || true
    run_tc004_node_publish_secret || true
    run_tc005_aksk || true
    run_tc006_cross_account || true
    run_tc007_ecs_ram_role || true
    run_tc008_jmespath || true
    run_tc009_rotation || true
    run_tc010_secret_sync || true
    run_tc011_cleanup || true
}

# ============================================================================
# Main Function
# ============================================================================

main() {
    # Log initialization
    mkdir -p "$LOG_DIR"
    exec > >(tee -a "$LOG_DIR/test-run.log") 2>&1

    cd "$TESTS_DIR"

    # ========== Phase 1: Validate Environment ==========
    log_step "Phase 1: Validate Environment"
    validate_env

    # ========== Phase 2: Ensure RRSA Enabled ==========
    log_step "Phase 2: Ensure RRSA Enabled"
    ensure_rrsa_enabled || log_warning "RRSA enable check failed, continuing anyway (RRSA may already be enabled)"

    # ========== Phase 3: Deploy Provider DaemonSet ==========
    log_step "Phase 3: Deploy Provider DaemonSet"
    deploy_provider

    # ========== Phase 4: Prepare Resources ==========
    log_step "Phase 4: Prepare Resources"
    prepare_all_resources

    # ========== Phase 5: Execute Tests ==========
    log_step "Phase 5: Execute Tests"
    run_all_tests

    # ========== Phase 6: Cleanup & Report ==========
    log_step "Phase 6: Cleanup & Report"
    
    # Ensure report is always generated, even if cleanup fails
    cleanup_all || log_warning "Cleanup encountered errors, but generating report anyway"
    generate_report || log_error "Failed to generate test report"

    # Exit code
    if [ ${#FAILED_TESTS[@]} -gt 0 ] 2>/dev/null || [ $FAILED -gt 0 ]; then
        exit 1
    fi
    exit 0
}

# Register cleanup function
trap cleanup_all EXIT

main "$@"
