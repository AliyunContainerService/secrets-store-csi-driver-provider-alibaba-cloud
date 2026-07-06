// framework/cleanup.go - Cleanup helpers for E2E tests
package framework

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
)

// CleanupNamespace deletes a namespace and waits for it to be fully removed
func CleanupNamespace(ctx context.Context, f *Framework, namespace string) {
	if namespace == "" {
		return
	}
	_ = f.DeleteNamespace(ctx, namespace)
	_ = f.WaitForNamespaceDeleted(ctx, namespace, 2*time.Minute)
}

// CleanupKMSSecret deletes a KMS Secret via ResourceManager
func CleanupKMSSecret(rm CloudResourceManager, name string) {
	if name == "" {
		return
	}
	_ = rm.DeleteKMSSecret(name)
}

// CleanupRAMResources detaches policies and deletes RAM roles and policies
func CleanupRAMResources(rm CloudResourceManager, policyNames, roleNames []string) {
	// Detach policies from roles first
	for _, roleName := range roleNames {
		for _, policyName := range policyNames {
			_ = rm.DetachPolicyFromRole(policyName, roleName)
		}
	}

	// Delete roles
	for _, roleName := range roleNames {
		_ = rm.DeleteRAMRole(roleName)
	}

	// Delete policies
	for _, policyName := range policyNames {
		_ = rm.DeleteRAMPolicy(policyName)
	}
}

// CloudResourceManager defines the interface for cloud resource operations
// This is implemented by the ResourceManager in resource_manager.go
type CloudResourceManager interface {
	DeleteKMSSecret(name string) error
	DeleteRAMRole(name string) error
	DeleteRAMPolicy(name string) error
	DetachPolicyFromRole(policyName, roleName string) error
}

// DeferCleanupNamespace registers a namespace cleanup with Ginkgo's DeferCleanup
func DeferCleanupNamespace(ctx context.Context, f *Framework, namespace string) {
	DeferCleanup(func() {
		CleanupNamespace(ctx, f, namespace)
	})
}

// DeferCleanupSPC registers a SecretProviderClass cleanup with Ginkgo's DeferCleanup
func DeferCleanupSPC(ctx context.Context, f *Framework, namespace, spcName string) {
	DeferCleanup(func() {
		_ = f.DeleteSecretProviderClass(ctx, namespace, spcName)
	})
}

// DeferCleanupPod registers a Pod cleanup with Ginkgo's DeferCleanup
func DeferCleanupPod(ctx context.Context, f *Framework, namespace, podName string) {
	DeferCleanup(func() {
		_ = f.DeletePod(ctx, namespace, podName)
	})
}

// DeferCleanupKMS registers a KMS Secret cleanup with Ginkgo's DeferCleanup
func DeferCleanupKMS(rm CloudResourceManager, secretName string) {
	DeferCleanup(func() {
		CleanupKMSSecret(rm, secretName)
	})
}

// DeferCleanupRAM registers RAM resource cleanup with Ginkgo's DeferCleanup
func DeferCleanupRAM(rm CloudResourceManager, policyNames, roleNames []string) {
	DeferCleanup(func() {
		CleanupRAMResources(rm, policyNames, roleNames)
	})
}

// TestCleanup aggregates all cleanup operations for a test case
type TestCleanup struct {
	f           *Framework
	rm          CloudResourceManager
	namespace   string
	kmsSecrets  []string
	ramPolicies []string
	ramRoles    []string
	spcNames    []string
	podNames    []string
	ctx         context.Context
}

// NewTestCleanup creates a new TestCleanup instance
func NewTestCleanup(ctx context.Context, f *Framework, rm CloudResourceManager, namespace string) *TestCleanup {
	return &TestCleanup{
		f:         f,
		rm:        rm,
		namespace: namespace,
		ctx:       ctx,
	}
}

// AddKMSSecret adds a KMS Secret to the cleanup list
func (tc *TestCleanup) AddKMSSecret(name string) {
	tc.kmsSecrets = append(tc.kmsSecrets, name)
}

// AddRAMPolicy adds a RAM Policy to the cleanup list
func (tc *TestCleanup) AddRAMPolicy(name string) {
	tc.ramPolicies = append(tc.ramPolicies, name)
}

// AddRAMRole adds a RAM Role to the cleanup list
func (tc *TestCleanup) AddRAMRole(name string) {
	tc.ramRoles = append(tc.ramRoles, name)
}

// AddSPC adds a SecretProviderClass to the cleanup list
func (tc *TestCleanup) AddSPC(name string) {
	tc.spcNames = append(tc.spcNames, name)
}

// AddPod adds a Pod to the cleanup list
func (tc *TestCleanup) AddPod(name string) {
	tc.podNames = append(tc.podNames, name)
}

// Run executes all cleanup operations
func (tc *TestCleanup) Run() {
	var errs []string

	// Delete Pods first
	for _, podName := range tc.podNames {
		if err := tc.f.DeletePod(tc.ctx, tc.namespace, podName); err != nil {
			errs = append(errs, fmt.Sprintf("pod %s: %v", podName, err))
		}
	}

	// Delete SPCs
	for _, spcName := range tc.spcNames {
		if err := tc.f.DeleteSecretProviderClass(tc.ctx, tc.namespace, spcName); err != nil {
			errs = append(errs, fmt.Sprintf("spc %s: %v", spcName, err))
		}
	}

	// Delete KMS Secrets
	for _, name := range tc.kmsSecrets {
		if err := tc.rm.DeleteKMSSecret(name); err != nil {
			errs = append(errs, fmt.Sprintf("kms %s: %v", name, err))
		}
	}

	// Cleanup RAM resources
	CleanupRAMResources(tc.rm, tc.ramPolicies, tc.ramRoles)

	if len(errs) > 0 {
		GinkgoWriter.Printf("Cleanup warnings: %v\n", errs)
	}
}
