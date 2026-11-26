package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// newPodSAAuthConfig builds a podServiceAccountAuth config whose Pod SA token
// is delivered via nodePublishSecret (CSI Driver v1.5.0+ primary path).
func newPodSAAuthConfig(namespace, serviceAccount, token string) *authConfig {
	tokensJSON, err := json.Marshal(map[string]map[string]string{
		"sts.aliyuncs.com": {"token": token},
	})
	if err != nil {
		panic(err)
	}
	secrets, err := json.Marshal(map[string]string{
		"csi.storage.k8s.io/serviceAccount.tokens": string(tokensJSON),
	})
	if err != nil {
		panic(err)
	}
	return &authConfig{
		oidcArn:                   "acs:ram::1234567890:oidc-provider/ack-rrsa-ctest",
		nodePublishSecret:         string(secrets),
		podNamespace:              namespace,
		podServiceAccount:         serviceAccount,
		podServiceAccountRoleArn:  "acs:ram::1234567890:role/test-pod-sa-role",
		usePodServiceAccountToken: true,
	}
}

// When any Pod SA precondition is unmet, NewCredential must return (nil, nil)
// so the auth chain falls through to the next method.
func TestPodSAAuthPreconditionsUnmetFallback(t *testing.T) {
	valid := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")

	cases := []struct {
		name   string
		mutate func(cfg *authConfig)
	}{
		{"pod SA auth disabled", func(cfg *authConfig) { cfg.usePodServiceAccountToken = false }},
		{"missing pod SA role arn", func(cfg *authConfig) { cfg.podServiceAccountRoleArn = "" }},
		{"missing pod namespace", func(cfg *authConfig) { cfg.podNamespace = "" }},
		{"missing pod service account", func(cfg *authConfig) { cfg.podServiceAccount = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *valid
			tc.mutate(&cfg)
			auth := &podServiceAccountAuth{authConfig: &cfg}

			cred, err := auth.NewCredential()
			if err != nil {
				t.Fatalf("expected fallback (nil, nil), got error: %v", err)
			}
			if cred != nil {
				t.Fatalf("expected nil credential for fallback, got %v", cred)
			}
		})
	}
}

// When Pod SA auth is explicitly enabled and all preconditions are met but the
// token JSON is absent from both secrets and volume context, NewCredential must
// return (nil, error) to abort the auth chain instead of silently falling back.
func TestPodSAAuthMissingTokenAborts(t *testing.T) {
	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")
	// Strip the token from both delivery paths while keeping all preconditions set.
	cfg.nodePublishSecret = ""
	cfg.tokenFromVolumeContext = ""
	auth := &podServiceAccountAuth{authConfig: cfg}

	cred, err := auth.NewCredential()
	if err == nil {
		t.Fatalf("expected hard failure (nil, error), got nil error (cred=%v)", cred)
	}
	if cred != nil {
		t.Fatalf("expected nil credential on error, got %v", cred)
	}
	if !strings.Contains(err.Error(), "aborting auth chain") {
		t.Fatalf("expected error to mention 'aborting auth chain', got: %v", err)
	}
}

// A malformed tokens JSON must abort with an error (no fallback).
func TestPodSAAuthInvalidTokensJSON(t *testing.T) {
	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")
	secrets, err := json.Marshal(map[string]string{
		"csi.storage.k8s.io/serviceAccount.tokens": "{not-valid-json",
	})
	if err != nil {
		t.Fatalf("failed to marshal secrets: %v", err)
	}
	cfg.nodePublishSecret = string(secrets)
	auth := &podServiceAccountAuth{authConfig: cfg}

	cred, err := auth.NewCredential()
	if err == nil {
		t.Fatalf("expected unmarshal error, got nil (cred=%v)", cred)
	}
	if cred != nil {
		t.Fatalf("expected nil credential on error, got %v", cred)
	}
	if !strings.Contains(err.Error(), "failed to unmarshal service account tokens") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// Missing sts.aliyuncs.com audience must abort the auth chain with an error.
func TestPodSAAuthMissingSTSAudienceAborts(t *testing.T) {
	tokensJSON, err := json.Marshal(map[string]map[string]string{
		"api.aliyuncs.com": {"token": "jwt-other-audience"},
	})
	if err != nil {
		t.Fatalf("failed to marshal tokens: %v", err)
	}
	secrets, err := json.Marshal(map[string]string{
		"csi.storage.k8s.io/serviceAccount.tokens": string(tokensJSON),
	})
	if err != nil {
		t.Fatalf("failed to marshal secrets: %v", err)
	}
	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")
	cfg.nodePublishSecret = string(secrets)
	auth := &podServiceAccountAuth{authConfig: cfg}

	cred, err := auth.NewCredential()
	if err == nil {
		t.Fatalf("expected audience error, got nil (cred=%v)", cred)
	}
	if cred != nil {
		t.Fatalf("expected nil credential on error, got %v", cred)
	}
	if !strings.Contains(err.Error(), "sts.aliyuncs.com") ||
		!strings.Contains(err.Error(), "aborting auth chain") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// With a valid config, NewCredential must succeed. credentials-go builds the
// oidc_role_arn provider lazily (STS AssumeRoleWithOIDC is only called on
// credential retrieval), so construction makes no network request.
func TestPodSAAuthInMemoryTokenBuildsCredential(t *testing.T) {
	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")
	auth := &podServiceAccountAuth{authConfig: cfg}

	cred, err := auth.NewCredential()
	if err != nil {
		t.Fatalf("NewCredential failed: %v", err)
	}
	if cred == nil {
		t.Fatal("expected non-nil credential, got nil")
	}
	if credType := cred.GetType(); credType == nil || *credType != OidcAuthType {
		t.Fatalf("credential type = %v, want %q", credType, OidcAuthType)
	}
}

// The legacy VolumeContext token path must also produce a credential.
func TestPodSAAuthTokenFromVolumeContext(t *testing.T) {
	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "ignored")
	cfg.nodePublishSecret = ""
	tokensJSON, err := json.Marshal(map[string]map[string]string{
		"sts.aliyuncs.com": {"token": "jwt-from-volume-context"},
	})
	if err != nil {
		t.Fatalf("failed to marshal tokens: %v", err)
	}
	cfg.tokenFromVolumeContext = string(tokensJSON)
	auth := &podServiceAccountAuth{authConfig: cfg}

	cred, err := auth.NewCredential()
	if err != nil {
		t.Fatalf("NewCredential failed: %v", err)
	}
	if cred == nil {
		t.Fatal("expected non-nil credential, got nil")
	}
}

// TestPodSAAuthDoesNotWriteTokenFile verifies the in-memory token path never
// writes to disk.
//
// Coverage boundary: This test asserts that credentials-go's NewCredential
// construction does not create or modify files in TMPDIR. It uses a decoy file
// to detect unintended overwrites and checks for any new entries. The test does
// NOT exercise the SDK's runtime oidcTokenValue() path (which would require a
// real STS endpoint), but since we pass Config.SetOIDCToken(token) the SDK
// stores the token in an unexported struct field and returns it directly from
// oidcTokenValue() without any file I/O. A regression to SetOIDCTokenFilePath
// would be caught only if the SDK eagerly reads the file at construction time;
// the type-check in TestPodSAAuthInMemoryTokenBuildsCredential provides
// additional protection against config-level regressions.
func TestPodSAAuthDoesNotWriteTokenFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	if got := os.TempDir(); got != tmpDir {
		t.Fatalf("os.TempDir() = %q, want %q", got, tmpDir)
	}

	// Place a decoy file to detect unintended writes/overwrites.
	const decoyName = "oidc-token-decoy"
	const decoyContent = "decoy-unchanged-content"
	decoyPath := tmpDir + "/" + decoyName
	if err := os.WriteFile(decoyPath, []byte(decoyContent), 0600); err != nil {
		t.Fatalf("failed to write decoy file: %v", err)
	}

	cfg := newPodSAAuthConfig("argocd", "k8s-proxy", "jwt-token-v1")
	auth := &podServiceAccountAuth{authConfig: cfg}

	const mounts = 3
	for i := 0; i < mounts; i++ {
		cred, err := auth.NewCredential()
		if err != nil {
			t.Fatalf("mount %d: NewCredential failed: %v", i, err)
		}
		if cred == nil {
			t.Fatalf("mount %d: expected non-nil credential, got nil", i)
		}
	}

	// Assert decoy file was not modified.
	got, err := os.ReadFile(decoyPath)
	if err != nil {
		t.Fatalf("failed to read decoy file: %v", err)
	}
	if string(got) != decoyContent {
		t.Fatalf("decoy file was modified: got %q, want %q", string(got), decoyContent)
	}

	// Assert no new files were created (only the decoy should exist).
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only decoy file in temp dir after %d mounts, found: %v", mounts, names)
	}
	if entries[0].Name() != decoyName {
		t.Fatalf("unexpected file %q in temp dir (expected only %q)", entries[0].Name(), decoyName)
	}
}
