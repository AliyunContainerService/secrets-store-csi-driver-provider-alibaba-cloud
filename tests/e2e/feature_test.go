// feature_test.go - Feature tests TC-008~TC-011
package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/tests/e2e/framework"
)

var _ = Describe("Feature Tests", func() {

	BeforeEach(func() {
		// Feature tests reuse existing auth if configured, otherwise set up minimal AK/SK auth
		ensureAuthConfigured()
	})

	// ========================================================================
	// TC-008: JMESPath JSON Parsing
	// ========================================================================
	Context("TC-008: JMESPath JSON Parsing", func() {
		var (
			kmsSecretName string
			spcName       = "jmespath-test"
			podName       = "jmespath-test"
			mountPath     = "/mnt/secrets-store"
		)

		BeforeEach(func() {
			kmsSecretName = "jmespath-test-" + TestSuffix
		})

		It("should extract JSON fields using JMESPath expressions", func() {
			By("creating SecretProviderClass with jmesPath configuration")
			spc := buildJMESPathSPC(spcName, kmsSecretName)
			err := TestFramework.CreateSecretProviderClass(TestCtx, TestFramework.Namespace, spc)
			Expect(err).NotTo(HaveOccurred(), "Failed to create JMESPath SPC")

			By("deploying test Pod")
			pod := buildTestPod(podName, spcName, mountPath)
			err = TestFramework.CreatePod(TestCtx, TestFramework.Namespace, pod)
			Expect(err).NotTo(HaveOccurred(), "Failed to create JMESPath Pod")

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(TestCtx, TestFramework.Namespace, podName, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "JMESPath Pod not ready")

			By("verifying JMESPath extracted username field")
			err = TestFramework.VerifyMountedFile(TestCtx, podName, TestFramework.Namespace,
				mountPath+"/myUsername", "testUser")
			Expect(err).NotTo(HaveOccurred(), "JMESPath username extraction failed")

			By("verifying JMESPath extracted password field")
			err = TestFramework.VerifyMountedFile(TestCtx, podName, TestFramework.Namespace,
				mountPath+"/myPassword", "testPassword")
			Expect(err).NotTo(HaveOccurred(), "JMESPath password extraction failed")
		})

		AfterEach(func() {
			cleanupFeatureTestResources(podName, spcName, kmsSecretName)
		})
	})

	// ========================================================================
	// TC-009: Secret Rotation
	// ========================================================================
	Context("TC-009: Secret Rotation", func() {
		var (
			kmsSecretName string
			spcName       = "rotation-test"
			podName       = "rotation-test"
			mountPath     = "/mnt/secrets-store"
		)

		BeforeEach(func() {
			kmsSecretName = "rotation-test-" + TestSuffix
		})

		It("should reflect updated KMS Secret value after rotation", func() {
			By("creating SecretProviderClass")
			spc := buildSimpleSPC(spcName, kmsSecretName)
			err := TestFramework.CreateSecretProviderClass(TestCtx, TestFramework.Namespace, spc)
			Expect(err).NotTo(HaveOccurred(), "Failed to create rotation SPC")

			By("deploying test Pod")
			pod := buildTestPod(podName, spcName, mountPath)
			err = TestFramework.CreatePod(TestCtx, TestFramework.Namespace, pod)
			Expect(err).NotTo(HaveOccurred(), "Failed to create rotation Pod")

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(TestCtx, TestFramework.Namespace, podName, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Rotation Pod not ready")

			By("verifying initial mounted value")
			err = TestFramework.VerifyMountedFile(TestCtx, podName, TestFramework.Namespace,
				mountPath+"/"+kmsSecretName, "before-rotation")
			Expect(err).NotTo(HaveOccurred(), "Initial rotation value mismatch")

			By("updating KMS Secret to trigger rotation")
			err = GlobalResourceManager.UpdateKMSSecret(kmsSecretName, "after-rotation", "v2")
			Expect(err).NotTo(HaveOccurred(), "Failed to update KMS secret for rotation")

			By("waiting for KMS eventual consistency")
			time.Sleep(15 * time.Second)

			By("waiting for rotation to take effect and verifying updated value")
			Eventually(func() error {
				return TestFramework.VerifyMountedFile(TestCtx, podName, TestFramework.Namespace,
					mountPath+"/"+kmsSecretName, "after-rotation")
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(),
				"Secret rotation did not take effect")
		})

		AfterEach(func() {
			cleanupFeatureTestResources(podName, spcName, kmsSecretName)
		})
	})

	// ========================================================================
	// TC-010: K8s Secret Sync (secretObjects)
	// ========================================================================
	Context("TC-010: K8s Secret Sync", func() {
		var (
			kmsSecretName string
			k8sSecretName string
			spcName       = "sync-test"
			podName       = "sync-test"
			mountPath     = "/mnt/secrets-store"
		)

		BeforeEach(func() {
			kmsSecretName = "sync-test-" + TestSuffix
			k8sSecretName = "synced-secret-" + TestSuffix
		})

		It("should sync KMS Secret to K8s Secret via secretObjects", func() {
			By("creating SecretProviderClass with secretObjects")
			spc := buildSecretSyncSPC(spcName, kmsSecretName, k8sSecretName)
			err := TestFramework.CreateSecretProviderClass(TestCtx, TestFramework.Namespace, spc)
			Expect(err).NotTo(HaveOccurred(), "Failed to create sync SPC")

			By("deploying test Pod")
			pod := buildTestPod(podName, spcName, mountPath)
			err = TestFramework.CreatePod(TestCtx, TestFramework.Namespace, pod)
			Expect(err).NotTo(HaveOccurred(), "Failed to create sync Pod")

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(TestCtx, TestFramework.Namespace, podName, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Sync Pod not ready")

			By("verifying K8s Secret was synced")
			err = TestFramework.VerifySecretExists(TestCtx, k8sSecretName, TestFramework.Namespace)
			Expect(err).NotTo(HaveOccurred(), "Synced K8s Secret not found")

			By("verifying synced K8s Secret contains correct data")
			err = TestFramework.VerifySecretData(TestCtx, k8sSecretName, TestFramework.Namespace,
				"value", "sync-test-value")
			Expect(err).NotTo(HaveOccurred(), "Synced secret data mismatch")
		})

		AfterEach(func() {
			cleanupFeatureTestResources(podName, spcName, kmsSecretName)
		})
	})

	// ========================================================================
	// TC-011: Cleanup After Deletion
	// ========================================================================
	Context("TC-011: Cleanup After Deletion", func() {
		var (
			kmsSecretName string
			k8sSecretName string
			spcName       = "cleanup-test"
			podName       = "cleanup-test"
			mountPath     = "/mnt/secrets-store"
		)

		BeforeEach(func() {
			kmsSecretName = "cleanup-test-" + TestSuffix
			k8sSecretName = "cleanup-synced-" + TestSuffix
		})

		It("should clean up K8s Secret after Pod deletion", func() {
			By("creating SecretProviderClass with secretObjects")
			spc := buildSecretSyncSPC(spcName, kmsSecretName, k8sSecretName)
			err := TestFramework.CreateSecretProviderClass(TestCtx, TestFramework.Namespace, spc)
			Expect(err).NotTo(HaveOccurred(), "Failed to create cleanup SPC")

			By("deploying test Pod")
			pod := buildTestPod(podName, spcName, mountPath)
			err = TestFramework.CreatePod(TestCtx, TestFramework.Namespace, pod)
			Expect(err).NotTo(HaveOccurred(), "Failed to create cleanup Pod")

			By("waiting for Pod to be ready")
			err = TestFramework.WaitForPodReady(TestCtx, TestFramework.Namespace, podName, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Cleanup Pod not ready")

			By("verifying K8s Secret was synced before deletion")
			err = TestFramework.VerifySecretExists(TestCtx, k8sSecretName, TestFramework.Namespace)
			Expect(err).NotTo(HaveOccurred(), "K8s Secret not synced before deletion test")

			By("deleting Pod to trigger Secret cleanup")
			err = TestFramework.DeletePod(TestCtx, TestFramework.Namespace, podName)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete Pod")

			By("waiting for Pod to be fully deleted")
			err = TestFramework.WaitForPodDeleted(TestCtx, TestFramework.Namespace, podName, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Pod not deleted")

			By("verifying K8s Secret was cleaned up")
			err = TestFramework.VerifySecretNotExists(TestCtx, k8sSecretName, TestFramework.Namespace)
			Expect(err).NotTo(HaveOccurred(), "K8s Secret not cleaned up after Pod deletion")
		})

		AfterEach(func() {
			// Best-effort cleanup: Pod may already be deleted, SPC and KMS secret still need cleanup
			cleanupFeatureTestResources(podName, spcName, kmsSecretName)
		})
	})
})

// ============================================================================
// Helper functions for building test resources
// ============================================================================

// buildJMESPathSPC constructs a SecretProviderClass with jmesPath configuration
func buildJMESPathSPC(name, kmsSecretName string) *unstructured.Unstructured {
	objectsYAML := fmt.Sprintf(`- objectName: "%s"
  jmesPath:
    - path: "username"
      objectAlias: "myUsername"
    - path: "password"
      objectAlias: "myPassword"
`, kmsSecretName)

	spc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "secrets-store.csi.x-k8s.io/v1",
			"kind":       "SecretProviderClass",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"provider": "alibabacloud",
				"parameters": map[string]interface{}{
					"objects": objectsYAML,
				},
			},
		},
	}
	return spc
}

// buildSimpleSPC constructs a basic SecretProviderClass with a single object
func buildSimpleSPC(name, kmsSecretName string) *unstructured.Unstructured {
	objectsYAML := fmt.Sprintf(`- objectName: "%s"
`, kmsSecretName)

	spc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "secrets-store.csi.x-k8s.io/v1",
			"kind":       "SecretProviderClass",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"provider": "alibabacloud",
				"parameters": map[string]interface{}{
					"objects": objectsYAML,
				},
			},
		},
	}
	return spc
}

// buildSecretSyncSPC constructs a SecretProviderClass with secretObjects for K8s Secret sync
func buildSecretSyncSPC(name, kmsSecretName, k8sSecretName string) *unstructured.Unstructured {
	objectsYAML := fmt.Sprintf(`- objectName: "%s"
`, kmsSecretName)

	spc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "secrets-store.csi.x-k8s.io/v1",
			"kind":       "SecretProviderClass",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"provider": "alibabacloud",
				"secretObjects": []interface{}{
					map[string]interface{}{
						"secretName": k8sSecretName,
						"type":       "Opaque",
						"data": []interface{}{
							map[string]interface{}{
								"objectName": kmsSecretName,
								"key":        "value",
							},
						},
					},
				},
				"parameters": map[string]interface{}{
					"objects": objectsYAML,
				},
			},
		},
	}
	return spc
}

// buildTestPod constructs a Pod with CSI volume mount for secret-store
func buildTestPod(name, spcName, mountPath string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"app": name,
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":    name,
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
	return pod
}

// cleanupFeatureTestResources deletes Pod, SPC, and KMS Secret with best-effort semantics
func cleanupFeatureTestResources(podName, spcName, kmsSecretName string) {
	_ = TestFramework.DeletePod(TestCtx, TestFramework.Namespace, podName)
	_ = TestFramework.DeleteSecretProviderClass(TestCtx, TestFramework.Namespace, spcName)
	_ = GlobalResourceManager.DeleteKMSSecret(kmsSecretName)
}
