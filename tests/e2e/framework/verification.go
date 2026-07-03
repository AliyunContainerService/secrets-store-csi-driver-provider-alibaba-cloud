// framework/verification.go - CSI mount and Secret verification helpers
package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultVerifyTimeout is the default timeout for verification operations
	DefaultVerifyTimeout = 2 * time.Minute
	// DefaultVerifyPollInterval is the default polling interval
	DefaultVerifyPollInterval = 5 * time.Second
)

// VerifyMountedFile executes into a pod and reads a file, comparing its content
func (f *Framework) VerifyMountedFile(ctx context.Context, podName, namespace, mountPath, expectedContent string) error {
	var lastErr error
	deadline := time.Now().Add(DefaultVerifyTimeout)

	for time.Now().Before(deadline) {
		actual, err := f.ExecInPod(ctx, namespace, podName, []string{"cat", mountPath})
		if err != nil {
			lastErr = fmt.Errorf("failed to read file %s: %w", mountPath, err)
			time.Sleep(DefaultVerifyPollInterval)
			continue
		}

		// Trim whitespace (including \r\n) for comparison
		actual = strings.TrimSpace(actual)
		expected := strings.TrimSpace(expectedContent)

		if actual == expected {
			return nil
		}
		lastErr = fmt.Errorf("content mismatch: expected %q, got %q", expected, actual)
		time.Sleep(DefaultVerifyPollInterval)
	}

	return fmt.Errorf("timeout verifying mounted file: %w", lastErr)
}

// VerifyMountedFileContains executes into a pod and checks if a file contains a substring
func (f *Framework) VerifyMountedFileContains(ctx context.Context, podName, namespace, mountPath, substring string) error {
	var lastErr error
	deadline := time.Now().Add(DefaultVerifyTimeout)

	for time.Now().Before(deadline) {
		actual, err := f.ExecInPod(ctx, namespace, podName, []string{"cat", mountPath})
		if err != nil {
			lastErr = fmt.Errorf("failed to read file %s: %w", mountPath, err)
			time.Sleep(DefaultVerifyPollInterval)
			continue
		}

		if strings.Contains(actual, substring) {
			return nil
		}
		lastErr = fmt.Errorf("file %s does not contain %q, content: %q", mountPath, substring, actual)
		time.Sleep(DefaultVerifyPollInterval)
	}

	return fmt.Errorf("timeout verifying mounted file contains: %w", lastErr)
}

// VerifySecretExists checks if a Kubernetes Secret exists
func (f *Framework) VerifySecretExists(ctx context.Context, name, namespace string) error {
	var lastErr error
	deadline := time.Now().Add(DefaultVerifyTimeout)

	for time.Now().Before(deadline) {
		_, err := f.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			return nil
		}
		if !k8serrors.IsNotFound(err) {
			lastErr = err
		}
		time.Sleep(DefaultVerifyPollInterval)
	}

	if lastErr != nil {
		return fmt.Errorf("secret %s/%s not found: %w", namespace, name, lastErr)
	}
	return fmt.Errorf("timeout: secret %s/%s not found", namespace, name)
}

// VerifySecretData checks if a Kubernetes Secret contains expected data
func (f *Framework) VerifySecretData(ctx context.Context, name, namespace, key, expectedValue string) error {
	var lastErr error
	deadline := time.Now().Add(DefaultVerifyTimeout)

	for time.Now().Before(deadline) {
		secret, err := f.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			lastErr = fmt.Errorf("failed to get secret %s/%s: %w", namespace, name, err)
			time.Sleep(DefaultVerifyPollInterval)
			continue
		}

		data, ok := secret.Data[key]
		if !ok {
			lastErr = fmt.Errorf("secret %s/%s does not contain key %q", namespace, name, key)
			time.Sleep(DefaultVerifyPollInterval)
			continue
		}

		actual := strings.TrimSpace(string(data))
		expected := strings.TrimSpace(expectedValue)
		if actual == expected {
			return nil
		}
		lastErr = fmt.Errorf("secret data mismatch for key %q: expected %q, got %q", key, expected, actual)
		time.Sleep(DefaultVerifyPollInterval)
	}

	return fmt.Errorf("timeout verifying secret data: %w", lastErr)
}

// VerifySecretNotExists checks that a Kubernetes Secret does NOT exist (for cleanup verification)
func (f *Framework) VerifySecretNotExists(ctx context.Context, name, namespace string) error {
	deadline := time.Now().Add(DefaultVerifyTimeout)

	for time.Now().Before(deadline) {
		_, err := f.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		time.Sleep(DefaultVerifyPollInterval)
	}

	return fmt.Errorf("timeout: secret %s/%s still exists", namespace, name)
}

// ExecInPod executes a command in a pod using kubectl exec and returns stdout
func (f *Framework) ExecInPod(ctx context.Context, namespace, podName string, command []string) (string, error) {
	args := []string{"exec", podName, "-n", namespace, "-c", podName, "--"}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl exec failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// WaitForPodDeleted waits until a pod is fully deleted
func (f *Framework) WaitForPodDeleted(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := f.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		time.Sleep(DefaultVerifyPollInterval)
	}
	return fmt.Errorf("timeout waiting for pod %s/%s to be deleted", namespace, podName)
}

// GetSecretData retrieves a specific key from a Kubernetes Secret
func (f *Framework) GetSecretData(ctx context.Context, name, namespace, key string) (string, error) {
	secret, err := f.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", namespace, name, err)
	}

	data, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s does not contain key %q", namespace, name, key)
	}

	return string(data), nil
}

// Ensure Framework implements the interface needed for verification
var _ PodExecutor = &Framework{}

// PodExecutor interface for testing
type PodExecutor interface {
	ExecInPod(ctx context.Context, namespace, podName string, command []string) (string, error)
}

// Helper to create a simple test pod with CSI volume
func NewTestPodWithCSI(podName, spcName, mountPath string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: map[string]string{"app": podName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    podName,
					Image:   TestPodImage,
					Command: []string{"sleep", "3600"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "secrets-store",
							MountPath: mountPath,
							ReadOnly:  true,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "secrets-store",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "secrets-store.csi.k8s.io",
							ReadOnly: boolPtr(true),
							VolumeAttributes: map[string]string{
								"secretProviderClass": spcName,
							},
						},
					},
				},
			},
		},
	}
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool {
	return &b
}
