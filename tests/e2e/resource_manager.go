// resource_manager.go - Alibaba Cloud resource manager for E2E tests
package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	kms20160120 "github.com/alibabacloud-go/kms-20160120/v3/client"
	ram20150501 "github.com/alibabacloud-go/ram-20150501/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
)

const (
	// TestResourcePrefix is the prefix for all test resources
	TestResourcePrefix = "csi-provider-test-"
	// kmsEndpointFormat is the KMS API endpoint format (region-based)
	kmsEndpointFormat = "kms.%s.aliyuncs.com"
	// ramEndpoint is the RAM API endpoint
	ramEndpoint = "ram.aliyuncs.com"
	// UnifiedKMSPolicyDoc is the shared KMS policy granted to all roles and users
	UnifiedKMSPolicyDoc = `{"Version":"1","Statement":[{"Effect":"Allow","Action":["kms:GetSecretValue","kms:Decrypt"],"Resource":"*"}]}`
)

// RAMUser tracks a created RAM user and its associated resources for cleanup
type RAMUser struct {
	Username         string
	AccessKeyID      string
	AssociatedPolicy string
}

// TestConfig holds the test configuration loaded from environment
type TestConfig struct {
	SourceAccountID string
	TargetAccountID string
	ClusterID       string
	Namespace       string
	Kubeconfig      string

	// AK/SK for AK/SK auth tests
	AccessKeyID     string
	AccessKeySecret string

	// Target account AK/SK (for cross-account resource management)
	TargetAccessKeyID     string
	TargetAccessKeySecret string
	TargetRoleARN         string

	// OIDC Provider ARN (auto-constructed if empty)
	OIDCProviderARN string

	// KMS encryption config
	EncryptionKeyID string
	DKMSInstanceID  string

	// Target account KMS config (for cross-account DKMS scenarios)
	TargetEncryptionKeyID string
	TargetDKMSInstanceID  string

	// RAM Role ARN for RAM Role (AK/SK + RoleArn) auth tests
	RAMRoleArn string

	// Region for KMS endpoint
	RegionID string

	// Test control
	SkipCrossAccount   bool
	SkipProviderDeploy bool
	AutoCleanupKMS     bool
	AutoCleanupRAM     bool
}

// LoadTestConfig loads test configuration from environment variables
func LoadTestConfig() *TestConfig {
	cfg := &TestConfig{
		SourceAccountID:       os.Getenv("SOURCE_ACCOUNT_ID"),
		TargetAccountID:       os.Getenv("TARGET_ACCOUNT_ID"),
		ClusterID:             os.Getenv("CLUSTER_ID"),
		Namespace:             os.Getenv("NAMESPACE"),
		Kubeconfig:            os.Getenv("KUBECONFIG"),
		AccessKeyID:           os.Getenv("ALIBABA_ACCESS_KEY_ID"),
		AccessKeySecret:       os.Getenv("ALIBABA_ACCESS_KEY_SECRET"),
		TargetAccessKeyID:     os.Getenv("TARGET_ACCOUNT_ACCESS_KEY_ID"),
		TargetAccessKeySecret: os.Getenv("TARGET_ACCOUNT_ACCESS_KEY_SECRET"),
		OIDCProviderARN:       os.Getenv("OIDC_PROVIDER_ARN"),
		EncryptionKeyID:       os.Getenv("ENCRYPTION_KEY_ID"),
		DKMSInstanceID:        os.Getenv("DKMS_INSTANCE_ID"),
		TargetEncryptionKeyID: os.Getenv("TARGET_ACCOUNT_ENCRYPTION_KEY_ID"),
		TargetDKMSInstanceID:  os.Getenv("TARGET_ACCOUNT_DKMS_INSTANCE_ID"),
		RegionID:              os.Getenv("REGION"),
		SkipCrossAccount:      os.Getenv("SKIP_CROSS_ACCOUNT") == "true",
		SkipProviderDeploy:    os.Getenv("SKIP_PROVIDER_DEPLOY") == "true",
		AutoCleanupKMS:        os.Getenv("AUTO_CLEANUP_KMS") != "false",
		AutoCleanupRAM:        os.Getenv("AUTO_CLEANUP_RAM") != "false",
		RAMRoleArn:            os.Getenv("RAM_ROLE_ARN"),
	}

	if cfg.Namespace == "" {
		cfg.Namespace = "staging"
	}

	// Auto-construct OIDC Provider ARN if not set
	if cfg.OIDCProviderARN == "" && cfg.SourceAccountID != "" && cfg.ClusterID != "" {
		cfg.OIDCProviderARN = fmt.Sprintf("acs:ram::%s:oidc-provider/ack-rrsa-%s",
			cfg.SourceAccountID, cfg.ClusterID)
	}

	if cfg.TargetAccountID == "" {
		cfg.SkipCrossAccount = true
	}

	return cfg
}

// Validate checks that required configuration is present
func (c *TestConfig) Validate() error {
	if c.SourceAccountID == "" {
		return fmt.Errorf("source account ID (SOURCE_ACCOUNT_ID) is required")
	}
	if c.RegionID == "" {
		return fmt.Errorf("region (REGION) environment variable is required")
	}
	if c.ClusterID == "" {
		return fmt.Errorf("cluster ID (CLUSTER_ID) environment variable is required")
	}
	return nil
}

// ResourceManager manages Alibaba Cloud resources for E2E tests
type ResourceManager struct {
	kmsClient *kms20160120.Client
	ramClient *ram20150501.Client
	config    *TestConfig

	mu              sync.Mutex
	createdSecrets  []string
	createdPolicies []string
	createdRoles    []string

	// Target account resources (for cross-account testing)
	targetKMSSecrets  []string
	targetRAMPolicies []string
	targetRAMRoles    []string

	// Created RAM users (for cleanup) - tracks username, AK, and associated policy
	createdUsers []RAMUser
}

// NewResourceManager creates a new ResourceManager instance
func NewResourceManager(config *TestConfig) (*ResourceManager, error) {
	// Explicitly use source account AK/SK for default client
	cred, err := credential.NewCredential(&credential.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     tea.String(config.AccessKeyID),
		AccessKeySecret: tea.String(config.AccessKeySecret),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	// Create KMS client
	kmsConfig := &openapi.Config{
		Credential: cred,
		Endpoint:   tea.String(fmt.Sprintf(kmsEndpointFormat, config.RegionID)),
	}
	kmsClient, err := kms20160120.NewClient(kmsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create KMS client: %w", err)
	}

	// Create RAM client
	ramConfig := &openapi.Config{
		Credential: cred,
	}
	ramConfig.Endpoint = tea.String(ramEndpoint)
	ramClient, err := ram20150501.NewClient(ramConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RAM client: %w", err)
	}

	return &ResourceManager{
		kmsClient: kmsClient,
		ramClient: ramClient,
		config:    config,
	}, nil
}

// CreateKMSSecret creates a KMS Secret using source account config
func (rm *ResourceManager) CreateKMSSecret(name, secretData, versionID string) error {
	return rm.createKMSSecretWithConfig(name, secretData, versionID, rm.config.EncryptionKeyID, rm.config.DKMSInstanceID)
}

// CreateTargetKMSSecret creates a KMS Secret in target account with target account DKMS config
func (rm *ResourceManager) CreateTargetKMSSecret(name, secretData, versionID string) error {
	return rm.createKMSSecretWithConfig(name, secretData, versionID, rm.config.TargetEncryptionKeyID, rm.config.TargetDKMSInstanceID)
}

// createKMSSecretWithConfig creates a KMS Secret with specified DKMS configuration
func (rm *ResourceManager) createKMSSecretWithConfig(name, secretData, versionID, encryptionKeyID, dkmsInstanceID string) error {
	req := &kms20160120.CreateSecretRequest{
		SecretName: tea.String(name),
		SecretData: tea.String(secretData),
		VersionId:  tea.String(versionID),
	}
	if encryptionKeyID != "" {
		req.EncryptionKeyId = tea.String(encryptionKeyID)
	}
	if dkmsInstanceID != "" {
		req.DKMSInstanceId = tea.String(dkmsInstanceID)
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.kmsClient.CreateSecret(req)
		if err == nil {
			rm.mu.Lock()
			rm.createdSecrets = append(rm.createdSecrets, name)
			rm.mu.Unlock()
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create KMS secret %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to create KMS secret %s after 3 attempts: %w", name, lastErr)
}

// UpdateKMSSecret updates a KMS Secret with a new version
func (rm *ResourceManager) UpdateKMSSecret(name, secretData, versionID string) error {
	req := &kms20160120.PutSecretValueRequest{
		SecretName: tea.String(name),
		SecretData: tea.String(secretData),
		VersionId:  tea.String(versionID),
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.kmsClient.PutSecretValue(req)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to update KMS secret %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to update KMS secret %s after 3 attempts: %w", name, lastErr)
}

// DeleteKMSSecret deletes a KMS Secret
func (rm *ResourceManager) DeleteKMSSecret(name string) error {
	req := &kms20160120.DeleteSecretRequest{
		SecretName:                 tea.String(name),
		ForceDeleteWithoutRecovery: tea.String("true"),
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.kmsClient.DeleteSecret(req)
		if err == nil {
			return nil
		}
		// Ignore "Resource not found" errors
		if strings.Contains(err.Error(), "Resource not found") {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to delete KMS secret %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to delete KMS secret %s after 3 attempts: %w", name, lastErr)
}

// CreateRAMPolicy creates a RAM Policy and returns the policy name
func (rm *ResourceManager) CreateRAMPolicy(name, document string) (string, error) {
	req := &ram20150501.CreatePolicyRequest{
		PolicyName:     tea.String(name),
		PolicyDocument: tea.String(document),
		Description:    tea.String("CSI Provider E2E Test Policy"),
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.CreatePolicy(req)
		if err == nil {
			rm.mu.Lock()
			rm.createdPolicies = append(rm.createdPolicies, name)
			rm.mu.Unlock()
			return name, nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM policy %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return "", fmt.Errorf("failed to create RAM policy %s after 3 attempts: %w", name, lastErr)
}

// DeleteRAMPolicy deletes a RAM Policy
func (rm *ResourceManager) DeleteRAMPolicy(name string) error {
	// 0. Detach all entities (Users + Roles) from the policy first
	listEntitiesReq := &ram20150501.ListEntitiesForPolicyRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(name),
	}
	entitiesResp, err := rm.ramClient.ListEntitiesForPolicy(listEntitiesReq)
	if err == nil && entitiesResp.Body != nil {
		// Detach from Users
		if entitiesResp.Body.Users != nil && entitiesResp.Body.Users.User != nil {
			for _, user := range entitiesResp.Body.Users.User {
				_ = rm.DetachPolicyFromUser(name, tea.StringValue(user.UserName))
			}
		}
		// Detach from Roles
		if entitiesResp.Body.Roles != nil && entitiesResp.Body.Roles.Role != nil {
			for _, role := range entitiesResp.Body.Roles.Role {
				_ = rm.DetachPolicyFromRole(name, tea.StringValue(role.RoleName))
			}
		}
		userCount := 0
		if entitiesResp.Body.Users != nil && entitiesResp.Body.Users.User != nil {
			userCount = len(entitiesResp.Body.Users.User)
		}
		roleCount := 0
		if entitiesResp.Body.Roles != nil && entitiesResp.Body.Roles.Role != nil {
			roleCount = len(entitiesResp.Body.Roles.Role)
		}
		if userCount > 0 || roleCount > 0 {
			log.Printf("Detached policy %s from %d users and %d roles, waiting 5s for propagation...", name, userCount, roleCount)
			time.Sleep(5 * time.Second)
		}
	}

	req := &ram20150501.DeletePolicyRequest{
		PolicyName: tea.String(name),
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.DeletePolicy(req)
		if err == nil {
			return nil
		}
		// Ignore "Resource not found" errors
		if strings.Contains(err.Error(), "Resource not found") {
			return nil
		}
		lastErr = err

		// Handle DeleteConflict: policy still attached, re-detach and retry
		if strings.Contains(err.Error(), "DeleteConflict") && attempt < 3 {
			log.Printf("DeleteConflict on policy %s (attempt %d/3), re-detaching from entities...", name, attempt)
			if entResp, entErr := rm.ramClient.ListEntitiesForPolicy(&ram20150501.ListEntitiesForPolicyRequest{
				PolicyType: tea.String("Custom"), PolicyName: tea.String(name),
			}); entErr == nil && entResp.Body != nil {
				if entResp.Body.Users != nil && entResp.Body.Users.User != nil {
					for _, u := range entResp.Body.Users.User {
						if detachErr := rm.DetachPolicyFromUser(name, tea.StringValue(u.UserName)); detachErr != nil {
							log.Printf("Warning: re-detach policy %s from user %s failed: %v", name, tea.StringValue(u.UserName), detachErr)
						}
					}
				}
				if entResp.Body.Roles != nil && entResp.Body.Roles.Role != nil {
					for _, r := range entResp.Body.Roles.Role {
						if detachErr := rm.DetachPolicyFromRole(name, tea.StringValue(r.RoleName)); detachErr != nil {
							log.Printf("Warning: re-detach policy %s from role %s failed: %v", name, tea.StringValue(r.RoleName), detachErr)
						}
					}
				}
			}
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		if attempt < 3 {
			log.Printf("Warning: failed to delete RAM policy %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to delete RAM policy %s after 3 attempts: %w", name, lastErr)
}

// CreateRAMRole creates a RAM Role and returns the role name.
// If the role already exists, updates its trust policy to match the desired document.
func (rm *ResourceManager) CreateRAMRole(name, trustPolicy string) (string, error) {
	req := &ram20150501.CreateRoleRequest{
		RoleName:                 tea.String(name),
		AssumeRolePolicyDocument: tea.String(trustPolicy),
		Description:              tea.String("CSI Provider E2E Test Role"),
	}

	// Retry logic for network timeout errors
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.CreateRole(req)
		if err == nil {
			rm.mu.Lock()
			rm.createdRoles = append(rm.createdRoles, name)
			rm.mu.Unlock()
			return name, nil
		}
		// If role already exists, update its trust policy to ensure it matches
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM role %s already exists, updating trust policy...", name)
			if updateErr := rm.UpdateRAMRoleTrustPolicy(name, trustPolicy); updateErr != nil {
				return "", fmt.Errorf("role %s exists but failed to update trust policy: %w", name, updateErr)
			}
			rm.mu.Lock()
			rm.createdRoles = append(rm.createdRoles, name)
			rm.mu.Unlock()
			return name, nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM role %s (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return "", fmt.Errorf("failed to create RAM role %s after 3 attempts: %w", name, lastErr)
}

// UpdateRAMRoleTrustPolicy updates the trust policy (AssumeRolePolicyDocument) of an existing RAM Role
func (rm *ResourceManager) UpdateRAMRoleTrustPolicy(name, trustPolicy string) error {
	req := &ram20150501.UpdateRoleRequest{
		RoleName:                    tea.String(name),
		NewAssumeRolePolicyDocument: tea.String(trustPolicy),
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.UpdateRole(req)
		if err == nil {
			log.Printf("Updated trust policy for RAM role: %s", name)
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to update RAM role %s trust policy (attempt %d/3): %v, retrying...", name, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to update RAM role %s trust policy after 3 attempts: %w", name, lastErr)
}

// DeleteRAMRole deletes a RAM Role with retry and policy detachment verification.
// Handles RAM eventual consistency: DetachPolicy may return success before the server
// state is updated, causing DeleteConflict on DeleteRole. On DeleteConflict, we
// re-list policies, re-detach, wait, and retry.
func (rm *ResourceManager) DeleteRAMRole(name string) error {
	maxRetries := 3

	// 1. Pre-delete: detach all policies with verification
	for attempt := 1; attempt <= maxRetries; attempt++ {
		listReq := &ram20150501.ListPoliciesForRoleRequest{
			RoleName: tea.String(name),
		}
		resp, err := rm.ramClient.ListPoliciesForRole(listReq)
		if err != nil {
			// Retry ListPolicies on failure instead of silently breaking
			if strings.Contains(err.Error(), "EntityNotExist") || strings.Contains(err.Error(), "Resource not found") {
				log.Printf("RAM role %s does not exist, skipping", name)
				return nil
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to list policies for role %s (attempt %d/%d): %v, retrying...", name, attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
				continue
			}
			log.Printf("Warning: failed to list policies for role %s after %d attempts: %v, proceeding with delete", name, maxRetries, err)
			break
		}

		policyCount := 0
		if resp.Body != nil && resp.Body.Policies != nil {
			policyCount = len(resp.Body.Policies.Policy)
		}

		if policyCount == 0 {
			break
		}

		// Detach all policies
		for _, policy := range resp.Body.Policies.Policy {
			if detachErr := rm.DetachPolicyFromRole(tea.StringValue(policy.PolicyName), name); detachErr != nil {
				log.Printf("Warning: failed to detach policy %s from role %s: %v", tea.StringValue(policy.PolicyName), name, detachErr)
			}
		}
		log.Printf("Detached %d policies from role %s, waiting 5s for propagation...", policyCount, name)
		time.Sleep(5 * time.Second)
	}

	// 2. Retry delete the role with DeleteConflict handling
	req := &ram20150501.DeleteRoleRequest{
		RoleName: tea.String(name),
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := rm.ramClient.DeleteRole(req)
		if err == nil {
			log.Printf("Deleted RAM role: %s", name)
			return nil
		}
		// Ignore "Resource not found" errors
		if strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			log.Printf("RAM role already deleted: %s", name)
			return nil
		}
		lastErr = err

		// Handle DeleteConflict: re-list, re-detach, wait, then retry
		if strings.Contains(err.Error(), "DeleteConflict") && attempt < maxRetries {
			log.Printf("DeleteConflict on role %s (attempt %d/%d), re-detaching policies...", name, attempt, maxRetries)
			listResp, listErr := rm.ramClient.ListPoliciesForRole(&ram20150501.ListPoliciesForRoleRequest{
				RoleName: tea.String(name),
			})
			if listErr == nil && listResp.Body != nil && listResp.Body.Policies != nil {
				for _, policy := range listResp.Body.Policies.Policy {
					if detachErr := rm.DetachPolicyFromRole(tea.StringValue(policy.PolicyName), name); detachErr != nil {
						log.Printf("Warning: re-detach policy %s from role %s failed: %v", tea.StringValue(policy.PolicyName), name, detachErr)
					}
				}
			}
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		if attempt < maxRetries {
			log.Printf("Warning: failed to delete RAM role %s (attempt %d/%d): %v, retrying...", name, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}

	return fmt.Errorf("failed to delete RAM role %s after %d attempts: %w", name, maxRetries, lastErr)
}

// AttachPolicyToRole attaches a RAM Policy to a RAM Role (with retry for DNS timeouts)
func (rm *ResourceManager) AttachPolicyToRole(policyName, roleName string) error {
	req := &ram20150501.AttachPolicyToRoleRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(policyName),
		RoleName:   tea.String(roleName),
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.AttachPolicyToRole(req)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to attach policy %s to role %s (attempt %d/3): %v, retrying...", policyName, roleName, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	return fmt.Errorf("failed to attach policy %s to role %s after 3 attempts: %w", policyName, roleName, lastErr)
}

// VerifyRolePolicyAttachment verifies that a policy is attached to a role, re-attaching if missing.
// This guards against ack-ram-tool or other tooling stripping previously attached policies.
func (rm *ResourceManager) VerifyRolePolicyAttachment(roleName, policyName string) error {
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := rm.ramClient.ListPoliciesForRole(&ram20150501.ListPoliciesForRoleRequest{
			RoleName: tea.String(roleName),
		})
		if err != nil {
			if attempt < 3 {
				log.Printf("Warning: failed to list policies for role %s (attempt %d/3): %v, retrying...", roleName, attempt, err)
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("failed to list policies for role %s after 3 attempts: %w", roleName, err)
		}

		if resp.Body != nil && resp.Body.Policies != nil {
			for _, policy := range resp.Body.Policies.Policy {
				if tea.StringValue(policy.PolicyName) == policyName {
					log.Printf("Verified policy %s attached to role %s", policyName, roleName)
					return nil
				}
			}
		}

		log.Printf("Warning: policy %s not found on role %s, re-attaching (attempt %d/3)...", policyName, roleName, attempt)
		if err := rm.AttachPolicyToRole(policyName, roleName); err != nil {
			if attempt < 3 {
				log.Printf("Warning: re-attach failed: %v, retrying...", err)
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("failed to re-attach policy %s to role %s after 3 attempts: %w", policyName, roleName, err)
		}
		log.Printf("Re-attached policy %s to role %s", policyName, roleName)
	}

	return fmt.Errorf("policy %s could not be verified on role %s after 3 attempts", policyName, roleName)
}

// DetachPolicyFromRole detaches a RAM Policy from a RAM Role (with retry)
func (rm *ResourceManager) DetachPolicyFromRole(policyName, roleName string) error {
	req := &ram20150501.DetachPolicyFromRoleRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(policyName),
		RoleName:   tea.String(roleName),
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.DetachPolicyFromRole(req)
		if err == nil {
			return nil
		}
		// Treat "not found" as success (idempotent delete)
		if strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to detach policy %s from role %s (attempt %d/3): %v, retrying...", policyName, roleName, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	return fmt.Errorf("failed to detach policy %s from role %s after 3 attempts: %w", policyName, roleName, lastErr)
}

// AttachPolicyToUser attaches a RAM Policy to a RAM User (with retry for DNS timeouts)
func (rm *ResourceManager) AttachPolicyToUser(policyName, userName string) error {
	req := &ram20150501.AttachPolicyToUserRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(policyName),
		UserName:   tea.String(userName),
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.AttachPolicyToUser(req)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to attach policy %s to user %s (attempt %d/3): %v, retrying...", policyName, userName, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	return fmt.Errorf("failed to attach policy %s to user %s after 3 attempts: %w", policyName, userName, lastErr)
}

// DetachPolicyFromUser detaches a RAM Policy from a RAM User (with retry)
func (rm *ResourceManager) DetachPolicyFromUser(policyName, userName string) error {
	req := &ram20150501.DetachPolicyFromUserRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(policyName),
		UserName:   tea.String(userName),
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.DetachPolicyFromUser(req)
		if err == nil {
			return nil
		}
		// Treat "not found" as success (idempotent delete)
		if strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			return nil
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to detach policy %s from user %s (attempt %d/3): %v, retrying...", policyName, userName, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	return fmt.Errorf("failed to detach policy %s from user %s after 3 attempts: %w", policyName, userName, lastErr)
}

// CreateRAMUserWithKMSPolicy creates a RAM user and attaches the unified KMS policy.
// Returns accessKeyId, accessKeySecret, error.
func (rm *ResourceManager) CreateRAMUserWithKMSPolicy(username, kmsPolicyName string) (string, string, error) {
	// 1. Create RAM user with retry
	log.Printf("Creating RAM user: %s", username)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.CreateUser(&ram20150501.CreateUserRequest{
			UserName: tea.String(username),
		})
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM user %s already exists, reusing", username)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM user %s (attempt %d/3): %v, retrying...", username, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("failed to create RAM user %s after 3 attempts: %w", username, lastErr)
	}

	// 2. Attach KMS policy (already has retry via AttachPolicyToUser)
	log.Printf("Attaching KMS policy %s to user %s", kmsPolicyName, username)
	if err := rm.AttachPolicyToUser(kmsPolicyName, username); err != nil {
		return "", "", fmt.Errorf("failed to attach KMS policy to user %s: %w", username, err)
	}

	// 3. Create access key with retry
	log.Printf("Creating access key for user %s", username)
	var akResp *ram20150501.CreateAccessKeyResponse
	for attempt := 1; attempt <= 3; attempt++ {
		akResp, lastErr = rm.ramClient.CreateAccessKey(&ram20150501.CreateAccessKeyRequest{
			UserName: tea.String(username),
		})
		if lastErr == nil {
			break
		}
		if attempt < 3 {
			log.Printf("Warning: failed to create access key for user %s (attempt %d/3): %v, retrying...", username, attempt, lastErr)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("failed to create access key for user %s after 3 attempts: %w", username, lastErr)
	}
	if akResp.Body == nil || akResp.Body.AccessKey == nil {
		return "", "", fmt.Errorf("empty access key response for user %s", username)
	}

	accessKeyId := tea.StringValue(akResp.Body.AccessKey.AccessKeyId)
	accessKeySecret := tea.StringValue(akResp.Body.AccessKey.AccessKeySecret)
	log.Printf("Created RAM user %s with access key %s", username, accessKeyId)

	// Track user for cleanup (must be after AK creation)
	rm.mu.Lock()
	rm.createdUsers = append(rm.createdUsers, RAMUser{
		Username:         username,
		AccessKeyID:      accessKeyId,
		AssociatedPolicy: kmsPolicyName,
	})
	rm.mu.Unlock()

	return accessKeyId, accessKeySecret, nil
}

// DeleteRAMUserWithKMSPolicy cleans up a RAM user created with CreateRAMUserWithKMSPolicy.
// Implements retry logic for IAM eventual consistency, including DeleteConflict handling.
func (rm *ResourceManager) DeleteRAMUserWithKMSPolicy(username, accessKeyId, kmsPolicyName string) {
	maxRetries := 3

	// 1. Delete known AccessKey with retry
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{
			UserAccessKeyId: tea.String(accessKeyId),
			UserName:        tea.String(username),
		})
		if err == nil || strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			break
		}
		if attempt < maxRetries {
			log.Printf("Warning: failed to delete access key %s for user %s (attempt %d/%d): %v, retrying...", accessKeyId, username, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
	}

	// 1b. List and delete any remaining AccessKeys (user may have multiple)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		listAKResp, listErr := rm.ramClient.ListAccessKeys(&ram20150501.ListAccessKeysRequest{
			UserName: tea.String(username),
		})
		if listErr != nil {
			if strings.Contains(listErr.Error(), "EntityNotExist") || strings.Contains(listErr.Error(), "Resource not found") {
				log.Printf("RAM user %s already deleted, skipping cleanup", username)
				return
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to list access keys for user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, listErr)
				time.Sleep(time.Duration(attempt*3) * time.Second)
				continue
			}
			break
		}
		if listAKResp.Body == nil || listAKResp.Body.AccessKeys == nil || len(listAKResp.Body.AccessKeys.AccessKey) == 0 {
			break
		}
		for _, ak := range listAKResp.Body.AccessKeys.AccessKey {
			akID := tea.StringValue(ak.AccessKeyId)
			log.Printf("Deleting remaining access key %s for user %s", akID, username)
			_, delErr := rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{
				UserAccessKeyId: tea.String(akID),
				UserName:        tea.String(username),
			})
			if delErr != nil && !strings.Contains(delErr.Error(), "Resource not found") && !strings.Contains(delErr.Error(), "EntityNotExist") {
				log.Printf("Warning: failed to delete access key %s for user %s: %v", akID, username, delErr)
			}
		}
		break
	}

	// 2. Retry detach ALL policies with verification (not just kmsPolicyName)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := rm.ramClient.ListPoliciesForUser(&ram20150501.ListPoliciesForUserRequest{
			UserName: tea.String(username),
		})
		if err != nil {
			if strings.Contains(err.Error(), "EntityNotExist") || strings.Contains(err.Error(), "Resource not found") {
				log.Printf("RAM user %s already deleted, skipping policy detach", username)
				return
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to list policies for user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
				continue
			}
			log.Printf("Warning: failed to list policies for user %s after %d attempts: %v, proceeding with delete", username, maxRetries, err)
			break
		}

		policyCount := 0
		if resp.Body != nil && resp.Body.Policies != nil {
			policyCount = len(resp.Body.Policies.Policy)
		}

		if policyCount == 0 {
			break
		}

		// Detach ALL policies (not just the tracked one)
		for _, policy := range resp.Body.Policies.Policy {
			if detachErr := rm.DetachPolicyFromUser(tea.StringValue(policy.PolicyName), username); detachErr != nil {
				log.Printf("Warning: failed to detach policy %s from user %s: %v", tea.StringValue(policy.PolicyName), username, detachErr)
			}
		}
		log.Printf("Detached %d policies from user %s, waiting 5s for propagation...", policyCount, username)
		time.Sleep(5 * time.Second)
	}

	// 3. Retry delete User with DeleteConflict handling
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := rm.ramClient.DeleteUser(&ram20150501.DeleteUserRequest{
			UserName: tea.String(username),
		})
		if err == nil {
			log.Printf("Deleted RAM user: %s", username)
			return
		}
		if strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			log.Printf("RAM user already deleted: %s", username)
			return
		}

		// Handle DeleteConflict: re-list, re-detach, delete remaining AKs, wait, retry
		if strings.Contains(err.Error(), "DeleteConflict") && attempt < maxRetries {
			log.Printf("DeleteConflict on user %s (attempt %d/%d), re-cleaning...", username, attempt, maxRetries)
			// Re-list and delete access keys
			if akListResp, akErr := rm.ramClient.ListAccessKeys(&ram20150501.ListAccessKeysRequest{UserName: tea.String(username)}); akErr == nil && akListResp.Body != nil && akListResp.Body.AccessKeys != nil {
				for _, ak := range akListResp.Body.AccessKeys.AccessKey {
					akID := tea.StringValue(ak.AccessKeyId)
					_, _ = rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{UserAccessKeyId: tea.String(akID), UserName: tea.String(username)})
				}
			}
			// Re-list and detach policies
			if polListResp, polErr := rm.ramClient.ListPoliciesForUser(&ram20150501.ListPoliciesForUserRequest{UserName: tea.String(username)}); polErr == nil && polListResp.Body != nil && polListResp.Body.Policies != nil {
				for _, policy := range polListResp.Body.Policies.Policy {
					_ = rm.DetachPolicyFromUser(tea.StringValue(policy.PolicyName), username)
				}
			}
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		if attempt < maxRetries {
			log.Printf("Warning: failed to delete user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	log.Printf("Warning: failed to delete RAM user %s after %d retries", username, maxRetries)
}

// GetECSWorkerRoleName retrieves the ECS worker RAM role name for the cluster (with retry for DNS timeouts)
func (rm *ResourceManager) GetECSWorkerRoleName() (string, error) {
	const maxRetries = 3
	const retryInterval = 5 * time.Second

	var output []byte
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cmd := exec.Command("aliyun", "cs", "GET", "/clusters/"+rm.config.ClusterID,
			"--region", rm.config.RegionID,
			"--header", "Content-Type=application/json")
		output, err = cmd.CombinedOutput()
		if err == nil {
			break
		}
		if attempt < maxRetries {
			log.Printf("Warning: failed to get cluster info (attempt %d/%d): %v, retrying in %v...", attempt, maxRetries, err, retryInterval)
			time.Sleep(retryInterval)
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to get cluster info after %d attempts: %w, output: %s", maxRetries, err, string(output))
	}

	var clusterInfo struct {
		WorkerRAMRoleName string `json:"worker_ram_role_name"`
	}
	if err := json.Unmarshal(output, &clusterInfo); err != nil {
		return "", fmt.Errorf("failed to parse cluster info: %w, output: %s", err, string(output))
	}
	if clusterInfo.WorkerRAMRoleName == "" {
		return "", fmt.Errorf("cluster has no worker RAM role configured")
	}
	return clusterInfo.WorkerRAMRoleName, nil
}

// CreateRAMUserWithAccessKey creates a RAM user, attaches a policy, and generates an AK/SK pair.
// username: RAM user name
// policyName: policy name (e.g. sts:AssumeRole permission)
// policyDocument: policy JSON document
// Returns: accessKeyId, accessKeySecret, error
func (rm *ResourceManager) CreateRAMUserWithAccessKey(username, policyName, policyDocument string) (string, string, error) {
	// 1. Create RAM user with retry
	log.Printf("Creating RAM user: %s", username)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.CreateUser(&ram20150501.CreateUserRequest{
			UserName: tea.String(username),
		})
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM user %s already exists, reusing", username)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM user %s (attempt %d/3): %v, retrying...", username, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("failed to create RAM user %s after 3 attempts: %w", username, lastErr)
	}

	// 2. Create policy with retry
	log.Printf("Creating RAM policy: %s", policyName)
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := rm.ramClient.CreatePolicy(&ram20150501.CreatePolicyRequest{
			PolicyName:     tea.String(policyName),
			PolicyDocument: tea.String(policyDocument),
			Description:    tea.String("CSI Provider E2E Test - Provider Auth Policy"),
		})
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM policy %s already exists, reusing", policyName)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM policy %s (attempt %d/3): %v, retrying...", policyName, attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("failed to create RAM policy %s after 3 attempts: %w", policyName, lastErr)
	}

	// 3. Attach policy to user (already has retry via AttachPolicyToUser)
	log.Printf("Attaching policy %s to user %s", policyName, username)
	if err := rm.AttachPolicyToUser(policyName, username); err != nil {
		return "", "", fmt.Errorf("failed to attach policy %s to user %s: %w", policyName, username, err)
	}

	// 4. Create access key with retry
	log.Printf("Creating access key for user %s", username)
	var akResp *ram20150501.CreateAccessKeyResponse
	for attempt := 1; attempt <= 3; attempt++ {
		akResp, lastErr = rm.ramClient.CreateAccessKey(&ram20150501.CreateAccessKeyRequest{
			UserName: tea.String(username),
		})
		if lastErr == nil {
			break
		}
		if attempt < 3 {
			log.Printf("Warning: failed to create access key for user %s (attempt %d/3): %v, retrying...", username, attempt, lastErr)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("failed to create access key for user %s after 3 attempts: %w", username, lastErr)
	}
	if akResp.Body == nil || akResp.Body.AccessKey == nil {
		return "", "", fmt.Errorf("empty access key response for user %s", username)
	}

	accessKeyId := tea.StringValue(akResp.Body.AccessKey.AccessKeyId)
	accessKeySecret := tea.StringValue(akResp.Body.AccessKey.AccessKeySecret)
	log.Printf("Created RAM user %s with access key %s", username, accessKeyId)

	rm.mu.Lock()
	rm.createdUsers = append(rm.createdUsers, RAMUser{
		Username:         username,
		AccessKeyID:      accessKeyId,
		AssociatedPolicy: policyName,
	})
	rm.mu.Unlock()

	return accessKeyId, accessKeySecret, nil
}

// DeleteRAMUserWithAccessKey cleans up a RAM user and its associated resources (reverse order).
// Implements retry logic for IAM eventual consistency, including DeleteConflict handling.
func (rm *ResourceManager) DeleteRAMUserWithAccessKey(username, accessKeyId, policyName string) {
	maxRetries := 3

	// 1. Delete known access key with retry
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{
			UserAccessKeyId: tea.String(accessKeyId),
			UserName:        tea.String(username),
		})
		if err == nil || strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			break
		}
		if attempt < maxRetries {
			log.Printf("Warning: failed to delete access key %s for user %s (attempt %d/%d): %v, retrying...", accessKeyId, username, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
	}

	// 1b. List and delete any remaining AccessKeys
	for attempt := 1; attempt <= maxRetries; attempt++ {
		listAKResp, listErr := rm.ramClient.ListAccessKeys(&ram20150501.ListAccessKeysRequest{
			UserName: tea.String(username),
		})
		if listErr != nil {
			if strings.Contains(listErr.Error(), "EntityNotExist") || strings.Contains(listErr.Error(), "Resource not found") {
				log.Printf("RAM user %s already deleted, skipping cleanup", username)
				return
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to list access keys for user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, listErr)
				time.Sleep(time.Duration(attempt*3) * time.Second)
				continue
			}
			break
		}
		if listAKResp.Body == nil || listAKResp.Body.AccessKeys == nil || len(listAKResp.Body.AccessKeys.AccessKey) == 0 {
			break
		}
		for _, ak := range listAKResp.Body.AccessKeys.AccessKey {
			akID := tea.StringValue(ak.AccessKeyId)
			log.Printf("Deleting remaining access key %s for user %s", akID, username)
			_, delErr := rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{
				UserAccessKeyId: tea.String(akID),
				UserName:        tea.String(username),
			})
			if delErr != nil && !strings.Contains(delErr.Error(), "Resource not found") && !strings.Contains(delErr.Error(), "EntityNotExist") {
				log.Printf("Warning: failed to delete access key %s for user %s: %v", akID, username, delErr)
			}
		}
		break
	}

	// 2. Detach ALL policies from user (not just the tracked one)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := rm.ramClient.ListPoliciesForUser(&ram20150501.ListPoliciesForUserRequest{
			UserName: tea.String(username),
		})
		if err != nil {
			if strings.Contains(err.Error(), "EntityNotExist") || strings.Contains(err.Error(), "Resource not found") {
				log.Printf("RAM user %s already deleted, skipping policy detach", username)
				return
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to list policies for user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
				continue
			}
			break
		}
		if resp.Body == nil || resp.Body.Policies == nil || len(resp.Body.Policies.Policy) == 0 {
			break
		}
		for _, policy := range resp.Body.Policies.Policy {
			if detachErr := rm.DetachPolicyFromUser(tea.StringValue(policy.PolicyName), username); detachErr != nil {
				log.Printf("Warning: failed to detach policy %s from user %s: %v", tea.StringValue(policy.PolicyName), username, detachErr)
			}
		}
		log.Printf("Detached %d policies from user %s, waiting 5s for propagation...", len(resp.Body.Policies.Policy), username)
		time.Sleep(5 * time.Second)
		break
	}

	// 3. Delete the associated policy with retry
	if policyName != "" {
		for attempt := 1; attempt <= maxRetries; attempt++ {
			_, err := rm.ramClient.DeletePolicy(&ram20150501.DeletePolicyRequest{
				PolicyName: tea.String(policyName),
			})
			if err == nil || strings.Contains(err.Error(), "Resource not found") {
				break
			}
			// Handle DeleteConflict: policy still attached to entities, re-detach and retry
			if strings.Contains(err.Error(), "DeleteConflict") && attempt < maxRetries {
				log.Printf("DeleteConflict on policy %s (attempt %d/%d), re-detaching from entities...", policyName, attempt, maxRetries)
				if entResp, entErr := rm.ramClient.ListEntitiesForPolicy(&ram20150501.ListEntitiesForPolicyRequest{
					PolicyType: tea.String("Custom"), PolicyName: tea.String(policyName),
				}); entErr == nil && entResp.Body != nil {
					if entResp.Body.Users != nil && entResp.Body.Users.User != nil {
						for _, u := range entResp.Body.Users.User {
							_ = rm.DetachPolicyFromUser(policyName, tea.StringValue(u.UserName))
						}
					}
					if entResp.Body.Roles != nil && entResp.Body.Roles.Role != nil {
						for _, r := range entResp.Body.Roles.Role {
							_ = rm.DetachPolicyFromRole(policyName, tea.StringValue(r.RoleName))
						}
					}
				}
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			if attempt < maxRetries {
				log.Printf("Warning: failed to delete policy %s (attempt %d/%d): %v, retrying...", policyName, attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
			}
		}
	}

	// 4. Retry delete user with DeleteConflict handling
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := rm.ramClient.DeleteUser(&ram20150501.DeleteUserRequest{
			UserName: tea.String(username),
		})
		if err == nil {
			log.Printf("Deleted RAM user: %s", username)
			return
		}
		if strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
			log.Printf("RAM user already deleted: %s", username)
			return
		}

		// Handle DeleteConflict: re-list, re-detach, delete remaining AKs, wait, retry
		if strings.Contains(err.Error(), "DeleteConflict") && attempt < maxRetries {
			log.Printf("DeleteConflict on user %s (attempt %d/%d), re-cleaning...", username, attempt, maxRetries)
			if akListResp, akErr := rm.ramClient.ListAccessKeys(&ram20150501.ListAccessKeysRequest{UserName: tea.String(username)}); akErr == nil && akListResp.Body != nil && akListResp.Body.AccessKeys != nil {
				for _, ak := range akListResp.Body.AccessKeys.AccessKey {
					akID := tea.StringValue(ak.AccessKeyId)
					_, _ = rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{UserAccessKeyId: tea.String(akID), UserName: tea.String(username)})
				}
			}
			if polListResp, polErr := rm.ramClient.ListPoliciesForUser(&ram20150501.ListPoliciesForUserRequest{UserName: tea.String(username)}); polErr == nil && polListResp.Body != nil && polListResp.Body.Policies != nil {
				for _, policy := range polListResp.Body.Policies.Policy {
					_ = rm.DetachPolicyFromUser(tea.StringValue(policy.PolicyName), username)
				}
			}
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		if attempt < maxRetries {
			log.Printf("Warning: failed to delete user %s (attempt %d/%d): %v, retrying...", username, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	log.Printf("Warning: failed to delete RAM user %s after %d retries", username, maxRetries)
}

// GetConfig returns the test configuration
func (rm *ResourceManager) GetConfig() *TestConfig {
	return rm.config
}

// Cleanup cleans up all created resources
func (rm *ResourceManager) Cleanup() {
	log.Println("Cleaning up cloud resources...")

	if !rm.config.AutoCleanupKMS {
		log.Println("AutoCleanupKMS disabled, skipping KMS cleanup")
	} else {
		rm.mu.Lock()
		secrets := make([]string, len(rm.createdSecrets))
		copy(secrets, rm.createdSecrets)
		rm.mu.Unlock()

		for _, name := range secrets {
			if err := rm.DeleteKMSSecret(name); err != nil {
				log.Printf("Warning: failed to delete KMS secret %s: %v", name, err)
			}
		}
	}

	if !rm.config.AutoCleanupRAM {
		log.Println("AutoCleanupRAM disabled, skipping RAM cleanup")
		return
	}

	rm.mu.Lock()
	policies := make([]string, len(rm.createdPolicies))
	copy(policies, rm.createdPolicies)
	roles := make([]string, len(rm.createdRoles))
	copy(roles, rm.createdRoles)
	rm.mu.Unlock()

	// Detach policies from roles first
	detached := false
	for _, role := range roles {
		for _, policy := range policies {
			if err := rm.DetachPolicyFromRole(policy, role); err == nil {
				detached = true
			}
		}
	}
	if detached {
		log.Println("Detached policies from roles, waiting 5s for propagation...")
		time.Sleep(5 * time.Second)
	}

	// Delete roles
	for _, role := range roles {
		if err := rm.DeleteRAMRole(role); err != nil {
			log.Printf("Warning: failed to delete RAM role %s: %v", role, err)
		}
	}

	// Delete policies
	for _, policy := range policies {
		if err := rm.DeleteRAMPolicy(policy); err != nil {
			log.Printf("Warning: failed to delete RAM policy %s: %v", policy, err)
		}
	}

	// Clean up RAM users with full cleanup (AK + policies + user)
	rm.mu.Lock()
	users := make([]RAMUser, len(rm.createdUsers))
	copy(users, rm.createdUsers)
	rm.mu.Unlock()
	for _, user := range users {
		log.Printf("Cleaning up RAM user: %s", user.Username)
		if user.AssociatedPolicy != "" && user.AccessKeyID != "" {
			// Full cleanup: user created with policy and AK
			rm.DeleteRAMUserWithKMSPolicy(user.Username, user.AccessKeyID, user.AssociatedPolicy)
		} else if user.AccessKeyID != "" {
			// User has AK but no policy tracked - use full cleanup with AK listing
			rm.DeleteRAMUserWithAccessKey(user.Username, user.AccessKeyID, "")
		} else {
			// Last resort: list and clean up AKs + policies before deleting user
			log.Printf("Warning: no tracked AK/policy for user %s, attempting best-effort cleanup", user.Username)
			// Delete any access keys
			if akListResp, akErr := rm.ramClient.ListAccessKeys(&ram20150501.ListAccessKeysRequest{UserName: tea.String(user.Username)}); akErr == nil && akListResp.Body != nil && akListResp.Body.AccessKeys != nil {
				for _, ak := range akListResp.Body.AccessKeys.AccessKey {
					akID := tea.StringValue(ak.AccessKeyId)
					_, _ = rm.ramClient.DeleteAccessKey(&ram20150501.DeleteAccessKeyRequest{UserAccessKeyId: tea.String(akID), UserName: tea.String(user.Username)})
				}
			}
			// Detach any policies
			if polListResp, polErr := rm.ramClient.ListPoliciesForUser(&ram20150501.ListPoliciesForUserRequest{UserName: tea.String(user.Username)}); polErr == nil && polListResp.Body != nil && polListResp.Body.Policies != nil {
				for _, policy := range polListResp.Body.Policies.Policy {
					_ = rm.DetachPolicyFromUser(tea.StringValue(policy.PolicyName), user.Username)
				}
				if len(polListResp.Body.Policies.Policy) > 0 {
					time.Sleep(5 * time.Second)
				}
			}
			_, err := rm.ramClient.DeleteUser(&ram20150501.DeleteUserRequest{
				UserName: tea.String(user.Username),
			})
			if err != nil && !strings.Contains(err.Error(), "Resource not found") && !strings.Contains(err.Error(), "EntityNotExist") {
				log.Printf("Warning: failed to delete RAM user %s: %v", user.Username, err)
			}
		}
	}

	// Verify cleanup completion
	log.Println("Verifying cleanup completion...")
	if rm.config.AutoCleanupRAM {
		var err error

		// Check for remaining test roles
		var rolesResp *ram20150501.ListRolesResponse
		for attempt := 1; attempt <= 2; attempt++ {
			rolesResp, err = rm.ramClient.ListRoles(&ram20150501.ListRolesRequest{})
			if err == nil {
				break
			}
			if attempt < 2 {
				log.Printf("Warning: failed to list roles (attempt %d/2): %v, retrying...", attempt, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
			}
		}
		if err == nil && rolesResp.Body != nil && len(rolesResp.Body.Roles.Role) > 0 {
			testRoleCount := 0
			for _, role := range rolesResp.Body.Roles.Role {
				roleName := tea.StringValue(role.RoleName)
				if strings.Contains(roleName, "tc-") || strings.Contains(roleName, TestResourcePrefix) {
					testRoleCount++
					log.Printf("  Remaining role: %s", roleName)
				}
			}
			if testRoleCount > 0 {
				log.Printf("Warning: %d test RAM Roles still exist", testRoleCount)
			}
		}

		// Check for remaining test users
		var usersResp *ram20150501.ListUsersResponse
		for attempt := 1; attempt <= 2; attempt++ {
			usersResp, err = rm.ramClient.ListUsers(&ram20150501.ListUsersRequest{})
			if err == nil {
				break
			}
			if attempt < 2 {
				log.Printf("Warning: failed to list users (attempt %d/2): %v, retrying...", attempt, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
			}
		}
		if err == nil && usersResp.Body != nil && usersResp.Body.Users != nil {
			testUserCount := 0
			for _, user := range usersResp.Body.Users.User {
				userName := tea.StringValue(user.UserName)
				if strings.Contains(userName, "tc-") || strings.Contains(userName, TestResourcePrefix) {
					testUserCount++
					log.Printf("  Remaining user: %s", userName)
				}
			}
			if testUserCount > 0 {
				log.Printf("Warning: %d test RAM Users still exist", testUserCount)
			}
		}

		// Check for remaining test policies
		var policiesResp *ram20150501.ListPoliciesResponse
		for attempt := 1; attempt <= 2; attempt++ {
			policiesResp, err = rm.ramClient.ListPolicies(&ram20150501.ListPoliciesRequest{
				PolicyType: tea.String("Custom"),
			})
			if err == nil {
				break
			}
			if attempt < 2 {
				log.Printf("Warning: failed to list policies (attempt %d/2): %v, retrying...", attempt, err)
				time.Sleep(time.Duration(attempt*3) * time.Second)
			}
		}
		if err == nil && policiesResp.Body != nil && policiesResp.Body.Policies != nil {
			testPolicyCount := 0
			for _, policy := range policiesResp.Body.Policies.Policy {
				policyName := tea.StringValue(policy.PolicyName)
				if strings.Contains(policyName, "csi-provider-test-") || strings.Contains(policyName, "tc-") {
					testPolicyCount++
					log.Printf("  Remaining policy: %s", policyName)
				}
			}
			if testPolicyCount > 0 {
				log.Printf("Warning: %d test RAM Policies still exist", testPolicyCount)
			}
		}
	}

	log.Println("Cloud resource cleanup completed")
}

// GetCreatedSecrets returns a copy of the created KMS secrets list
func (rm *ResourceManager) GetCreatedSecrets() []string {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	result := make([]string, len(rm.createdSecrets))
	copy(result, rm.createdSecrets)
	return result
}

// GetCreatedPolicies returns a copy of the created RAM policies list
func (rm *ResourceManager) GetCreatedPolicies() []string {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	result := make([]string, len(rm.createdPolicies))
	copy(result, rm.createdPolicies)
	return result
}

// GetCreatedRoles returns a copy of the created RAM roles list
func (rm *ResourceManager) GetCreatedRoles() []string {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	result := make([]string, len(rm.createdRoles))
	copy(result, rm.createdRoles)
	return result
}

// SetupTargetAccountResources creates resources in target account for cross-account testing
func (rm *ResourceManager) SetupTargetAccountResources(crossAccountSecretName string) error {
	if rm.config.TargetAccountID == "" {
		return fmt.Errorf("target account ID (TARGET_ACCOUNT_ID) environment variable not set")
	}
	if rm.config.TargetAccessKeyID == "" || rm.config.TargetAccessKeySecret == "" {
		return fmt.Errorf("target account credentials (TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET) not set")
	}

	log.Printf("Setting up target account resources (Account: %s)", rm.config.TargetAccountID)

	// Create target account clients
	targetCred, err := credential.NewCredential(&credential.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     tea.String(rm.config.TargetAccessKeyID),
		AccessKeySecret: tea.String(rm.config.TargetAccessKeySecret),
	})
	if err != nil {
		return fmt.Errorf("failed to create target account credential: %w", err)
	}

	targetKMSConfig := &openapi.Config{
		Credential: targetCred,
		Endpoint:   tea.String(fmt.Sprintf(kmsEndpointFormat, rm.config.RegionID)),
	}
	targetKMSClient, err := kms20160120.NewClient(targetKMSConfig)
	if err != nil {
		return fmt.Errorf("failed to create target KMS client: %w", err)
	}

	targetRAMConfig := &openapi.Config{
		Credential: targetCred,
		Endpoint:   tea.String(ramEndpoint),
	}
	targetRAMClient, err := ram20150501.NewClient(targetRAMConfig)
	if err != nil {
		return fmt.Errorf("failed to create target RAM client: %w", err)
	}

	// Step 1: Create KMS Secret in target account with retry
	log.Printf("Creating KMS Secret in target account: %s", crossAccountSecretName)
	req := &kms20160120.CreateSecretRequest{
		SecretName: tea.String(crossAccountSecretName),
		SecretData: tea.String("cross-account-value"),
		VersionId:  tea.String("v1"),
	}
	if rm.config.TargetEncryptionKeyID != "" {
		req.EncryptionKeyId = tea.String(rm.config.TargetEncryptionKeyID)
	}
	if rm.config.TargetDKMSInstanceID != "" {
		req.DKMSInstanceId = tea.String(rm.config.TargetDKMSInstanceID)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, err = targetKMSClient.CreateSecret(req)
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("KMS secret %s already exists in target account, reusing", crossAccountSecretName)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create KMS secret in target account (attempt %d/3): %v, retrying...", attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to create KMS secret in target account after 3 attempts: %w", lastErr)
	}

	rm.mu.Lock()
	rm.targetKMSSecrets = append(rm.targetKMSSecrets, crossAccountSecretName)
	rm.mu.Unlock()

	// Step 2: Create RAM Policy with retry
	targetPolicyName := fmt.Sprintf("cross-account-test-kms-policy-%d", time.Now().Unix())
	log.Printf("Creating RAM Policy in target account: %s", targetPolicyName)
	policyDoc := UnifiedKMSPolicyDoc
	policyReq := &ram20150501.CreatePolicyRequest{
		PolicyName:     tea.String(targetPolicyName),
		PolicyDocument: tea.String(policyDoc),
		Description:    tea.String("Cross-account test KMS policy"),
	}
	for attempt := 1; attempt <= 3; attempt++ {
		_, err = targetRAMClient.CreatePolicy(policyReq)
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM policy %s already exists in target account, reusing", targetPolicyName)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM policy in target account (attempt %d/3): %v, retrying...", attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to create RAM policy in target account after 3 attempts: %w", lastErr)
	}

	rm.mu.Lock()
	rm.targetRAMPolicies = append(rm.targetRAMPolicies, targetPolicyName)
	rm.mu.Unlock()

	// Step 3: Create RAM Role with trust policy
	targetRoleName := fmt.Sprintf("cross-account-test-role-%d", time.Now().Unix())
	log.Printf("Creating RAM Role in target account: %s", targetRoleName)
	trustPolicy := fmt.Sprintf(`{
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
}`, rm.config.SourceAccountID)

	roleReq := &ram20150501.CreateRoleRequest{
		RoleName:                 tea.String(targetRoleName),
		AssumeRolePolicyDocument: tea.String(trustPolicy),
		Description:              tea.String(fmt.Sprintf("Cross-account test role (trusts source account %s)", rm.config.SourceAccountID)),
	}
	for attempt := 1; attempt <= 3; attempt++ {
		_, err = targetRAMClient.CreateRole(roleReq)
		if err == nil {
			lastErr = nil
			break
		}
		if strings.Contains(err.Error(), "EntityAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			log.Printf("RAM role %s already exists in target account, reusing", targetRoleName)
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to create RAM role in target account (attempt %d/3): %v, retrying...", attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to create RAM role in target account after 3 attempts: %w", lastErr)
	}

	rm.mu.Lock()
	rm.targetRAMRoles = append(rm.targetRAMRoles, targetRoleName)
	rm.mu.Unlock()

	// Step 4: Attach policy to role with retry
	attachReq := &ram20150501.AttachPolicyToRoleRequest{
		PolicyType: tea.String("Custom"),
		PolicyName: tea.String(targetPolicyName),
		RoleName:   tea.String(targetRoleName),
	}
	for attempt := 1; attempt <= 3; attempt++ {
		_, err = targetRAMClient.AttachPolicyToRole(attachReq)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if attempt < 3 {
			log.Printf("Warning: failed to attach policy to role in target account (attempt %d/3): %v, retrying...", attempt, err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
	}
	if lastErr != nil {
		log.Printf("Warning: failed to attach policy to role in target account after 3 attempts: %v", lastErr)
	}

	// Store target role ARN
	rm.config.TargetRoleARN = fmt.Sprintf("acs:ram::%s:role/%s", rm.config.TargetAccountID, targetRoleName)
	log.Printf("Target Role ARN: %s", rm.config.TargetRoleARN)

	log.Println("Target account resources setup complete")
	return nil
}

// CleanupTargetAccountResources cleans up resources created in target account
func (rm *ResourceManager) CleanupTargetAccountResources() {
	rm.mu.Lock()
	secrets := make([]string, len(rm.targetKMSSecrets))
	copy(secrets, rm.targetKMSSecrets)
	policies := make([]string, len(rm.targetRAMPolicies))
	copy(policies, rm.targetRAMPolicies)
	roles := make([]string, len(rm.targetRAMRoles))
	copy(roles, rm.targetRAMRoles)
	rm.mu.Unlock()

	if len(secrets) == 0 && len(policies) == 0 && len(roles) == 0 {
		log.Println("No target account resources to clean up")
		return
	}

	if rm.config.TargetAccessKeyID == "" || rm.config.TargetAccessKeySecret == "" {
		log.Println("Warning: TARGET_ACCOUNT_ACCESS_KEY_ID/SECRET not set, cannot clean up target account resources")
		return
	}

	log.Println("Cleaning up target account resources...")

	// Create target account clients
	targetCred, err := credential.NewCredential(&credential.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     tea.String(rm.config.TargetAccessKeyID),
		AccessKeySecret: tea.String(rm.config.TargetAccessKeySecret),
	})
	if err != nil {
		log.Printf("Warning: failed to create target account credential for cleanup: %v", err)
		return
	}

	targetKMSConfig := &openapi.Config{
		Credential: targetCred,
		Endpoint:   tea.String(fmt.Sprintf(kmsEndpointFormat, rm.config.RegionID)),
	}
	targetKMSClient, err := kms20160120.NewClient(targetKMSConfig)
	if err != nil {
		log.Printf("Warning: failed to create target KMS client for cleanup: %v", err)
		return
	}

	targetRAMConfig := &openapi.Config{
		Credential: targetCred,
		Endpoint:   tea.String(ramEndpoint),
	}
	targetRAMClient, err := ram20150501.NewClient(targetRAMConfig)
	if err != nil {
		log.Printf("Warning: failed to create target RAM client for cleanup: %v", err)
		return
	}

	// Clean up KMS Secrets with retry
	for _, secret := range secrets {
		log.Printf("Deleting KMS Secret in target account: %s", secret)
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			_, err := targetKMSClient.DeleteSecret(&kms20160120.DeleteSecretRequest{
				SecretName:                 tea.String(secret),
				ForceDeleteWithoutRecovery: tea.String("true"),
			})
			if err == nil || strings.Contains(err.Error(), "Resource not found") {
				break
			}
			lastErr = err
			if attempt < 3 {
				log.Printf("Warning: failed to delete KMS secret %s in target account (attempt %d/3): %v, retrying...", secret, attempt, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
			}
		}
		if lastErr != nil {
			log.Printf("Warning: failed to delete KMS secret %s in target account after 3 attempts: %v", secret, lastErr)
		}
	}

	// Detach policies from roles
	for _, role := range roles {
		for _, policy := range policies {
			log.Printf("Detaching policy from role in target account: %s from %s", policy, role)
			_, _ = targetRAMClient.DetachPolicyFromRole(&ram20150501.DetachPolicyFromRoleRequest{
				PolicyType: tea.String("Custom"),
				PolicyName: tea.String(policy),
				RoleName:   tea.String(role),
			})
		}
	}

	// Wait for detach to take effect
	time.Sleep(5 * time.Second)

	// Delete roles with retry and DeleteConflict handling
	for _, role := range roles {
		log.Printf("Deleting RAM Role in target account: %s", role)
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			_, err := targetRAMClient.DeleteRole(&ram20150501.DeleteRoleRequest{
				RoleName: tea.String(role),
			})
			if err == nil || strings.Contains(err.Error(), "Resource not found") || strings.Contains(err.Error(), "EntityNotExist") {
				lastErr = nil
				break
			}
			lastErr = err
			// Handle DeleteConflict: re-list and re-detach policies
			if strings.Contains(err.Error(), "DeleteConflict") && attempt < 3 {
				log.Printf("DeleteConflict on target role %s (attempt %d/3), re-detaching policies...", role, attempt)
				if listResp, listErr := targetRAMClient.ListPoliciesForRole(&ram20150501.ListPoliciesForRoleRequest{
					RoleName: tea.String(role),
				}); listErr == nil && listResp.Body != nil && listResp.Body.Policies != nil {
					for _, policy := range listResp.Body.Policies.Policy {
						_, _ = targetRAMClient.DetachPolicyFromRole(&ram20150501.DetachPolicyFromRoleRequest{
							PolicyType: tea.String("Custom"),
							PolicyName: tea.String(tea.StringValue(policy.PolicyName)),
							RoleName:   tea.String(role),
						})
					}
				}
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			if attempt < 3 {
				log.Printf("Warning: failed to delete RAM role %s in target account (attempt %d/3): %v, retrying...", role, attempt, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
			}
		}
		if lastErr != nil {
			log.Printf("Warning: failed to delete RAM role %s in target account after 3 attempts: %v", role, lastErr)
		}
	}

	// Wait for role deletion to take effect
	time.Sleep(5 * time.Second)

	// Delete policies with retry and DeleteConflict handling
	for _, policy := range policies {
		log.Printf("Deleting RAM Policy in target account: %s", policy)
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			_, err := targetRAMClient.DeletePolicy(&ram20150501.DeletePolicyRequest{
				PolicyName: tea.String(policy),
			})
			if err == nil || strings.Contains(err.Error(), "Resource not found") {
				lastErr = nil
				break
			}
			lastErr = err
			// Handle DeleteConflict: re-detach from entities
			if strings.Contains(err.Error(), "DeleteConflict") && attempt < 3 {
				log.Printf("DeleteConflict on target policy %s (attempt %d/3), re-detaching...", policy, attempt)
				if entResp, entErr := targetRAMClient.ListEntitiesForPolicy(&ram20150501.ListEntitiesForPolicyRequest{
					PolicyType: tea.String("Custom"), PolicyName: tea.String(policy),
				}); entErr == nil && entResp.Body != nil {
					if entResp.Body.Roles != nil && entResp.Body.Roles.Role != nil {
						for _, r := range entResp.Body.Roles.Role {
							_, _ = targetRAMClient.DetachPolicyFromRole(&ram20150501.DetachPolicyFromRoleRequest{
								PolicyType: tea.String("Custom"),
								PolicyName: tea.String(policy),
								RoleName:   tea.String(tea.StringValue(r.RoleName)),
							})
						}
					}
				}
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			if attempt < 3 {
				log.Printf("Warning: failed to delete RAM policy %s in target account (attempt %d/3): %v, retrying...", policy, attempt, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
			}
		}
		if lastErr != nil {
			log.Printf("Warning: failed to delete RAM policy %s in target account after 3 attempts: %v", policy, lastErr)
		}
	}

	log.Println("Target account resources cleanup complete")
}
