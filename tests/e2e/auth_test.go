// auth_test.go - Authentication method E2E tests (TC-001 ~ TC-007)
package e2e

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/tests/e2e/framework"
)

// authEnvVars is the set of environment variable names managed by auth configuration.
// These are filtered out before applying a new auth mode to avoid stale variables.
// Includes OIDC auto-construction variables (ALICLOUD_ACCOUNT_ID, ALICLOUD_CLUSTER_ID)
// that auth.go uses to derive OIDC Provider ARN.
var authEnvVars = map[string]bool{
	"ACCESS_KEY_ID":                    true,
	"SECRET_ACCESS_KEY":                true,
	"ALICLOUD_ACCOUNT_ID":              true,
	"ALICLOUD_CLUSTER_ID":              true,
	"ALICLOUD_ROLE_ARN":                true,
	"ALICLOUD_OIDC_PROVIDER_ARN":       true,
	"ALICLOUD_USE_CSI_DRIVER":          true,
	"ALICLOUD_ROLE_SESSION_NAME":       true,
	"ALICLOUD_ROLE_SESSION_EXPIRATION": true,
	"ALICLOUD_ACCESS_KEY_ID":           true,
	"ALICLOUD_ACCESS_KEY_SECRET":       true,
	"ALICLOUD_CROSS_ACCOUNT_ROLE_ARN":  true,
}

// patchDaemonSetAuth configures the Provider DaemonSet authentication mode.
// Supported modes: "rrsa", "rrsa_oidc_only", "aksk", "aksk_role", "none"
//
// IMPORTANT: This function uses a full GET → modify → UPDATE cycle rather than
// StrategicMergePatch. Kubernetes StrategicMergePatch merges container env lists
// by name and CANNOT remove env vars that are not present in the patch payload.
// A full update gives us complete control over the env list, ensuring that old
// SecretKeyRef env vars are fully removed when switching auth modes or clearing auth.
func patchDaemonSetAuth(ctx context.Context, authMode string) {
	By(fmt.Sprintf("configuring DaemonSet auth mode: %s", authMode))

	// Step 1: GET the current DaemonSet to capture pre-update generation and existing env vars
	ds, err := TestFramework.Clientset.AppsV1().DaemonSets(framework.ProviderDaemonSetNamespace).Get(
		ctx, framework.ProviderDaemonSetName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get DaemonSet")

	// Capture pre-update spec.Generation (NOT Status.ObservedGeneration).
	// After our update, the controller will bump Generation; we wait for ObservedGeneration to catch up.
	preGeneration := ds.Generation

	// Step 2: Find the provider container by name
	containerIdx := -1
	for i, c := range ds.Spec.Template.Spec.Containers {
		if c.Name == "provider-alibabacloud-installer" {
			containerIdx = i
			break
		}
	}
	// Fallback to first container if provider container name not found
	if containerIdx == -1 && len(ds.Spec.Template.Spec.Containers) > 0 {
		containerIdx = 0
		log.Printf("WARNING: provider container 'provider-alibabacloud-installer' not found, falling back to container[0] %q",
			ds.Spec.Template.Spec.Containers[0].Name)
	}
	Expect(containerIdx).NotTo(Equal(-1), "no containers found in DaemonSet")

	containerName := ds.Spec.Template.Spec.Containers[containerIdx].Name

	// Step 3: Filter out ALL existing auth env vars from the container's env list.
	// This is the critical fix: StrategicMergePatch cannot delete env vars, but a full
	// update replaces the entire env list, so we rebuild it without auth vars.
	var newEnv []corev1.EnvVar
	for _, env := range ds.Spec.Template.Spec.Containers[containerIdx].Env {
		if !authEnvVars[env.Name] {
			newEnv = append(newEnv, env)
		}
	}

	// Step 4: Append new auth env vars based on the requested mode
	switch authMode {
	case "rrsa":
		newEnv = append(newEnv,
			corev1.EnvVar{Name: "ALICLOUD_OIDC_PROVIDER_ARN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "oidcproviderarn"}}},
			corev1.EnvVar{Name: "ALICLOUD_ROLE_ARN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "rolearn"}}},
			corev1.EnvVar{Name: "ALICLOUD_USE_CSI_DRIVER", Value: "true"},
		)
	case "rrsa_oidc_only":
		newEnv = append(newEnv,
			corev1.EnvVar{Name: "ALICLOUD_OIDC_PROVIDER_ARN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "oidcproviderarn"}}},
			corev1.EnvVar{Name: "ALICLOUD_USE_CSI_DRIVER", Value: "true"},
		)
	case "aksk":
		newEnv = append(newEnv,
			corev1.EnvVar{Name: "ACCESS_KEY_ID", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "id"}}},
			corev1.EnvVar{Name: "SECRET_ACCESS_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "secret"}}},
		)
	case "aksk_role":
		newEnv = append(newEnv,
			corev1.EnvVar{Name: "ACCESS_KEY_ID", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "id"}}},
			corev1.EnvVar{Name: "SECRET_ACCESS_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "secret"}}},
			corev1.EnvVar{Name: "ALICLOUD_ROLE_ARN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "alibaba-credentials"}, Key: "rolearn"}}},
		)
	case "none":
		// No auth env vars added — newEnv contains only non-auth (Helm-injected) vars
	default:
		Fail(fmt.Sprintf("unknown auth mode: %s", authMode))
	}

	// Step 5: Set the new env list directly on the DaemonSet object.
	// This is a full replacement of the env slice, not a merge patch.
	ds.Spec.Template.Spec.Containers[containerIdx].Env = newEnv

	// Step 6: Add restart annotation to force a rolling restart
	if ds.Spec.Template.Annotations == nil {
		ds.Spec.Template.Annotations = make(map[string]string)
	}
	ds.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Step 7: UPDATE (PUT) the entire DaemonSet. This replaces the spec atomically,
	// ensuring all env changes (including removals) take effect in a single generation bump.
	_, err = TestFramework.Clientset.AppsV1().DaemonSets(framework.ProviderDaemonSetNamespace).Update(
		ctx, ds, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to update DaemonSet")

	// Step 8: Wait for DaemonSet controller to process the update and complete rollout
	By("waiting for DaemonSet rollout to complete")
	err = TestFramework.WaitForDaemonSetRollout(ctx,
		framework.ProviderDaemonSetNamespace,
		framework.ProviderDaemonSetName,
		preGeneration,
		2*time.Minute)
	Expect(err).NotTo(HaveOccurred(), "DaemonSet not ready after auth mode change")

	// Step 9: Verify Pod env vars match the requested auth mode.
	// After rollout, new Pods should have exactly the env vars we set.
	By(fmt.Sprintf("verifying Pod env vars for auth mode: %s", authMode))
	pods, listErr := TestFramework.Clientset.CoreV1().Pods(framework.ProviderDaemonSetNamespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: "app=" + framework.ProviderDaemonSetName,
		})
	if listErr == nil && len(pods.Items) > 0 {
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.Containers {
				if container.Name == containerName {
					for _, env := range container.Env {
						if authEnvVars[env.Name] && authMode == "none" {
							log.Printf("WARNING: auth env var %s still present in pod %s after cleanup to mode none",
								env.Name, pod.Name)
						}
					}
				}
			}
		}
	} else {
		log.Printf("WARNING: could not list pods for env verification: %v", listErr)
	}
}

// createSPC creates a SecretProviderClass with the given parameters and objects YAML
func createSPC(ctx context.Context, namespace, name, provider, objectsYAML string, params map[string]string) {
	spc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "secrets-store.csi.x-k8s.io/v1",
			"kind":       "SecretProviderClass",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"test-case": "auth-test",
				},
			},
			"spec": map[string]interface{}{
				"provider":   provider,
				"parameters": buildParamsMap(params, objectsYAML),
			},
		},
	}

	err := TestFramework.CreateSecretProviderClass(ctx, namespace, spc)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create SPC %s", name))
}

// buildParamsMap merges user params with the objects field
func buildParamsMap(params map[string]string, objectsYAML string) map[string]interface{} {
	m := make(map[string]interface{})
	for k, v := range params {
		m[k] = v
	}
	if objectsYAML != "" {
		m["objects"] = objectsYAML
	}
	return m
}

// createTestPod creates a Pod with CSI volume mount for the given SPC
func createTestPod(ctx context.Context, namespace, podName, spcName, saName, mountPath string) {
	podObj := buildCSIPod(podName, spcName, saName, mountPath)
	err := TestFramework.CreatePod(ctx, namespace, podObj)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create pod %s", podName))
}

// buildCSIPod constructs an unstructured Pod with CSI volume
func buildCSIPod(podName, spcName, saName, mountPath string) *unstructured.Unstructured {
	return buildCSIPodWithNodePublishSecret(podName, spcName, saName, mountPath, "")
}

// buildCSIPodWithNodePublishSecret constructs an unstructured Pod with CSI volume,
// optionally configuring nodePublishSecretRef on the CSI volume.
func buildCSIPodWithNodePublishSecret(podName, spcName, saName, mountPath, nodePublishSecretName string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": podName,
				"labels": map[string]interface{}{
					"app":       podName,
					"test-case": "auth-test",
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":    podName,
						"image":   framework.TestPodImage,
						"command": []interface{}{"sleep", "3600"},
						"volumeMounts": []interface{}{
							map[string]interface{}{
								"name":      "secrets-store",
								"mountPath": mountPath,
								"readOnly":  true,
							},
						},
					},
				},
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "secrets-store",
						"csi": map[string]interface{}{
							"driver":   "secrets-store.csi.k8s.io",
							"readOnly": true,
							"volumeAttributes": map[string]interface{}{
								"secretProviderClass": spcName,
							},
						},
					},
				},
			},
		},
	}

	if saName != "" {
		_ = unstructured.SetNestedField(pod.Object, saName, "spec", "serviceAccountName")
	}

	if nodePublishSecretName != "" {
		volumes, found, _ := unstructured.NestedSlice(pod.Object, "spec", "volumes")
		if found && len(volumes) > 0 {
			csi, found, _ := unstructured.NestedMap(volumes[0].(map[string]interface{}), "csi")
			if found {
				csi["nodePublishSecretRef"] = map[string]interface{}{
					"name": nodePublishSecretName,
				}
				_ = unstructured.SetNestedMap(volumes[0].(map[string]interface{}), csi, "csi")
				_ = unstructured.SetNestedSlice(pod.Object, volumes, "spec", "volumes")
			}
		}
	}

	return pod
}

// cleanupTestResources deletes Pods and SPCs with the test-case label
func cleanupTestResources(ctx context.Context, namespace string) {
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "pod",
		"-l", "test-case=auth-test",
		"-n", namespace, "--ignore-not-found=true", "--wait=false")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	_ = cmd.Run()

	cmd = exec.CommandContext(ctx, "kubectl", "delete", "secretproviderclass",
		"-l", "test-case=auth-test",
		"-n", namespace, "--ignore-not-found=true")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	_ = cmd.Run()

	time.Sleep(3 * time.Second)
}

// ============================================================================
// Authentication Tests (TC-001 ~ TC-007)
// Each test calls clearAllAuth() before configuring its own auth mode.
// ============================================================================

var _ = Describe("Authentication Methods", func() {

	// TC-001: Pod SA RRSA authentication (auth chain priority: 1)
	Context("TC-001: Pod SA RRSA", func() {
		var (
			spcName = "tc001-podsa-spc"
			podName = "tc001-podsa-pod"
			saName  = "tc001-test-sa"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via Pod ServiceAccount RRSA and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			// Configure DaemonSet with OIDC provider ARN only (Pod SA handles role assumption)
			By("configuring DaemonSet with OIDC provider ARN only")
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alibaba-credentials",
					Namespace: "kube-system",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"oidcproviderarn": []byte(TestCfg.OIDCProviderARN),
				},
			}
			_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			err := TestFramework.CreateSecret(ctx, "kube-system", credSecret)
			Expect(err).NotTo(HaveOccurred(), "failed to create OIDC credentials secret")
			patchDaemonSetAuth(ctx, "rrsa_oidc_only")

			// Verify OIDC issuer URL format
			By("verifying OIDC issuer URL format")
			isValidFormat := strings.Contains(TestCfg.OIDCProviderARN, fmt.Sprintf("oidc-ack-cn-%s", TestCfg.RegionID)) ||
				strings.Contains(TestCfg.OIDCProviderARN, "ack-rrsa-")
			Expect(isValidFormat).To(BeTrue(),
				"OIDC provider ARN should use oidc-ack-cn-<region> or ack-rrsa-<clusterID> format")

			// Create ServiceAccount with RRSA annotation
			By("creating ServiceAccount with role ARN annotation")
			podSARoleARN := fmt.Sprintf("acs:ram::%s:role/%s", TestCfg.SourceAccountID, PodSARoleName)
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      saName,
					Namespace: ns,
					Annotations: map[string]string{
						"ack.alibabacloud.com/role-arn": podSARoleARN,
					},
				},
			}
			err = TestFramework.CreateServiceAccount(ctx, ns, sa)
			Expect(err).NotTo(HaveOccurred(), "failed to create ServiceAccount")
			DeferCleanup(func() {
				_ = TestFramework.DeleteServiceAccount(ctx, ns, saName)
			})

			// Verify Pod SA Role has KMS policy attached
			By("verifying Pod SA Role has KMS policy attached")
			err = GlobalResourceManager.VerifyRolePolicyAttachment(PodSARoleName, KMSPolicyName)
			Expect(err).NotTo(HaveOccurred(), "Pod SA Role missing KMS policy attachment")

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, PodSASecretName)
			params := map[string]string{
				"usePodServiceAccountToken": "true",
			}
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, params)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with specific ServiceAccount")
			createTestPod(ctx, ns, podName, spcName, saName, "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", PodSASecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "pod-sa-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-002: Provider RRSA authentication (auth chain priority: 2)
	Context("TC-002: Provider RRSA", func() {
		var (
			spcName = "tc002-rrsa-spc"
			podName = "tc002-rrsa-pod"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via Provider RRSA and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			// Create alibaba-credentials Secret with OIDC provider ARN + role ARN
			By("creating alibaba-credentials Secret with OIDC and role ARN")
			providerRoleARN := fmt.Sprintf("acs:ram::%s:role/%s", TestCfg.SourceAccountID, ProviderRoleName)
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alibaba-credentials",
					Namespace: "kube-system",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"oidcproviderarn": []byte(TestCfg.OIDCProviderARN),
					"rolearn":         []byte(providerRoleARN),
				},
			}
			_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			err := TestFramework.CreateSecret(ctx, "kube-system", credSecret)
			Expect(err).NotTo(HaveOccurred(), "failed to create alibaba-credentials Secret")
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			})

			// Configure DaemonSet with RRSA env vars
			By("configuring DaemonSet with RRSA env vars")
			patchDaemonSetAuth(ctx, "rrsa")

			// Verify Provider Role has KMS policy
			By("verifying Provider Role has KMS policy attached")
			err = GlobalResourceManager.VerifyRolePolicyAttachment(ProviderRoleName, KMSPolicyName)
			Expect(err).NotTo(HaveOccurred(), "Provider Role missing KMS policy attachment")

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, RRSASecretName)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume")
			createTestPod(ctx, ns, podName, spcName, "", "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", RRSASecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "rrsa-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-003: RAM Role (AK/SK + RoleArn) authentication (auth chain priority: 3)
	Context("TC-003: RAM Role", func() {
		var (
			spcName = "tc003-ramrole-spc"
			podName = "tc003-ramrole-pod"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via AK/SK + RoleArn AssumeRole and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			if TestCfg.AccessKeyID == "" || TestCfg.AccessKeySecret == "" {
				Skip("AK/SK credentials not configured")
			}

			// Step 1: Create RAM User with sts:AssumeRole permission
			By("creating RAM User with sts:AssumeRole permission")
			ramUserName := fmt.Sprintf("tc003-provider-%d", time.Now().Unix())
			assumeRolePolicyName := fmt.Sprintf("tc003-assume-role-%d", time.Now().Unix())
			assumeRolePolicyDoc := fmt.Sprintf(
				`{"Statement":[{"Action":"sts:AssumeRole","Effect":"Allow","Resource":"acs:ram::%s:role/%s"}],"Version":"1"}`,
				TestCfg.SourceAccountID, RAMRoleName)

			providerAK, providerSK, err := GlobalResourceManager.CreateRAMUserWithAccessKey(
				ramUserName, assumeRolePolicyName, assumeRolePolicyDoc)
			Expect(err).NotTo(HaveOccurred(), "failed to create RAM user for TC-003")
			defer func() {
				GlobalResourceManager.DeleteRAMUserWithAccessKey(ramUserName, providerAK, assumeRolePolicyName)
			}()

			// Step 2: Create K8s Secret with AK/SK + RoleArn
			By("creating alibaba-credentials Secret with user AK/SK and RoleArn")
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alibaba-credentials",
					Namespace: "kube-system",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"id":      []byte(providerAK),
					"secret":  []byte(providerSK),
					"rolearn": []byte(TestCfg.RAMRoleArn),
				},
			}
			_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			err = TestFramework.CreateSecret(ctx, "kube-system", credSecret)
			Expect(err).NotTo(HaveOccurred(), "failed to create alibaba-credentials Secret")
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			})

			// Step 3: Configure DaemonSet with AK/SK + RoleArn
			By("switching DaemonSet to aksk_role mode")
			patchDaemonSetAuth(ctx, "aksk_role")

			By("Waiting for IAM propagation of TC-003 AK/SK (AssumeRole verification)")
			// Use AssumeRole verification instead of GetCallerIdentity.
			// GetCallerIdentity only verifies AK/SK identity (a local/simple check),
			// but does NOT verify the AK/SK can call AssumeRole for the target role.
			// Newly created RAM user AK/SK may pass GetCallerIdentity immediately but fail
			// AssumeRole due to IAM eventual consistency (InvalidAccessKeyId.NotFound).
			// We must verify the actual AssumeRole path that the Provider will use.
			var assumeRoleReady bool
			for i := 0; i < 36; i++ { // max 36 * 5s = 180s
				verifyCmd := exec.Command("aliyun", "sts", "AssumeRole",
					"--access-key-id", providerAK, "--access-key-secret", providerSK,
					"--RoleArn", TestCfg.RAMRoleArn, "--RoleSessionName", "tc003-propagation-check")
				if out, err := verifyCmd.CombinedOutput(); err == nil {
					log.Printf("TC-003 AssumeRole propagation verified after %d attempts", i+1)
					_ = out
					assumeRoleReady = true
					break
				} else if i == 35 {
					log.Printf("WARNING: TC-003 AssumeRole propagation timeout after 180s: %s", string(out))
				}
				time.Sleep(5 * time.Second)
			}

			// Additional buffer wait for KMS-level IAM propagation.
			// Even after AssumeRole succeeds, KMS endpoints may have additional
			// eventual consistency delay for the newly assumed role credentials.
			if assumeRoleReady {
				log.Printf("TC-003: waiting 15s for KMS-level IAM propagation buffer")
				time.Sleep(15 * time.Second)
			}

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, RAMRoleSecretName)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume")
			createTestPod(ctx, ns, podName, spcName, "", "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", RAMRoleSecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "ram-role-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-004: Node Publish Secret authentication (auth chain priority: 4)
	Context("TC-004: Node Publish Secret", func() {
		var (
			spcName       = "tc004-nodepub-spc"
			podName       = "tc004-nodepub-pod"
			nodePubSecret = "tc004-node-publish-secret"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via nodePublishSecretRef and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			if TestCfg.AccessKeyID == "" || TestCfg.AccessKeySecret == "" {
				Skip("AK/SK credentials not configured")
			}

			// DaemonSet env is already cleared by clearAllAuth() in BeforeEach.
			// Create RAM User with unified KMS policy
			By("creating RAM User with unified KMS policy for node publish auth")
			ramUserName := fmt.Sprintf("tc004-nodepub-%d", time.Now().Unix())
			ak, sk, err := GlobalResourceManager.CreateRAMUserWithKMSPolicy(ramUserName, KMSPolicyName)
			Expect(err).NotTo(HaveOccurred(), "failed to create RAM user for TC-004")
			defer func() {
				GlobalResourceManager.DeleteRAMUserWithKMSPolicy(ramUserName, ak, KMSPolicyName)
			}()

			// Create K8s Secret with AK/SK in Pod namespace
			By("creating nodePublishSecret in test namespace")
			nodePubSecretObj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nodePubSecret,
					Namespace: ns,
					Labels: map[string]string{
						"secrets-store.csi.k8s.io/used": "true",
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"access_key":    []byte(ak),
					"access_secret": []byte(sk),
				},
			}
			_ = TestFramework.DeleteSecret(ctx, ns, nodePubSecret)
			err = TestFramework.CreateSecret(ctx, ns, nodePubSecretObj)
			Expect(err).NotTo(HaveOccurred(), "failed to create nodePublishSecret")
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecret(ctx, ns, nodePubSecret)
			})

			// DaemonSet has no auth env vars (node publish is Pod-level auth)
			By("DaemonSet stays in none mode (no auth env vars)")

			// Verify RAM User credentials are ready (IAM propagation).
			// Newly created RAM user AK/SK may not be immediately valid across all regions;
			// poll STS GetCallerIdentity to confirm the credentials are active before proceeding.
			// For nodePublishSecret auth, we need AK/SK to be active (not AssumeRole).
			By("waiting for IAM propagation of TC-004 nodePublishSecret AK/SK")
			Eventually(func() error {
				verifyCmd := exec.Command("aliyun", "sts", "GetCallerIdentity",
					"--access-key-id", ak,
					"--access-key-secret", sk,
					"--region", TestCfg.RegionID)
				output, verifyErr := verifyCmd.CombinedOutput()
				if verifyErr != nil {
					log.Printf("TC-004 STS GetCallerIdentity not yet ready: %s", string(output))
					return verifyErr
				}
				log.Printf("TC-004 IAM propagation verified: AK/SK is active (%s)", string(output))
				return nil
			}, 120*time.Second, 5*time.Second).Should(Succeed(),
				"TC-004 RAM User AK/SK did not become active within 120s")

			// Additional buffer wait for KMS-level IAM propagation.
			// Even after GetCallerIdentity succeeds, KMS endpoints may have additional
			// eventual consistency delay for the newly created credentials.
			log.Printf("TC-004: waiting 15s for KMS-level IAM propagation buffer")
			time.Sleep(15 * time.Second)

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, NodePubSecretName)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume and nodePublishSecretRef")
			podObj := buildCSIPodWithNodePublishSecret(podName, spcName, "", "/mnt/secrets-store", nodePubSecret)
			// Diagnostic: verify nodePublishSecretRef is set on the Pod's CSI volume
			volumes, _, _ := unstructured.NestedSlice(podObj.Object, "spec", "volumes")
			if len(volumes) > 0 {
				if csi, found, _ := unstructured.NestedMap(volumes[0].(map[string]interface{}), "csi"); found {
					if _, hasSecretRef := csi["nodePublishSecretRef"]; hasSecretRef {
						log.Printf("TC-004: nodePublishSecretRef is configured on Pod CSI volume (secret: %s)", nodePubSecret)
					} else {
						log.Printf("WARNING: TC-004: nodePublishSecretRef is MISSING from Pod CSI volume!")
					}
				}
			}
			err = TestFramework.CreatePod(ctx, ns, podObj)
			Expect(err).NotTo(HaveOccurred(), "failed to create pod %s", podName)
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", NodePubSecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "node-pub-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-005: AK/SK authentication (auth chain priority: 5)
	Context("TC-005: AK/SK Static", func() {
		var (
			spcName = "tc005-aksk-spc"
			podName = "tc005-aksk-pod"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via AK/SK and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			if TestCfg.AccessKeyID == "" || TestCfg.AccessKeySecret == "" {
				Skip("AK/SK credentials not configured")
			}

			// Create RAM User with unified KMS policy
			By("creating RAM User with unified KMS policy")
			ramUserName := fmt.Sprintf("tc005-aksk-%d", time.Now().Unix())
			ak, sk, err := GlobalResourceManager.CreateRAMUserWithKMSPolicy(ramUserName, KMSPolicyName)
			Expect(err).NotTo(HaveOccurred(), "failed to create RAM user for TC-005")
			defer func() {
				GlobalResourceManager.DeleteRAMUserWithKMSPolicy(ramUserName, ak, KMSPolicyName)
			}()

			// Create alibaba-credentials Secret with AK/SK
			By("creating alibaba-credentials Secret with AK/SK in kube-system")
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
			_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			err = TestFramework.CreateSecret(ctx, "kube-system", credSecret)
			Expect(err).NotTo(HaveOccurred(), "failed to create alibaba-credentials Secret")
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			})

			// Configure DaemonSet with AK/SK
			By("switching DaemonSet to AK/SK mode")
			patchDaemonSetAuth(ctx, "aksk")

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, AKSKSecretName)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume")
			createTestPod(ctx, ns, podName, spcName, "", "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", AKSKSecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "aksk-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-006: Cross-account authentication
	Context("TC-006: Cross-Account", func() {
		var (
			spcName = "tc006-cross-spc"
			podName = "tc006-cross-pod"
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via cross-account AssumeRole and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			if TestCfg.SkipCrossAccount || TestCfg.TargetAccountID == "" {
				Skip("Target account not configured")
			}
			if TestCfg.AccessKeyID == "" || TestCfg.AccessKeySecret == "" {
				Skip("AK/SK credentials not configured")
			}

			// Create source account RAM User with KMS policy + STS AssumeRole for cross-account
			By("creating source account RAM User for cross-account auth")
			ramUserName := fmt.Sprintf("tc006-cross-%d", time.Now().Unix())
			assumeRolePolicyName := fmt.Sprintf("tc006-cross-policy-%d", time.Now().Unix())
			crossRoleARN := TestCfg.TargetRoleARN
			assumeRolePolicyDoc := fmt.Sprintf(
				`{"Statement":[{"Action":"sts:AssumeRole","Effect":"Allow","Resource":"%s"}],"Version":"1"}`,
				crossRoleARN)

			ak, sk, err := GlobalResourceManager.CreateRAMUserWithAccessKey(
				ramUserName, assumeRolePolicyName, assumeRolePolicyDoc)
			Expect(err).NotTo(HaveOccurred(), "failed to create RAM user for TC-006")
			defer func() {
				GlobalResourceManager.DeleteRAMUserWithAccessKey(ramUserName, ak, assumeRolePolicyName)
			}()

			// Source account RAM User only needs sts:AssumeRole (already attached above)
			// Do NOT attach KMS policy — only target account role needs KMS access

			// Create K8s Secret with AK/SK only (crossAccountRoleArn in SPC handles AssumeRole)
			By("creating alibaba-credentials Secret with cross-account AK/SK credentials")
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
			_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			err = TestFramework.CreateSecret(ctx, "kube-system", credSecret)
			Expect(err).NotTo(HaveOccurred(), "failed to create alibaba-credentials Secret")
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecret(ctx, "kube-system", "alibaba-credentials")
			})

			// Configure DaemonSet with pure AK/SK (crossAccountRoleArn in SPC handles AssumeRole)
			By("switching DaemonSet to aksk mode")
			patchDaemonSetAuth(ctx, "aksk")

			By("Waiting for IAM propagation of TC-006 cross-account AK/SK")
			// Use AssumeRole verification instead of GetCallerIdentity.
			// GetCallerIdentity only verifies AK/SK identity (a local/simple check),
			// but does NOT verify the AK/SK can call AssumeRole for cross-account access.
			// Newly created RAM user AK/SK may pass GetCallerIdentity immediately but fail
			// AssumeRole due to IAM eventual consistency (InvalidAccessKeyId.NotFound).
			// We must verify the actual AssumeRole path that the Provider will use.
			var assumeRoleReady bool
			for i := 0; i < 36; i++ { // max 36 * 5s = 180s
				verifyCmd := exec.Command("aliyun", "sts", "AssumeRole",
					"--access-key-id", ak, "--access-key-secret", sk,
					"--RoleArn", crossRoleARN, "--RoleSessionName", "tc006-propagation-check")
				if out, err := verifyCmd.CombinedOutput(); err == nil {
					log.Printf("TC-006 AssumeRole propagation verified after %d attempts", i+1)
					_ = out
					assumeRoleReady = true
					break
				} else if i == 35 {
					log.Printf("WARNING: TC-006 AssumeRole propagation timeout after 180s: %s", string(out))
				}
				time.Sleep(5 * time.Second)
			}

			// Additional buffer wait for KMS-level IAM propagation.
			// Even after AssumeRole succeeds, KMS endpoints may have additional
			// eventual consistency delay for the newly assumed role credentials.
			if assumeRoleReady {
				log.Printf("TC-006: waiting 15s for KMS-level IAM propagation buffer")
				time.Sleep(15 * time.Second)
			}

			By("creating SecretProviderClass with crossAccountRoleArn")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"
  crossAccountRoleArn: "%s"`, CrossAccountSecret, crossRoleARN)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume")
			createTestPod(ctx, ns, podName, spcName, "", "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", CrossAccountSecret)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "cross-account-value")
			Expect(err).NotTo(HaveOccurred(), "cross-account mounted secret content mismatch")
		})

		AfterEach(func() {
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})

	// TC-007: ECS RAM Role authentication (auth chain priority: 6 - fallback)
	Context("TC-007: ECS RAM Role", func() {
		var (
			spcName        = "tc007-ecsrole-spc"
			podName        = "tc007-ecsrole-pod"
			workerRoleName string
		)

		BeforeEach(func() {
			clearAllAuth()
		})

		It("should authenticate via ECS RAM Role and mount KMS secret", func() {
			ctx := TestCtx
			ns := TestCfg.Namespace

			// DaemonSet env is already cleared by clearAllAuth() in BeforeEach.

			// Get cluster worker role name
			By("getting cluster worker RAM role name")
			var err error
			workerRoleName, err = GlobalResourceManager.GetECSWorkerRoleName()
			Expect(err).NotTo(HaveOccurred(), "failed to get ECS worker role name")

			// Attach unified KMS policy to worker role
			By(fmt.Sprintf("attaching KMS policy to worker role: %s", workerRoleName))
			err = GlobalResourceManager.AttachPolicyToRole(KMSPolicyName, workerRoleName)
			Expect(err).NotTo(HaveOccurred(), "failed to attach KMS policy to worker role")

			// DaemonSet has no auth env vars — ECS RAM Role is the fallback
			By("DaemonSet stays in none mode (ECS RAM Role fallback)")

			By("creating SecretProviderClass")
			objectsYAML := fmt.Sprintf(`- objectName: "%s"
  objectType: "kms"`, ECSRoleSecretName)
			createSPC(ctx, ns, spcName, "alibabacloud", objectsYAML, nil)
			DeferCleanup(func() {
				_ = TestFramework.DeleteSecretProviderClass(ctx, ns, spcName)
			})

			By("creating Pod with CSI volume")
			createTestPod(ctx, ns, podName, spcName, "", "/mnt/secrets-store")
			DeferCleanup(func() {
				_ = TestFramework.DeletePod(ctx, ns, podName)
			})

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(ctx, ns, podName, 3*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod did not become ready")

			By("verifying mounted secret content")
			secretPath := fmt.Sprintf("/mnt/secrets-store/%s", ECSRoleSecretName)
			err = TestFramework.VerifyMountedFile(ctx, podName, ns, secretPath, "ecs-role-test-value")
			Expect(err).NotTo(HaveOccurred(), "mounted secret content mismatch")
		})

		AfterEach(func() {
			// Detach KMS policy from worker role (cleanup)
			if workerRoleName != "" {
				_ = GlobalResourceManager.DetachPolicyFromRole(KMSPolicyName, workerRoleName)
			}
			cleanupTestResources(TestCtx, TestCfg.Namespace)
		})
	})
})
