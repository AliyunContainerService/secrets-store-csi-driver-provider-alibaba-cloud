// suite_test.go - E2E test suite entry point
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/tests/e2e/framework"
)

var (
	// GlobalResourceManager is the shared resource manager for all tests
	GlobalResourceManager *ResourceManager
	// TestFramework is the shared Kubernetes framework
	TestFramework *framework.Framework
	// TestCfg is the shared test configuration
	TestCfg *TestConfig
	// TestCtx is the shared context for all tests
	TestCtx context.Context
	// TestCancel is the cancel function for TestCtx
	TestCancel context.CancelFunc

	// Test resource names (set in BeforeSuite)
	KMSPolicyName      string
	ProviderRoleName   string
	PodSARoleName      string
	RRSASecretName     string
	PodSASecretName    string
	AKSKSecretName     string
	CrossAccountSecret string
	RAMRoleSecretName  string
	NodePubSecretName  string
	ECSRoleSecretName  string
	RAMRoleName        string
	TestSuffix         string

	// Feature test KMS secret names
	JMESPathSecretName string
	RotationSecretName string
	SyncSecretName     string
	CleanupSecretName  string

	// authConfigured tracks whether authentication is currently configured on the DaemonSet.
	// TC-001~TC-007 set this to false via clearAllAuth() before configuring their own auth.
	// TC-008~TC-011 check this to decide whether to reuse existing auth or configure independently.
	authConfigured bool
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Secrets Store Provider Alibaba Cloud E2E Suite")
}

var _ = BeforeSuite(func() {
	var err error

	// Set up context
	TestCtx, TestCancel = context.WithCancel(context.TODO())

	By("loading test configuration")
	TestCfg = LoadTestConfig()
	Expect(TestCfg.Validate()).To(Succeed(), "Test configuration validation failed")

	GinkgoWriter.Printf("Test Configuration:\n")
	GinkgoWriter.Printf("  SourceAccountID: %s\n", TestCfg.SourceAccountID)
	GinkgoWriter.Printf("  ClusterID: %s\n", TestCfg.ClusterID)
	GinkgoWriter.Printf("  Namespace: %s\n", TestCfg.Namespace)
	GinkgoWriter.Printf("  OIDCProviderARN: %s\n", TestCfg.OIDCProviderARN)

	// Generate unique test suffix
	TestSuffix = fmt.Sprintf("%d", time.Now().Unix())

	// Set up resource names
	KMSPolicyName = TestResourcePrefix + "kms-policy-" + TestSuffix
	ProviderRoleName = TestResourcePrefix + "rrsa-role-" + TestSuffix
	PodSARoleName = TestResourcePrefix + "pod-sa-role-" + TestSuffix
	RRSASecretName = "rrsa-test-secret-" + TestSuffix
	PodSASecretName = "pod-sa-test-secret-" + TestSuffix
	AKSKSecretName = "aksk-test-secret-" + TestSuffix
	CrossAccountSecret = "cross-account-test-secret-" + TestSuffix
	RAMRoleSecretName = "ram-role-test-secret-" + TestSuffix
	NodePubSecretName = "node-pub-test-secret-" + TestSuffix
	ECSRoleSecretName = "ecs-role-test-secret-" + TestSuffix
	RAMRoleName = TestResourcePrefix + "ram-role-" + TestSuffix

	// Feature test KMS secret names
	JMESPathSecretName = "jmespath-test-" + TestSuffix
	RotationSecretName = "rotation-test-" + TestSuffix
	SyncSecretName = "sync-test-" + TestSuffix
	CleanupSecretName = "cleanup-test-" + TestSuffix

	By("initializing Kubernetes framework")
	TestFramework, err = framework.NewFramework(TestCfg.Kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes framework")

	By("initializing cloud resource manager")
	GlobalResourceManager, err = NewResourceManager(TestCfg)
	Expect(err).NotTo(HaveOccurred(), "Failed to create resource manager")

	By("ensuring RRSA is enabled on the cluster")
	err = ensureRRSAEnabled()
	Expect(err).NotTo(HaveOccurred(), "Failed to ensure RRSA is enabled")

	By("creating test namespace")
	actualNamespace, err := TestFramework.CreateNamespace(TestCtx, TestCfg.Namespace)
	Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
	TestCfg.Namespace = actualNamespace
	GinkgoWriter.Printf("  Test namespace: %s\n", TestCfg.Namespace)

	By("deploying Helm chart")
	err = deployHelmChart(TestCfg)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy Helm chart")

	By("waiting for Provider DaemonSet to be ready")
	err = TestFramework.WaitForDaemonSetReady(TestCtx,
		framework.ProviderDaemonSetNamespace,
		framework.ProviderDaemonSetName,
		3*time.Minute)
	Expect(err).NotTo(HaveOccurred(), "Provider DaemonSet not ready")

	By("preparing cloud resources")
	err = prepareCloudResources()
	Expect(err).NotTo(HaveOccurred(), "Failed to prepare cloud resources")

	By("creating KMS secrets for auth tests")
	authSecrets := map[string]string{
		RRSASecretName:    "rrsa-test-value",
		PodSASecretName:   "pod-sa-test-value",
		AKSKSecretName:    "aksk-test-value",
		RAMRoleSecretName: "ram-role-test-value",
		NodePubSecretName: "node-pub-test-value",
		ECSRoleSecretName: "ecs-role-test-value",
	}
	for name, data := range authSecrets {
		err = GlobalResourceManager.CreateKMSSecret(name, data, "v1")
		Expect(err).NotTo(HaveOccurred(), "Failed to create auth KMS secret: %s", name)
	}

	By("creating feature test KMS secrets")
	featureSecrets := map[string]string{
		JMESPathSecretName: `{"username": "testUser", "password": "testPassword"}`,
		RotationSecretName: "before-rotation",
		SyncSecretName:     "sync-test-value",
		CleanupSecretName:  "cleanup-test-value",
	}
	for name, data := range featureSecrets {
		err = GlobalResourceManager.CreateKMSSecret(name, data, "v1")
		Expect(err).NotTo(HaveOccurred(), "Failed to create feature KMS secret: %s", name)
	}

	// Prepare cross-account resources if target account is configured
	if TestCfg.TargetAccountID != "" && TestCfg.TargetAccessKeyID != "" && TestCfg.TargetAccessKeySecret != "" {
		By("setting up target account resources")
		err = GlobalResourceManager.SetupTargetAccountResources(CrossAccountSecret)
		Expect(err).NotTo(HaveOccurred(), "Failed to setup target account resources")
		GinkgoWriter.Printf("Target Role ARN: %s\n", TestCfg.TargetRoleARN)
	} else if TestCfg.TargetAccountID != "" {
		GinkgoWriter.Println("Warning: TARGET_ACCOUNT_ID set but TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET not set, skipping cross-account resource creation")
	}

	GinkgoWriter.Println("E2E test suite setup completed successfully")
})

var _ = AfterSuite(func() {
	By("cleaning up cloud resources")
	if GlobalResourceManager != nil {
		GlobalResourceManager.Cleanup()
	}

	By("cleaning up target account resources")
	if GlobalResourceManager != nil {
		GlobalResourceManager.CleanupTargetAccountResources()
	}

	By("uninstalling Helm chart")
	if TestCfg != nil {
		_ = uninstallHelmChart()
	}

	By("cleaning up test namespace")
	if TestFramework != nil && TestCfg != nil {
		_ = TestFramework.DeleteNamespace(TestCtx, TestCfg.Namespace)
	}

	By("tearing down the test environment")
	if TestCancel != nil {
		TestCancel()
	}
})

// execAliyunWithRetry executes an aliyun CLI command with retry and linear backoff.
func execAliyunWithRetry(maxRetries int, args ...string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cmd := exec.Command("aliyun", args...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			return output, nil
		}
		lastErr = fmt.Errorf("aliyun %s failed (attempt %d/%d): %w, output: %s",
			strings.Join(args, " "), attempt, maxRetries, err, string(output))
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * 5 * time.Second)
		}
	}
	return nil, lastErr
}

// isRRSAEnabled queries the cluster and returns whether RRSA is enabled.
func isRRSAEnabled(clusterID string) (bool, error) {
	output, err := execAliyunWithRetry(3, "cs", "GET", "/clusters/"+clusterID, "--region", TestCfg.RegionID, "--header", "Content-Type=application/json")
	if err != nil {
		return false, fmt.Errorf("failed to check cluster RRSA status: %v", err)
	}

	var clusterInfo struct {
		RRSAConfig struct {
			Enabled bool `json:"enabled"`
		} `json:"rrsa_config"`
	}
	if err := json.Unmarshal(output, &clusterInfo); err != nil {
		return false, fmt.Errorf("failed to parse cluster info: %v, output: %s", err, string(output))
	}
	return clusterInfo.RRSAConfig.Enabled, nil
}

// ensureRRSAEnabled checks if RRSA is enabled on the cluster and enables it if not.
// Calls print warning if RRSA cannot be enabled within the timeout.
func ensureRRSAEnabled() error {
	clusterID := TestCfg.ClusterID
	if clusterID == "" {
		return fmt.Errorf("ClusterID not set, cannot verify RRSA status\n")
	}

	// Check cluster RRSA status
	enabled, err := isRRSAEnabled(clusterID)
	if err != nil {
		return fmt.Errorf("Failed to check RRSA status: %v\n", err)
	}
	if enabled {
		fmt.Println("RRSA is already enabled on the cluster")
		return nil
	}

	// Enable RRSA (idempotent: repeated calls have no side effects)
	fmt.Println("Enabling RRSA on cluster...")
	if _, err := execAliyunWithRetry(3, "cs", "PUT", "/api/v2/clusters/"+clusterID, "--region", TestCfg.RegionID, "--header", "Content-Type=application/json", "--body", "{\"enable_rrsa\":true}"); err != nil {
		return fmt.Errorf("Failed to enable RRSA: %v\n", err)
	}

	// Wait for RRSA to take effect by polling
	fmt.Println("Waiting for RRSA to take effect...")
	for i := 0; i < 12; i++ { // max 12 * 10s = 120s
		time.Sleep(10 * time.Second)

		enabled, err := isRRSAEnabled(clusterID)
		if err != nil {
			fmt.Printf("RRSA check error, retrying... (%d/12): %v\n", i+1, err)
			continue
		}
		if enabled {
			fmt.Println("RRSA is now enabled on the cluster")
			return nil
		}
		fmt.Printf("RRSA not yet enabled, retrying... (%d/12)\n", i+1)
	}
	return fmt.Errorf("RRSA did not become enabled within 120s\n")
}

// deployHelmChart deploys the CSI Provider Helm chart
func deployHelmChart(cfg *TestConfig) error {
	if cfg.SkipProviderDeploy {
		GinkgoWriter.Println("SKIP_PROVIDER_DEPLOY=true, skipping Helm chart deployment")
		return nil
	}

	// Check if the provider release is already deployed
	cmd := exec.Command("helm", "list", "-n", "kube-system", "-q")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "csi-secrets-store-provider-alibabacloud") {
		GinkgoWriter.Println("Provider Helm chart already deployed, skipping installation")
		return nil
	}

	// Auto-detect Helm Chart path based on test file location
	// tests/e2e/suite_test.go -> project root -> charts/csi-secrets-store-provider-alibabacloud
	chartPath := detectChartPath()
	GinkgoWriter.Printf("Using Helm chart path: %s\n", chartPath)

	// Build helm install command
	args := []string{
		"install", "csi-secrets-store-provider-alibabacloud",
		chartPath,
		"-n", "kube-system",
		"--create-namespace",
		"--wait",
		"--timeout", "5m",
	}

	// Add RRSA values if configured
	if cfg.OIDCProviderARN != "" {
		args = append(args,
			"--set", "rrsa.enable=true",
			"--set", fmt.Sprintf("rrsa.accountId=%s", cfg.SourceAccountID),
			"--set", fmt.Sprintf("rrsa.clusterId=%s", cfg.ClusterID),
		)
	}

	// Enable secret rotation and sync for feature tests (TC-009/TC-010/TC-011).
	// rotationPollInterval=10s overrides the default 2m interval so TC-009 rotation
	// verification completes within a reasonable test duration.
	args = append(args,
		"--set", "secrets-store-csi-driver.enableSecretRotation=true",
		"--set", "secrets-store-csi-driver.syncSecret.enabled=true",
		"--set", "secrets-store-csi-driver.rotationPollInterval=10s",
	)

	cmd = exec.Command("helm", args...)
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm install failed: %w", err)
	}

	return nil
}

// detectChartPath auto-detects the Helm chart path based on the current working directory
func detectChartPath() string {
	// Try common relative paths from different execution contexts
	candidates := []string{
		"../../charts/csi-secrets-store-provider-alibabacloud",    // from tests/e2e/
		"../../../charts/csi-secrets-store-provider-alibabacloud", // from tests/e2e/subdir
		"charts/csi-secrets-store-provider-alibabacloud",          // from project root
	}

	for _, path := range candidates {
		// Check if directory exists
		cmd := exec.Command("test", "-d", path)
		if cmd.Run() == nil {
			return path
		}
	}

	// Fallback to default path (will fail with clear error if not found)
	return "../../charts/csi-secrets-store-provider-alibabacloud"
}

// getOIDCIssuerURL constructs the OIDC issuer URL directly from known cluster information.
// Priority: 1) OIDC_ISSUER_URL env var, 2) construct from region and cluster-id.
// The URL format is: https://oidc-ack-<region>.oss-<region>.aliyuncs.com/<cluster-id>
// This avoids the unreliable aliyun cs GET /clusters/<id> API call.
func getOIDCIssuerURL() string {
	// Priority 1: explicit env var override
	if envURL := os.Getenv("OIDC_ISSUER_URL"); envURL != "" {
		log.Printf("Using OIDC issuer URL from OIDC_ISSUER_URL env var: %s", envURL)
		return envURL
	}

	// Priority 2: construct directly from region and cluster-id
	region := TestCfg.RegionID
	clusterID := TestCfg.ClusterID
	if region == "" || clusterID == "" {
		Fail(fmt.Sprintf("Cannot construct OIDC issuer URL: region=%q, clusterID=%q. "+
			"Set OIDC_ISSUER_URL env var or ensure REGION and CLUSTER_ID are set.", region, clusterID))
		return "" // unreachable, Fail panics
	}

	issuerURL := fmt.Sprintf("https://oidc-ack-%s.oss-%s.aliyuncs.com/%s", region, region, clusterID)
	log.Printf("Constructed OIDC issuer URL from region and clusterID: %s", issuerURL)
	return issuerURL
}

// uninstallHelmChart uninstalls the CSI Provider Helm chart
func uninstallHelmChart() error {
	cmd := exec.Command("helm", "uninstall", "csi-secrets-store-provider-alibabacloud",
		"-n", "kube-system")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm uninstall failed: %w", err)
	}

	// Clean up leftover pre-install hook Jobs (not deleted on hook failure)
	cleanupCmd := exec.Command("kubectl", "delete", "job", "-n", "kube-system",
		"secrets-store-csi-driver-upgrade-crds",
		"--ignore-not-found=true")
	cleanupCmd.Stdout = GinkgoWriter
	cleanupCmd.Stderr = GinkgoWriter
	_ = cleanupCmd.Run()

	return nil
}

// prepareCloudResources creates the shared KMS policy, RRSA roles, and RAM role
func prepareCloudResources() error {
	// Create unified RAM Policy for KMS access
	_, err := GlobalResourceManager.CreateRAMPolicy(KMSPolicyName, UnifiedKMSPolicyDoc)
	if err != nil {
		return fmt.Errorf("failed to create unified KMS policy: %w", err)
	}

	namespace := TestCfg.Namespace
	if namespace == "" {
		namespace = "default"
	}
	podSAName := "tc001-test-sa"
	uid := "placeholder-uid"
	sourceAccountID := TestCfg.SourceAccountID
	targetAccountID := TestCfg.TargetAccountID
	// Note: targetAccountID is the account where the OIDC Provider resides
	// The OIDC Provider ARN should be in the format: acs:ram::<target-account-id>:oidc-provider/<cluster-id>
	if TestCfg.OIDCProviderARN == "" {
		return fmt.Errorf("OIDCProviderARN is required for RRSA trust policies")
	}
	if sourceAccountID == "" {
		return fmt.Errorf("SourceAccountID is required for RRSA configuration")
	}
	if targetAccountID == "" {
		return fmt.Errorf("TargetAccountID is required for RRSA configuration")
	}
	GinkgoWriter.Printf("RRSA trust policies: namespace=%s, providerSA=%s, podSA=%s, uid=%s, sourceAccountID=%s, targetAccountID=%s, oidcProviderARN=%s\n",
		namespace, framework.ProviderDaemonSetName, podSAName, uid, sourceAccountID, targetAccountID, TestCfg.OIDCProviderARN)

	oidcIssuerURL := getOIDCIssuerURL()
	GinkgoWriter.Printf("OIDC issuer URL: %s\n", oidcIssuerURL)

	// === Cross-account RRSA authentication flow ===
	// 1. Pod SA gets OIDC token from OIDC Provider (in target account)
	// 2. Provider DaemonSet uses OIDCProviderARN + providerRoleARN to assume Provider Role
	// 3. Provider Role (in source account) trusts target account's OIDC Provider
	// 4. Provider can now access KMS secrets using the assumed role credentials
	//
	// Key points:
	// - OIDCProviderARN: target account's OIDC Provider ARN
	//   Format: acs:ram::<target-account-id>:oidc-provider/<cluster-id>
	// - providerRoleARN: source account's role ARN
	//   Format: acs:ram::<source-account-id>:role/<role-name>
	// - oidc:iss: OIDC issuer URL, used to validate the OIDC token issuer
	// - oidc:sub: OIDC subject, used to validate the Pod SA
	// - oidc:aud: OIDC audience, used to validate the STS token audience
	//
	// Note: targetAccountID must match the account ID embedded in OIDCProviderARN
	// === End of cross-account RRSA authentication flow ===

	// Create Provider RRSA Role
	// Authentication flow: Provider DaemonSet SA -> OIDC Provider (target account) -> STS AssumeRole -> Provider Role (source account)
	// The Provider DaemonSet uses this role to access KMS secrets in the target account
	// The OIDC Provider ARN contains the target account ID where the OIDC Provider resides
	// NOTE: oidc:aud must be "sts.aliyuncs.com" (the actual audience in OIDC tokens issued by ACK)
	// NOTE: StringLike is used because oidc:sub contains wildcard (*) for SA name matching;
	//       exact values (oidc:aud, oidc:iss) also match correctly under StringLike.
	// NOTE: The Provider DaemonSet runs in kube-system namespace with SA "csi-secrets-store-provider-alibabacloud",
	//       so oidc:sub must match "system:serviceaccount:kube-system:<sa-name>", NOT the test namespace.
	providerTrustPolicy := fmt.Sprintf(`{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringLike": {
          "oidc:aud": ["sts.aliyuncs.com"],
          "oidc:iss": ["%s"],
          "oidc:sub": ["system:serviceaccount:%s:*"]
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": ["%s"]
      }
    }
  ],
  "Version": "1"
}`, oidcIssuerURL, framework.ProviderDaemonSetNamespace, TestCfg.OIDCProviderARN)

	_, err = GlobalResourceManager.CreateRAMRole(ProviderRoleName, providerTrustPolicy)
	if err != nil {
		return fmt.Errorf("failed to create Provider role: %w", err)
	}
	// Provider role: used by the CSI Provider DaemonSet to access KMS secrets
	if err := GlobalResourceManager.AttachPolicyToRole(KMSPolicyName, ProviderRoleName); err != nil {
		return fmt.Errorf("failed to attach policy to Provider role: %w", err)
	}

	// Create Pod SA Role (for TC-001)
	// Authentication flow: Pod SA -> OIDC Provider (target account) -> STS AssumeRole -> Pod SA Role (source account)
	// TC-001 test case uses this role to verify Pod ServiceAccount authentication
	// The OIDC Provider ARN contains the target account ID where the OIDC Provider resides
	// NOTE: oidc:aud must be "sts.aliyuncs.com" (the actual audience in OIDC tokens issued by ACK)
	// NOTE: StringLike is used because oidc:sub contains wildcard (*) for SA name matching;
	//       exact values (oidc:aud, oidc:iss) also match correctly under StringLike.
	podSATrustPolicy := fmt.Sprintf(`{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringLike": {
          "oidc:aud": ["sts.aliyuncs.com"],
          "oidc:iss": ["%s"],
          "oidc:sub": ["system:serviceaccount:%s:*"]
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": ["%s"]
      }
    }
  ],
  "Version": "1"
}`, oidcIssuerURL, namespace, TestCfg.OIDCProviderARN)

	_, err = GlobalResourceManager.CreateRAMRole(PodSARoleName, podSATrustPolicy)
	if err != nil {
		return fmt.Errorf("failed to create Pod SA role: %w", err)
	}
	// Pod SA role: used by TC-001 test case (Pod ServiceAccount authentication)
	if err := GlobalResourceManager.AttachPolicyToRole(KMSPolicyName, PodSARoleName); err != nil {
		return fmt.Errorf("failed to attach policy to Pod SA role: %w", err)
	}

	// Create RAM Role for TC-003 (AK/SK + AssumeRole)
	// TC-003 authentication flow: AK/SK -> STS AssumeRole -> RAM Role -> access KMS secrets
	// This role is assumed using source account's AK/SK credentials
	// The trust policy uses sourceAccountID to allow the source account to assume this role
	ramRoleTrustPolicy := fmt.Sprintf(`{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Effect": "Allow",
      "Principal": {
        "RAM": ["acs:ram::%s:root"]
      }
    }
  ],
  "Version": "1"
}`, TestCfg.SourceAccountID)

	_, err = GlobalResourceManager.CreateRAMRole(RAMRoleName, ramRoleTrustPolicy)
	if err != nil {
		return fmt.Errorf("failed to create RAM role for TC-003: %w", err)
	}
	if err := GlobalResourceManager.AttachPolicyToRole(KMSPolicyName, RAMRoleName); err != nil {
		return fmt.Errorf("failed to attach policy to RAM role for TC-003: %w", err)
	}

	// Auto-populate RamRoleArn if not set
	if TestCfg.RAMRoleArn == "" {
		TestCfg.RAMRoleArn = fmt.Sprintf("acs:ram::%s:role/%s", TestCfg.SourceAccountID, RAMRoleName)
	}
	// RAM Role: used by TC-003 for AK/SK + AssumeRole authentication
	// The RamRoleArn is auto-populated with the source account ID and role name
	// Format: acs:ram::<source-account-id>:role/<role-name>

	return nil
}

// clearAllAuth clears all authentication configuration from the DaemonSet.
// This is called before each auth test (TC-001~TC-007) to ensure a clean state.
// It always patches the DaemonSet to remove all auth env vars (regardless of the
// authConfigured flag), ensuring no residual credentials from a previous test leak
// into the next one.
//
// IMPORTANT: The operation order is critical:
//  1. First, patchDaemonSetAuth("none") removes all auth env vars (including SecretKeyRef
//     references) from the DaemonSet and waits for rollout to complete. After this step,
//     new Pods no longer reference the alibaba-credentials Secret.
//  2. Then, delete the alibaba-credentials Secret. This is safe because no running Pod
//     depends on it anymore.
//
// If the order were reversed (delete Secret first, then patch), a Pod restart between
// the two operations would fail with CreateContainerConfigError because the SecretKeyRef
// env vars still reference the now-deleted Secret.
//
// Note: The Provider role is created in the source account, with ARN format: acs:ram::<source-account-id>:role/<role-name>
// The alibaba-credentials Secret contains credentials from the source account (sourceAccountID)
func clearAllAuth() {
	By("clearing all authentication from DaemonSet")

	// Step 1: Patch DaemonSet to remove all auth env vars (including SecretKeyRef references).
	// This triggers a rolling restart; patchDaemonSetAuth waits for rollout to complete.
	// After this, all running Pods have no auth env vars and no SecretKeyRef dependencies.
	patchDaemonSetAuth(TestCtx, "none")

	// Step 2: Now safe to delete the alibaba-credentials Secret.
	// No running Pod references it, so deletion cannot cause CreateContainerConfigError.
	// This Secret contains OIDCProviderARN (target account) and providerRoleARN (source account).
	_ = TestFramework.DeleteSecret(TestCtx, "kube-system", "alibaba-credentials")

	authConfigured = false
}

// verifyExistingAuth performs a lightweight check to verify that previously configured
// auth is still functional. It checks that the Provider DaemonSet is ready and that
// the alibaba-credentials Secret still exists with valid-looking data.
// Returns nil if auth is still valid, or an error describing why reconfiguration is needed.
func verifyExistingAuth() error {
	// Check 1: Verify Provider DaemonSet pods are ready
	err := TestFramework.WaitForDaemonSetReady(TestCtx,
		framework.ProviderDaemonSetNamespace,
		framework.ProviderDaemonSetName,
		30*time.Second)
	if err != nil {
		return fmt.Errorf("DaemonSet not ready: %w", err)
	}

	// Check 2: Verify alibaba-credentials Secret still exists
	secret, err := TestFramework.Clientset.CoreV1().Secrets("kube-system").Get(
		TestCtx, "alibaba-credentials", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("alibaba-credentials Secret missing: %w", err)
	}
	if len(secret.Data) == 0 {
		return fmt.Errorf("alibaba-credentials Secret has no data")
	}

	// Check 3: If Secret contains AK/SK (id/secret keys), verify credentials via STS
	if ak, ok := secret.Data["id"]; ok {
		sk := secret.Data["secret"]
		if _, verifyErr := execAliyunWithRetry(3, "sts", "GetCallerIdentity",
			"--access-key-id", string(ak),
			"--access-key-secret", string(sk)); verifyErr != nil {
			return fmt.Errorf("AK/SK credentials no longer valid: %v", verifyErr)
		}
	}

	return nil
}

// ensureAuthConfigured checks if auth is already configured; if not, configures minimal AK/SK auth.
// Used by feature tests (TC-008~TC-011) to reuse existing auth or set up minimal auth.
// When auth is already configured (authConfigured=true), it verifies the existing auth is still
// valid before reusing it. If verification fails, it falls through to full reconfiguration.
// The AK/SK credentials are from the source account (sourceAccountID)
func ensureAuthConfigured() {
	if authConfigured {
		// Verify existing auth is still functional before reusing
		if err := verifyExistingAuth(); err != nil {
			GinkgoWriter.Printf("Existing auth verification failed: %v, reconfiguring...\n", err)
			authConfigured = false
			// Fall through to full configuration
		} else {
			GinkgoWriter.Println("Existing auth verified successfully, reusing for feature tests")
			return
		}
	}

	By("configuring minimal AK/SK auth for feature tests")

	if TestCfg.AccessKeyID == "" || TestCfg.AccessKeySecret == "" {
		Fail("AK/SK credentials not configured, cannot set up auth for feature tests")
	}
	// The AK/SK credentials (AccessKeyID/AccessKeySecret) are from the source account (sourceAccountID)
	// These credentials are used to create a RAM user with KMS access

	// Create RAM user with unified KMS policy (tracked by GlobalResourceManager for cleanup)
	// The RAM user is created in the source account (using sourceAccountID's credentials)
	username := fmt.Sprintf("tc-feature-%d", time.Now().Unix())
	ak, sk, err := GlobalResourceManager.CreateRAMUserWithKMSPolicy(username, KMSPolicyName)
	Expect(err).NotTo(HaveOccurred(), "failed to create RAM user for feature tests")

	// Create alibaba-credentials Secret
	// This Secret contains AK/SK credentials from the source account (sourceAccountID)
	// These credentials are used by the Provider DaemonSet to access KMS secrets
	credSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alibaba-credentials",
			Namespace: "kube-system",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"id":     []byte(ak),
			"secret": []byte(sk),
		},
	}
	_ = TestFramework.DeleteSecret(TestCtx, "kube-system", "alibaba-credentials")
	err = TestFramework.CreateSecret(TestCtx, "kube-system", credSecret)
	Expect(err).NotTo(HaveOccurred(), "failed to create alibaba-credentials Secret")

	// Use patchDaemonSetAuth for atomic env replacement (preserves Helm vars)
	// This configures the DaemonSet to use AK/SK authentication with credentials from sourceAccountID
	// The AK/SK credentials were obtained from the source account and stored in alibaba-credentials Secret
	patchDaemonSetAuth(TestCtx, "aksk")

	// Wait for IAM propagation (IAM eventual consistency).
	// Newly created RAM User AK/SK may not be immediately valid across all regions;
	// poll STS GetCallerIdentity to confirm the credentials are active before proceeding.
	By("waiting for IAM propagation of new AK/SK credentials")
	var iamReady bool
	for i := 0; i < 12; i++ { // max 12 * 5s = 60s
		verifyCmd := exec.Command("aliyun", "sts", "GetCallerIdentity",
			"--access-key-id", ak,
			"--access-key-secret", sk)
		if out, verifyErr := verifyCmd.CombinedOutput(); verifyErr == nil {
			log.Printf("IAM propagation verified: AK/SK is active (%s)", string(out))
			iamReady = true
			break
		}
		log.Printf("AK/SK not yet active, retrying... (%d/12)", i+1)
		time.Sleep(5 * time.Second)
	}
	if !iamReady {
		log.Printf("Warning: AK/SK did not become active within 60s, proceeding anyway")
	}

	authConfigured = true
}
