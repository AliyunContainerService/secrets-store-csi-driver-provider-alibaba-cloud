// framework/cluster.go - Kubernetes client wrapper for E2E tests
package framework

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	// ProviderDaemonSetName is the name of the CSI Provider DaemonSet
	ProviderDaemonSetName = "csi-secrets-store-provider-alibabacloud"
	// ProviderDaemonSetNamespace is the namespace of the CSI Provider DaemonSet
	ProviderDaemonSetNamespace = "kube-system"
	// TestPodImage is the container image used for test Pods (accessible from China region clusters)
	TestPodImage = "anolis-registry.cn-zhangjiakou.cr.aliyuncs.com/openanolis/nginx:1.14.1-8.6"
)

// SPC GVR for SecretProviderClass
var SPCGVR = schema.GroupVersionResource{
	Group:    "secrets-store.csi.x-k8s.io",
	Version:  "v1",
	Resource: "secretproviderclasses",
}

// Framework holds the Kubernetes client and test configuration
type Framework struct {
	Clientset  kubernetes.Interface
	Dynamic    dynamic.Interface
	RestConfig *rest.Config
	Namespace  string
}

// NewFramework creates a new Framework instance from kubeconfig
func NewFramework(kubeconfig string) (*Framework, error) {
	cfg, err := buildConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Framework{
		Clientset:  clientset,
		Dynamic:    dynamicClient,
		RestConfig: cfg,
	}, nil
}

// CreateNamespace creates a namespace with a random suffix
func (f *Framework) CreateNamespace(ctx context.Context, baseName string) (string, error) {
	name := fmt.Sprintf("%s-%s", baseName, randomSuffix())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := f.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("failed to create namespace %s: %w", name, err)
	}
	f.Namespace = name
	return name, nil
}

// DeleteNamespace deletes a namespace and waits for it to be removed
func (f *Framework) DeleteNamespace(ctx context.Context, name string) error {
	err := f.Clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}
	return nil
}

// WaitForNamespaceDeleted waits until the namespace is fully deleted
func (f *Framework) WaitForNamespaceDeleted(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := f.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for namespace %s to be deleted", name)
}

// CreateSecretProviderClass creates a SecretProviderClass resource
func (f *Framework) CreateSecretProviderClass(ctx context.Context, namespace string, spc *unstructured.Unstructured) error {
	_, err := f.Dynamic.Resource(SPCGVR).Namespace(namespace).Create(ctx, spc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create SecretProviderClass: %w", err)
	}
	return nil
}

// DeleteSecretProviderClass deletes a SecretProviderClass resource
func (f *Framework) DeleteSecretProviderClass(ctx context.Context, namespace, name string) error {
	err := f.Dynamic.Resource(SPCGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete SecretProviderClass %s: %w", name, err)
	}
	return nil
}

// CreatePod creates a Pod resource using the dynamic client for full spec control
func (f *Framework) CreatePod(ctx context.Context, namespace string, pod *unstructured.Unstructured) error {
	_, err := f.Dynamic.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}).Namespace(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create pod %s: %w", pod.GetName(), err)
	}
	return nil
}

// WaitForPodReady waits for a Pod to be in Running phase
func (f *Framework) WaitForPodReady(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := f.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err == nil && pod.Status.Phase == corev1.PodRunning {
			return nil
		}
		if err == nil && pod.Status.Phase == corev1.PodFailed {
			return fmt.Errorf("pod %s/%s failed", namespace, podName)
		}
		time.Sleep(5 * time.Second)
	}

	baseErr := fmt.Errorf("timeout waiting for pod %s/%s to be ready", namespace, podName)
	diag := collectPodDiagnostics(namespace, podName)
	if diag != "" {
		return fmt.Errorf("%w\n\n%s", baseErr, diag)
	}
	return baseErr
}

// collectPodDiagnostics gathers diagnostic information for a timed-out Pod.
// It returns a formatted string with Pod Events and Provider logs, or empty string on failure.
func collectPodDiagnostics(namespace, podName string) string {
	var sb strings.Builder

	// Collect Pod Events via kubectl describe
	if events := runKubectl("describe", "pod", podName, "-n", namespace); events != "" {
		lines := strings.Split(strings.TrimSpace(events), "\n")
		// Keep last 30 lines to focus on recent events
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		sb.WriteString("--- Pod Events ---\n")
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}

	// Collect Provider Pod logs
	if logs := collectProviderLogs(); logs != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("--- Provider Logs ---\n")
		sb.WriteString(logs)
	}

	return sb.String()
}

// collectProviderLogs finds the Provider DaemonSet Pod and retrieves its recent logs.
func collectProviderLogs() string {
	// Find provider pod name via label selector
	out := runKubectl("get", "pods", "-n", ProviderDaemonSetNamespace,
		"-l", "app="+ProviderDaemonSetName,
		"-o", "jsonpath={.items[0].metadata.name}")
	providerPod := strings.TrimSpace(out)
	if providerPod == "" {
		return ""
	}

	return runKubectl("logs", providerPod, "-n", ProviderDaemonSetNamespace, "--tail=30")
}

// runKubectl executes a kubectl command and returns its stdout, or empty string on failure.
func runKubectl(args ...string) string {
	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	return stdout.String()
}

// DeletePod deletes a Pod
func (f *Framework) DeletePod(ctx context.Context, namespace, podName string) error {
	err := f.Clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pod %s/%s: %w", namespace, podName, err)
	}
	return nil
}

// PatchDaemonSet patches a DaemonSet with a JSON patch
func (f *Framework) PatchDaemonSet(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := f.Clientset.AppsV1().DaemonSets(namespace).Patch(
		ctx, name,
		"application/json-patch+json",
		patch,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch DaemonSet %s/%s: %w", namespace, name, err)
	}
	return nil
}

// WaitForDaemonSetReady waits for a DaemonSet to have all pods ready
func (f *Framework) WaitForDaemonSetReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ds, err := f.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
			ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for DaemonSet %s/%s to be ready", namespace, name)
}

// WaitForDaemonSetRollout waits for a DaemonSet rolling update to complete.
// It verifies that:
//  1. The observedGeneration has advanced past preGeneration (new spec observed)
//  2. All desired pods are updated (UpdatedNumberScheduled == DesiredNumberScheduled)
//  3. All updated pods are ready (NumberReady == DesiredNumberScheduled)
//  4. No unavailable pods remain (NumberUnavailable == 0)
func (f *Framework) WaitForDaemonSetRollout(ctx context.Context, namespace, name string, preGeneration int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastLog := time.Now()
	for time.Now().Before(deadline) {
		ds, err := f.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			// Log progress every 15 seconds
			if time.Since(lastLog) > 15*time.Second {
				if _, err := fmt.Fprintf(ginkgo.GinkgoWriter, "DaemonSet rollout status: generation=%d/%d, updated=%d/%d, ready=%d/%d, unavailable=%d\n",
					ds.Status.ObservedGeneration, preGeneration,
					ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled,
					ds.Status.NumberReady, ds.Status.DesiredNumberScheduled,
					ds.Status.NumberUnavailable); err != nil {
					klog.Warningf("failed to write rollout status log: %v", err)
				}
				lastLog = time.Now()
			}
			if ds.Status.DesiredNumberScheduled > 0 &&
				ds.Status.ObservedGeneration >= preGeneration+1 &&
				ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
				ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
				ds.Status.NumberUnavailable == 0 {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	// Collect final status for error message
	ds, _ := f.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	baseErr := fmt.Errorf("timeout waiting for DaemonSet %s/%s rollout (generation=%d/%d, updated=%d/%d, ready=%d/%d, unavailable=%d)",
		namespace, name,
		ds.Status.ObservedGeneration, preGeneration,
		ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled,
		ds.Status.NumberReady, ds.Status.DesiredNumberScheduled,
		ds.Status.NumberUnavailable)

	// Collect Pod diagnostics to help diagnose the root cause of the timeout.
	// This is especially useful when Pods are stuck in ImagePullBackOff, CrashLoopBackOff,
	// or CreateContainerConfigError (e.g. due to a missing SecretKeyRef after an env change).
	var diagnostics []string
	pods, listErr := f.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	if listErr == nil {
		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					diagnostics = append(diagnostics, fmt.Sprintf(
						"Pod %s container %s: %s - %s",
						pod.Name, cs.Name,
						cs.State.Waiting.Reason,
						cs.State.Waiting.Message))
				} else if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
					diagnostics = append(diagnostics, fmt.Sprintf(
						"Pod %s container %s: terminated (exit=%d, reason=%s)",
						pod.Name, cs.Name,
						cs.State.Terminated.ExitCode,
						cs.State.Terminated.Reason))
				}
			}
			// Also check Pod-level conditions (e.g. scheduling failures)
			for _, cond := range pod.Status.Conditions {
				if cond.Status == corev1.ConditionFalse && cond.Reason != "" {
					diagnostics = append(diagnostics, fmt.Sprintf(
						"Pod %s condition %s: %s - %s",
						pod.Name, cond.Type, cond.Reason, cond.Message))
				}
			}
		}
	}

	if len(diagnostics) > 0 {
		return fmt.Errorf("%w\n\nPod diagnostics:\n  %s", baseErr, strings.Join(diagnostics, "\n  "))
	}
	return baseErr
}

// CreateSecret creates a Kubernetes Secret
func (f *Framework) CreateSecret(ctx context.Context, namespace string, secret *corev1.Secret) error {
	_, err := f.Clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create secret %s: %w", secret.Name, err)
	}
	return nil
}

// DeleteSecret deletes a Kubernetes Secret
func (f *Framework) DeleteSecret(ctx context.Context, namespace, name string) error {
	err := f.Clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// CreateServiceAccount creates a Kubernetes ServiceAccount
func (f *Framework) CreateServiceAccount(ctx context.Context, namespace string, sa *corev1.ServiceAccount) error {
	_, err := f.Clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create service account %s: %w", sa.Name, err)
	}
	return nil
}

// DeleteServiceAccount deletes a Kubernetes ServiceAccount
func (f *Framework) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	err := f.Clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete service account %s/%s: %w", namespace, name, err)
	}
	return nil
}

// buildConfig builds a rest.Config from kubeconfig path
func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	if _, err := os.Stat(kubeconfig); err == nil {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	// Fall back to in-cluster config
	return rest.InClusterConfig()
}

// randomSuffix generates a random 8-character suffix
func randomSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	result := make([]byte, 8)
	for i := range result {
		result[i] = charset[r.Intn(len(charset))]
	}
	return string(result)
}

// BuildSPC constructs a SecretProviderClass unstructured object
func BuildSPC(name, provider, objects, secretObjects string, params map[string]string) *unstructured.Unstructured {
	spc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "secrets-store.csi.x-k8s.io/v1",
			"kind":       "SecretProviderClass",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"provider": provider,
			},
		},
	}

	if objects != "" {
		_ = unstructured.SetNestedField(spc.Object, objects, "spec", "parameters", "objects")
	}

	// Set parameters
	paramsMap := make(map[string]interface{})
	for k, v := range params {
		paramsMap[k] = v
	}
	if objects != "" {
		paramsMap["objects"] = objects
	}
	_ = unstructured.SetNestedStringMap(spc.Object, params, "spec", "parameters")

	if secretObjects != "" {
		// secretObjects is a JSON/YAML string that needs to be parsed
		// For simplicity, we set it as a raw field
		_ = unstructured.SetNestedField(spc.Object, secretObjects, "spec", "secretObjects")
	}

	return spc
}

// BuildPod constructs a basic Pod unstructured object with CSI volume mount
func BuildPod(name, saName, spcName, mountPath string) *unstructured.Unstructured {
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
				"serviceAccountName": saName,
				"containers": []interface{}{
					map[string]interface{}{
						"name":    name,
						"image":   TestPodImage,
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
