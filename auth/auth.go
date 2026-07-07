package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/aliyun/credentials-go/credentials"
	"k8s.io/klog/v2"
)

const (
	ProviderName       = "secrets-store-csi-driver-provider-alibabacloud"
	RamRoleARNAuthType = "ram_role_arn"
	AKAuthType         = "access_key"
	EcsRamRoleAuthType = "ecs_ram_role"
	OidcAuthType       = "oidc_role_arn"
	roleSessionName    = "csi-secrets-store-provider-alibaba"
	oidcTokenFilePath  = "/var/run/secrets/tokens/csi-secrets-store-provider-alibabacloud"

	// Environment variable names for automatic OIDC Provider ARN construction
	envAccountID = "ALICLOUD_ACCOUNT_ID"
	envClusterID = "ALICLOUD_CLUSTER_ID"
)

// PodSAInfo contains Pod ServiceAccount information
type PodSAInfo struct {
	Namespace              string // Pod namespace
	ServiceAccount         string // Pod service account name
	RoleArn                string // RRSA Role ARN for Pod SA (Source: SA annotation ack.alibabacloud.com/role-arn)
	UsePodServiceAccount   bool   // Whether to use Pod SA authentication
	TokenFromVolumeContext string // Service account token from VolumeContext (fallback)
}

type getCredential interface {
	NewCredential() (credentials.Credential, error)
}

type chainedCred interface {
	getCredential
	authNext(chainedCred) chainedCred
}

type chainedAuth struct {
	cred getCredential
	name string // auth method name for diagnostics
	next chainedCred
}

func (ch *chainedAuth) authNext(next chainedCred) chainedCred {
	ch.next = next
	return next
}

func (ch *chainedAuth) NewCredential() (credentials.Credential, error) {
	cred, err := ch.cred.NewCredential()
	if err != nil {
		return nil, err
	}
	if cred != nil {
		return cred, nil
	}
	klog.V(2).Infof("%s: skipped, trying next auth method", ch.name)
	if ch.next != nil {
		return ch.next.NewCredential()
	}
	return nil, errors.New("empty credential")
}

type oidcRoleAuth struct{ *authConfig }

func (c *oidcRoleAuth) NewCredential() (credentials.Credential, error) {
	//prefer to use rrsa oidc auth type
	if c.oidcArn == "" || c.roleArn == "" {
		return nil, nil
	}

	sessionName := roleSessionName
	if c.roleSessionName != "" {
		sessionName = c.roleSessionName
	}
	config := new(credentials.Config).
		SetType(OidcAuthType).
		SetOIDCProviderArn(c.oidcArn).
		SetOIDCTokenFilePath(oidcTokenFilePath).
		SetRoleArn(c.roleArn).
		SetRoleSessionName(sessionName)

	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.Warning("OIDC auth failed, deferring to next auth method",
			"roleArn", c.roleArn, "oidcArn", c.oidcArn, "err", err)
		return nil, err
	}
	if cred != nil {
		klog.Info("Using oidc rrsa auth", "roleArn", c.roleArn, "oidcArn", c.oidcArn)
	}
	return cred, nil
}

type podServiceAccountAuth struct{ *authConfig }

func (c *podServiceAccountAuth) NewCredential() (credentials.Credential, error) {
	if !c.usePodServiceAccountToken || c.podServiceAccountRoleArn == "" || c.podNamespace == "" || c.podServiceAccount == "" {
		return nil, nil
	}

	const serviceAccountTokenKey = "csi.storage.k8s.io/serviceAccount.tokens"
	var tokensJSON string

	// Try secrets field first (primary path for CSI Driver v1.5.0+)
	var secrets map[string]string
	if err := json.Unmarshal([]byte(c.nodePublishSecret), &secrets); err == nil {
		if token, exists := secrets[serviceAccountTokenKey]; exists && token != "" {
			tokensJSON = token
		}
	}

	// Fall back to VolumeContext (legacy path for CSI Driver < v1.5.0)
	if tokensJSON == "" && c.tokenFromVolumeContext != "" {
		tokensJSON = c.tokenFromVolumeContext
	}

	if tokensJSON == "" {
		klog.Warning("Pod SA token not found, skipping Pod SA authentication",
			"namespace", c.podNamespace, "serviceAccount", c.podServiceAccount)
		return nil, nil
	}

	var tokens map[string]struct {
		Token               string `json:"token"`
		ExpirationTimestamp string `json:"expirationTimestamp"`
	}
	if err := json.Unmarshal([]byte(tokensJSON), &tokens); err != nil {
		return nil, fmt.Errorf("failed to unmarshal service account tokens: %v", err)
	}

	// Find the token for sts.aliyuncs.com audience (RRSA)
	tokenData, exists := tokens["sts.aliyuncs.com"]
	if !exists || tokenData.Token == "" {
		return nil, fmt.Errorf(
			"pod SA token not found for audience 'sts.aliyuncs.com' (namespace=%s, serviceAccount=%s); skipping Pod SA authentication, falling back to next auth method", c.podNamespace, c.podServiceAccount)
	}

	// Write token to temporary file for credentials SDK.
	// The SDK requires a file path, not the token string directly.
	tmpFile, err := os.CreateTemp("", "pod-sa-token-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary token file: %v", err)
	}
	tmpFilePath := tmpFile.Name()
	klog.V(4).Info("Created temporary token file for Pod SA auth", "path", tmpFilePath)

	if _, err := tmpFile.WriteString(tokenData.Token); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFilePath) // Clean up on error
		return nil, fmt.Errorf("failed to write token to temporary file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFilePath)
		return nil, fmt.Errorf("failed to close temporary token file: %v", err)
	}

	sessionName := roleSessionName
	if c.roleSessionName != "" {
		sessionName = c.roleSessionName
	}
	config := new(credentials.Config).
		SetType(OidcAuthType).
		SetOIDCProviderArn(c.oidcArn).
		SetOIDCTokenFilePath(tmpFilePath).
		SetRoleArn(c.podServiceAccountRoleArn).
		SetRoleSessionName(sessionName)

	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.ErrorS(err, "Pod SA auth failed",
			"namespace", c.podNamespace,
			"serviceAccount", c.podServiceAccount,
			"roleArn", c.podServiceAccountRoleArn)
		return nil, err
	}
	if cred != nil {
		klog.Info("Using pod service account rrsa auth..",
			"namespace", c.podNamespace,
			"serviceAccount", c.podServiceAccount,
			"roleArn", c.podServiceAccountRoleArn)
	}
	// Note: The temporary token file is intentionally not deleted here.
	// The credentials SDK may read the file asynchronously after NewCredential() returns.
	// The file will be cleaned up by the OS temporary file cleaner (e.g., systemd-tmpfiles).
	// File path: tmpFilePath (logged at V(4) for debugging)
	return cred, err
}

type ramRoleAuth struct{ *authConfig }

func (c *ramRoleAuth) NewCredential() (credentials.Credential, error) {
	// Check if ram_role_arn auth type
	if c.accessKey == "" || c.accessSecretKey == "" || c.roleArn == "" {
		return nil, nil
	}
	sessionName := roleSessionName
	if c.roleSessionName != "" {
		sessionName = c.roleSessionName
	}
	config := new(credentials.Config).
		SetType(RamRoleARNAuthType).
		SetAccessKeyId(c.accessKey).
		SetAccessKeySecret(c.accessSecretKey).
		SetRoleArn(c.roleArn).
		SetRoleSessionName(sessionName)
	if c.roleSessionExpiration != "" {
		rseInt, err := strconv.Atoi(c.roleSessionExpiration)
		if err != nil {
			klog.Error(err, "failed to parse roleSessionExpiration", "value", c.roleSessionExpiration)
		} else {
			config.SetRoleSessionExpiration(rseInt)
		}
	}
	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.ErrorS(err, "RAM role ARN auth failed",
			"accessKey", c.accessKey[:min(4, len(c.accessKey))]+"...",
			"roleArn", c.roleArn)
		return nil, err
	}
	if cred != nil {
		klog.Info("Using ram role arn auth", "roleArn", c.roleArn)
	}
	return cred, nil
}

type akAuth struct{ *authConfig }

func (c *akAuth) NewCredential() (credentials.Credential, error) {
	if c.accessKey == "" || c.accessSecretKey == "" {
		return nil, nil
	}
	config := new(credentials.Config).
		SetType(AKAuthType).
		SetAccessKeyId(c.accessKey).
		SetAccessKeySecret(c.accessSecretKey)
	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.ErrorS(err, "AK/SK auth failed",
			"accessKey", c.accessKey[:min(4, len(c.accessKey))]+"...")
		return nil, err
	}
	if cred != nil {
		klog.Info("Using ak/sk auth..")
	}
	return cred, err
}

type nodePublishSecretAuth struct{ *authConfig }

func (c *nodePublishSecretAuth) NewCredential() (credentials.Credential, error) {
	if c.nodePublishSecret == "" {
		return nil, nil
	}

	// The secrets here are the relevant CSI driver (k8s) secrets. See
	// https://kubernetes-csi.github.io/docs/secrets-and-credentials-storage-class.html
	var secret map[string]string
	if err := json.Unmarshal([]byte(c.nodePublishSecret), &secret); err != nil {
		klog.Warningf("nodePublishSecretAuth: failed to parse secrets: %v", err)
		return nil, fmt.Errorf("failed to unmarshal secrets: %v", err)
	}

	var ak, sk string
	for k, v := range secret {
		switch strings.ToLower(k) {
		case "access_key":
			ak = v
		case "access_secret":
			sk = v
		}
	}

	//use ak auth type if ak/sk has set in cluster secret
	if ak == "" || sk == "" {
		return nil, nil
	}

	config := new(credentials.Config).
		SetType(AKAuthType).
		SetAccessKeyId(ak).
		SetAccessKeySecret(sk)
	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.ErrorS(err, "Node publish secret auth failed",
			"accessKey", ak[:min(4, len(ak))]+"...")
		return nil, err
	}
	if cred != nil {
		klog.Info("Using node publish secret auth..")
	}
	return cred, err
}

type ecsRoleAuth struct{ *authConfig }

func (c *ecsRoleAuth) NewCredential() (credentials.Credential, error) {
	//use ecs ramrole auth type default if no auth config given
	klog.V(2).Info("ecsRoleAuth: using ECS metadata service (fallback)")
	config := new(credentials.Config).
		SetType(EcsRamRoleAuthType)
	cred, err := credentials.NewCredential(config)
	if err != nil {
		klog.ErrorS(err, "ECS RAM role auth failed")
		return nil, err
	}
	if cred != nil {
		klog.Info("Using ecs ram role auth..")
	}
	return cred, err
}

type authConfig struct {
	roleArn                   string
	oidcArn                   string
	accessKey                 string
	accessSecretKey           string
	roleSessionName           string
	roleSessionExpiration     string
	nodePublishSecret         string
	podNamespace              string // Pod namespace
	podServiceAccount         string // Pod service account name
	podServiceAccountRoleArn  string // RRSA Role ARN for Pod SA (from SA annotation)
	usePodServiceAccountToken bool   // Whether to use Pod SA token
	tokenFromVolumeContext    string // Service account token from VolumeContext (fallback)
}

// buildOIDCProviderARN constructs OIDC Provider ARN from account ID and cluster ID.
// Format: acs:ram::<account-id>:oidc-provider/ack-rrsa-<cluster-id>
// Returns empty string if environment variables are not set.
func buildOIDCProviderARN() string {
	accountID := os.Getenv(envAccountID)
	clusterID := os.Getenv(envClusterID)

	if accountID == "" || clusterID == "" {
		return ""
	}

	return fmt.Sprintf("acs:ram::%s:oidc-provider/ack-rrsa-%s", accountID, clusterID)
}

// getOIDCProviderARN returns OIDC Provider ARN from environment variable or constructs it automatically.
// Priority: ALICLOUD_OIDC_PROVIDER_ARN > ALICLOUD_ACCOUNT_ID + ALICLOUD_CLUSTER_ID
func getOIDCProviderARN() string {
	// Priority 1: Use explicit OIDC Provider ARN if set
	if oidcArn := os.Getenv("ALICLOUD_OIDC_PROVIDER_ARN"); oidcArn != "" {
		return oidcArn
	}

	// Priority 2: Construct from account ID and cluster ID
	if constructedArn := buildOIDCProviderARN(); constructedArn != "" {
		return constructedArn
	}

	// No OIDC Provider ARN available
	klog.V(2).Info("OIDC Provider ARN not configured (neither ALICLOUD_OIDC_PROVIDER_ARN nor ALICLOUD_ACCOUNT_ID+ALICLOUD_CLUSTER_ID)")
	return ""
}

func GetKMSAuthCred(secrets string, podInfo PodSAInfo) (credentials.Credential, error) {
	// Diagnostic: log secret keys (not values) and pod info
	if secrets != "" {
		var secretKeys map[string]string
		if err := json.Unmarshal([]byte(secrets), &secretKeys); err == nil {
			keys := make([]string, 0, len(secretKeys))
			for k := range secretKeys {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			klog.V(4).Infof("Auth chain input: secret keys=%v, podInfo=(ns=%s, sa=%s, usePodSA=%v, roleArn=%q)",
				keys, podInfo.Namespace, podInfo.ServiceAccount, podInfo.UsePodServiceAccount, podInfo.RoleArn)
		}
	} else {
		klog.V(4).Infof("Auth chain input: secrets is empty, podInfo=(ns=%s, sa=%s, usePodSA=%v, roleArn=%q)",
			podInfo.Namespace, podInfo.ServiceAccount, podInfo.UsePodServiceAccount, podInfo.RoleArn)
	}

	aConfig := &authConfig{
		roleArn:               os.Getenv("ALICLOUD_ROLE_ARN"),
		oidcArn:               getOIDCProviderARN(),
		accessKey:             os.Getenv("ACCESS_KEY_ID"),
		accessSecretKey:       os.Getenv("SECRET_ACCESS_KEY"),
		roleSessionName:       os.Getenv("ALICLOUD_ROLE_SESSION_NAME"),
		roleSessionExpiration: os.Getenv("ALICLOUD_ROLE_SESSION_EXPIRATION"),
		nodePublishSecret:     secrets,
		// Pod SA authentication related fields
		podNamespace:              podInfo.Namespace,
		podServiceAccount:         podInfo.ServiceAccount,
		podServiceAccountRoleArn:  podInfo.RoleArn,
		usePodServiceAccountToken: podInfo.UsePodServiceAccount,
		tokenFromVolumeContext:    podInfo.TokenFromVolumeContext,
	}
	// Authentication chain order: Pod SA -> Provider SA (oidc) -> RAM Role -> Node Publish Secret -> AK -> ECS Role
	root := chainedAuth{name: "podServiceAccountAuth", cred: &podServiceAccountAuth{authConfig: aConfig}}
	root.authNext(&chainedAuth{name: "oidcRoleAuth", cred: &oidcRoleAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{name: "ramRoleAuth", cred: &ramRoleAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{name: "nodePublishSecretAuth", cred: &nodePublishSecretAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{name: "akAuth", cred: &akAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{name: "ecsRoleAuth", cred: &ecsRoleAuth{authConfig: aConfig}})
	return root.NewCredential()
}
