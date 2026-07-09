package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/auth"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/provider"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	kms "github.com/alibabacloud-go/kms-20160120/v3/client"
	oos "github.com/alibabacloud-go/oos-20190601/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
	"github.com/aliyun/credentials-go/credentials/providers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"
)

// Version filled in by Makefile durring build.
var Version string

const (
	namespaceAttrib                 = "csi.storage.k8s.io/pod.namespace"
	acctAttrib                      = "csi.storage.k8s.io/serviceAccount.name"
	podnameAttrib                   = "csi.storage.k8s.io/pod.name"
	regionAttrib                    = "region"          // The attribute name for the region in the SecretProviderClass
	transAttrib                     = "pathTranslation" // Path translation char
	secProvAttrib                   = "objects"         // The attributed used to pass the SecretProviderClass definition (with what to mount)
	defaultKmsEndpoint              = "kms-vpc.%s.aliyuncs.com"
	defaultOosEndpoint              = "oos-vpc.%s.aliyuncs.com"
	suffix                          = "cryptoservice.kms.aliyuncs.com"
	usePodServiceAccountTokenAttrib = "usePodServiceAccountToken"
)

// A Secrets Store CSI Driver provider implementation for Alibaba Cloud Secrets Manager.
type CSIDriverProviderServer struct {
	*grpc.Server
}

// K8s client for reading ServiceAccount annotations
var (
	k8sClient     kubernetes.Interface
	k8sClientMu   sync.Mutex
	k8sClientInit bool
	k8sClientErr  error
)

// getK8sClient returns or creates the Kubernetes client.
// Uses sync.Mutex instead of sync.Once to allow retry on failure.
func getK8sClient() (kubernetes.Interface, error) {
	k8sClientMu.Lock()
	defer k8sClientMu.Unlock()

	// Return cached result if already initialized successfully
	if k8sClientInit {
		return k8sClient, k8sClientErr
	}

	// Try to create the client
	config, err := rest.InClusterConfig()
	if err != nil {
		k8sClientErr = fmt.Errorf("failed to create in-cluster config: %v", err)
		return nil, k8sClientErr
	}

	k8sClient, k8sClientErr = kubernetes.NewForConfig(config)
	k8sClientInit = true
	return k8sClient, k8sClientErr
}

// getRoleArnFromServiceAccount retrieves RoleArn from ServiceAccount annotation
func getRoleArnFromServiceAccount(ctx context.Context, namespace, serviceAccount string) (string, error) {
	client, err := getK8sClient()
	if err != nil {
		return "", fmt.Errorf("failed to get K8s client: %v", err)
	}

	sa, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccount, v1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get ServiceAccount %s/%s: %v", namespace, serviceAccount, err)
	}

	if sa.Annotations == nil {
		return "", nil
	}

	roleArn := sa.Annotations["ack.alibabacloud.com/role-arn"]
	return roleArn, nil
}

// Factory function to create the server to handle incoming mount requests.
func NewServer() (srv *CSIDriverProviderServer, e error) {
	return &CSIDriverProviderServer{}, nil

}

// The provider will fetch the secret value from KMS Secrets Manager and write the secrets to the mount point. The
// version ids of the secrets are then returned to the driver.
func (s *CSIDriverProviderServer) Mount(ctx context.Context, req *v1alpha1.MountRequest) (response *v1alpha1.MountResponse, e error) {

	// Basic sanity check
	if len(req.GetTargetPath()) == 0 {
		return nil, fmt.Errorf("missing mount path")
	}
	mountDir := req.GetTargetPath()

	// Unpack the request.
	var attrib map[string]string
	err := json.Unmarshal([]byte(req.GetAttributes()), &attrib)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal attributes, error: %+v", err)
	}

	// Get the mount attributes.
	nameSpace := attrib[namespaceAttrib]
	svcAcct := attrib[acctAttrib]
	podName := attrib[podnameAttrib]
	region := attrib[regionAttrib]
	translate := attrib[transAttrib]

	// Set the region if one was not specified.
	if len(region) <= 0 {
		region = provider.Region
	}
	klog.Infof("Servicing mount request for pod %s in namespace %s using service account %s with region %s", podName, nameSpace, svcAcct, region)

	// Make a map of the currently mounted versions (if any)
	curVersions := req.GetCurrentObjectVersion()
	curVerMap := make(map[string]*v1alpha1.ObjectVersion)
	for _, ver := range curVersions {
		curVerMap[ver.Id] = ver
	}

	// Unpack the file permission to use.
	var filePermission os.FileMode
	err = json.Unmarshal([]byte(req.GetPermission()), &filePermission)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal file permission, error: %+v", err)
	}

	// Extract authentication configuration from SecretProviderClass parameters
	usePodSA := attrib[usePodServiceAccountTokenAttrib] == "true"

	// Get RoleArn from ServiceAccount annotation (the only source for Pod SA RRSA auth)
	// If RoleArn is empty, Pod SA auth will be skipped and fallback to next auth method.
	var podSARoleArn string
	if usePodSA {
		roleArnFromSA, err := getRoleArnFromServiceAccount(ctx, nameSpace, svcAcct)
		if err != nil {
			klog.Warningf("Failed to get RoleArn from ServiceAccount annotation: %v", err)
		} else {
			podSARoleArn = roleArnFromSA
		}
	}

	// Build Pod SA information
	podInfo := auth.PodSAInfo{
		Namespace:            nameSpace,
		ServiceAccount:       svcAcct,
		RoleArn:              podSARoleArn,
		UsePodServiceAccount: usePodSA,
		// Fallback for CSI Driver < v1.5.0 (unused when serviceAccountTokenInSecrets=true)
		TokenFromVolumeContext: attrib["csi.storage.k8s.io/serviceAccount.tokens"],
	}

	cred, err := auth.GetKMSAuthCred(req.GetSecrets(), podInfo)
	if err != nil {
		return nil, err
	}
	var smProvider provider.SecretsManagerProvider
	descriptors, err := provider.NewSecretObjectList(mountDir, translate, attrib[secProvAttrib])
	if err != nil {
		return nil, err
	}

	var baseKmsClient *kms.Client
	var crossAccountKmsClients sync.Map  // Cache KMS clients by roleArn
	var crossAccountCredentials sync.Map // Cache credentials by roleArn to avoid repeated AssumeRole calls
	var baseOosClient *oos.Client
	var crossAccountOosClients sync.Map // Cache OOS clients by roleArn

	// Predefined supported object types to avoid magic strings
	var supportedObjectTypes = map[string]bool{
		provider.ObjectTypeKMS: true,
		provider.ObjectTypeOOS: true,
		"":                     true, // Empty type defaults to KMS
	}

	for i, descriptor := range descriptors {
		// Validate object type
		if !supportedObjectTypes[descriptor.ObjectType] {
			return nil, fmt.Errorf("unsupported object type %q, only support %q and %q",
				descriptor.ObjectType, provider.ObjectTypeKMS, provider.ObjectTypeOOS)
		}

		// Handle KMS type (including default empty type)
		if descriptor.ObjectType == "" || descriptor.ObjectType == provider.ObjectTypeKMS {
			// Determine which credential to use
			var currentCred credentials.Credential
			if descriptor.CrossAccountRoleArn != "" {
				// Cross-account access: assume target account role
				klog.Infof("Using cross account role for object %s: %s",
					descriptor.ObjectName, descriptor.CrossAccountRoleArn)

				// Use roleArn as cache key
				cacheKey := descriptor.CrossAccountRoleArn

				// Try to get cached credential first
				if cachedCred, exists := crossAccountCredentials.Load(cacheKey); exists {
					currentCred = cachedCred.(credentials.Credential)
					klog.V(4).Infof("Using cached cross-account credential for role: %s", cacheKey)
				} else {
					// Create cross-account credential using base credential
					crossCred, err := createCrossAccountCredential(cred, descriptor.CrossAccountRoleArn, descriptor.ObjectName)
					if err != nil {
						return nil, fmt.Errorf("failed to assume cross account role %s for object %s: %w",
							descriptor.CrossAccountRoleArn, descriptor.ObjectName, err)
					}
					// Cache the credential for reuse
					crossAccountCredentials.Store(cacheKey, crossCred)
					currentCred = crossCred
				}

				// Use roleArn as cache key to prevent sharing clients across different cross-account roles
				// LoadOrStore prevents race condition in concurrent scenarios
				// Load→Store pattern has TOCTOU (Time-of-Check-Time-of-Use) race
				actual, loaded := crossAccountKmsClients.LoadOrStore(cacheKey, (*kms.Client)(nil))
				if !loaded {
					// We got the nil placeholder, we need to create the client
					var client *kms.Client
					if descriptor.KmsEndpoint != "" {
						client, err = newKmsClient(currentCred, descriptor.KmsEndpoint, region)
					} else {
						client, err = newKmsClient(currentCred, "", region)
					}
					if err != nil {
						// Clean up the nil placeholder on error
						crossAccountKmsClients.Delete(cacheKey)
						return nil, fmt.Errorf("create cross account KMS client failed: %w", err)
					}
					// Store the actual client
					crossAccountKmsClients.Store(cacheKey, client)
					descriptors[i].KmsClient = client
				} else {
					// Another goroutine created the client, use it
					if client, ok := actual.(*kms.Client); ok && client != nil {
						descriptors[i].KmsClient = client
					}
				}
			} else {
				// Use base credential
				currentCred = cred

				// Use base client cache
				if baseKmsClient == nil {
					if descriptor.KmsEndpoint != "" {
						baseKmsClient, err = newKmsClient(currentCred, descriptor.KmsEndpoint, region)
					} else {
						baseKmsClient, err = newKmsClient(currentCred, "", region)
					}
					if err != nil {
						return nil, fmt.Errorf("create KMS client failed: %w", err)
					}
				}
				descriptors[i].KmsClient = baseKmsClient
			}
			continue
		}

		// Handle OOS type
		if descriptor.ObjectType == provider.ObjectTypeOOS {
			var currentCred credentials.Credential

			// Determine which credential to use (same logic as KMS)
			if descriptor.CrossAccountRoleArn != "" {
				// Cross-account access: assume target account role
				klog.Infof("Using cross account role for OOS object %s: %s",
					descriptor.ObjectName, descriptor.CrossAccountRoleArn)

				// Use roleArn as cache key
				cacheKey := descriptor.CrossAccountRoleArn

				// Try to get cached credential first
				if cachedCred, exists := crossAccountCredentials.Load(cacheKey); exists {
					currentCred = cachedCred.(credentials.Credential)
					klog.V(4).Infof("Using cached cross-account credential for OOS role: %s", cacheKey)
				} else {
					// Create cross-account credential using base credential
					crossCred, err := createCrossAccountCredential(cred, descriptor.CrossAccountRoleArn, descriptor.ObjectName)
					if err != nil {
						return nil, fmt.Errorf("failed to assume cross account role %s for OOS object %s: %w",
							descriptor.CrossAccountRoleArn, descriptor.ObjectName, err)
					}
					// Cache the credential for reuse
					crossAccountCredentials.Store(cacheKey, crossCred)
					currentCred = crossCred
				}

				// Use roleArn as cache key to prevent sharing clients across different cross-account roles
				actual, loaded := crossAccountOosClients.LoadOrStore(cacheKey, (*oos.Client)(nil))
				if !loaded {
					// We got the nil placeholder, we need to create the client
					client, err := newOosClient(currentCred, region)
					if err != nil {
						// Clean up the nil placeholder on error
						crossAccountOosClients.Delete(cacheKey)
						return nil, fmt.Errorf("create cross account OOS client failed: %w", err)
					}
					// Store the actual client
					crossAccountOosClients.Store(cacheKey, client)
					descriptors[i].OosClient = client
				} else {
					// Another goroutine created the client, use it
					if client, ok := actual.(*oos.Client); ok && client != nil {
						descriptors[i].OosClient = client
					}
				}
			} else {
				// Use base credential (supports Pod SA, Provider RRSA, AK/SK, ECS RAM Role)
				currentCred = cred

				// Use base client cache
				if baseOosClient == nil {
					baseOosClient, err = newOosClient(currentCred, region)
					if err != nil {
						return nil, fmt.Errorf("create OOS client failed: %w", err)
					}
				}
				descriptors[i].OosClient = baseOosClient
			}
			continue
		}
	}

	smProvider = provider.SecretsManagerProvider{
		KmsClient: baseKmsClient,
		OosClient: baseOosClient,
	}

	// Fetch all secrets before saving so we write nothing on failure.
	var fetchedSecrets []*provider.SecretValue
	secrets, err := smProvider.GetSecretValues(descriptors, curVerMap)
	if err != nil {
		return nil, err
	}
	fetchedSecrets = append(fetchedSecrets, secrets...) // Build up the list of all secrets

	// Write out the secrets to the mount point after everything is fetched.
	var files []*v1alpha1.File
	for _, secret := range fetchedSecrets {
		files = append(files, &v1alpha1.File{
			Path:     secret.SecretObj.GetFileName(),
			Contents: secret.Value,
			Mode:     int32(filePermission),
		})
	}
	// Build the version response from the current version map and return it.
	var ov []*v1alpha1.ObjectVersion
	for id := range curVerMap {
		ov = append(ov, curVerMap[id])
	}
	return &v1alpha1.MountResponse{Files: files, ObjectVersion: ov}, nil

}

func newKmsClient(cred credentials.Credential, endpoint, region string) (*kms.Client, error) {
	if endpoint == "" {
		endpoint = defaultKmsEndpoint
		if strings.Contains(endpoint, "%s") {
			endpoint = fmt.Sprintf(endpoint, region)
		}
	}

	config := &openapi.Config{
		Endpoint:   tea.String(endpoint),
		Credential: cred,
	}

	if strings.Contains(endpoint, suffix) {
		config.Ca = tea.String(RegionIdAndCaMap[region])
	}

	kmsClient, err := kms.NewClient(config)

	return kmsClient, err
}

func newOosClient(cred credentials.Credential, region string) (*oos.Client, error) {
	endpoint := defaultOosEndpoint
	if strings.Contains(endpoint, "%s") {
		endpoint = fmt.Sprintf(endpoint, region)
	}
	oosClient, err := oos.NewClient(&openapi.Config{
		Endpoint:   tea.String(endpoint),
		Credential: cred,
	})

	return oosClient, err
}

// createCrossAccountCredential creates a cross-account access credential using the base credential
// credentialProviderAdapter adapts credentials.Credential to providers.CredentialsProvider
// This allows any authentication method (OIDC/RRSA, Worker RAM Role, AK/SK, ECS Instance Profile)
// to be used as the base credential for cross-account AssumeRole operations.
type credentialProviderAdapter struct {
	baseCred credentials.Credential
}

// GetCredentials implements providers.CredentialsProvider interface
func (a *credentialProviderAdapter) GetCredentials() (*providers.Credentials, error) {
	credModel, err := a.baseCred.GetCredential()
	if err != nil {
		return nil, fmt.Errorf("failed to get credential: %w", err)
	}

	creds := &providers.Credentials{}
	if credModel.AccessKeyId != nil {
		creds.AccessKeyId = *credModel.AccessKeyId
	}
	if credModel.AccessKeySecret != nil {
		creds.AccessKeySecret = *credModel.AccessKeySecret
	}
	if credModel.SecurityToken != nil {
		creds.SecurityToken = *credModel.SecurityToken
	}
	return creds, nil
}

// GetProviderName implements providers.CredentialsProvider interface
func (a *credentialProviderAdapter) GetProviderName() string {
	return "cross-account-adapter"
}

// createCrossAccountCredential creates a credential for cross-account access by assuming the target role.
// It supports ALL authentication methods (OIDC/RRSA, Worker RAM Role, AK/SK, ECS Instance Profile)
// by using a credentials provider adapter that wraps the base credential.
func createCrossAccountCredential(baseCred credentials.Credential, roleArn string, objectName string) (credentials.Credential, error) {
	if baseCred == nil {
		return nil, fmt.Errorf("base credential is required for cross-account access")
	}
	if roleArn == "" {
		return nil, fmt.Errorf("roleArn is required for cross-account access")
	}

	klog.Infof("Creating cross-account credential for object %s, assuming role: %s",
		objectName, roleArn)

	// Create a credentials provider adapter that wraps the base credential
	// This allows the SDK to automatically extract credentials from ANY authentication method:
	// - OIDC/RRSA: SDK gets temporary AK/SK/Token from OIDC token exchange
	// - Worker RAM Role: SDK gets credentials from ECS metadata
	// - AK/SK: SDK uses static credentials
	// - ECS Instance Profile: SDK gets credentials from instance metadata
	adapter := &credentialProviderAdapter{baseCred: baseCred}

	// Build the RAM Role ARN provider with the adapter
	// The SDK will:
	// 1. Call adapter.GetCredentials() to get temporary credentials from baseCred
	// 2. Use those credentials to call AssumeRole API
	// 3. Return the assumed role credentials
	ramRoleProvider, err := providers.NewRAMRoleARNCredentialsProviderBuilder().
		WithCredentialsProvider(adapter).
		WithRoleArn(roleArn).
		WithRoleSessionName("csi-secrets-store-cross-account").
		Build()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create cross-account credential provider for role %s: %w. "+
				"Ensure base credential has sts:AssumeRole permission and "+
				"target role trusts the source account",
			roleArn, err)
	}

	// Wrap the provider as a credential
	cred := credentials.FromCredentialsProvider("cross-account-ram-role-arn", ramRoleProvider)

	klog.Infof("Successfully created cross-account credential for role: %s", roleArn)
	return cred, nil
}

// Return the provider plugin version information to the driver.
func (s *CSIDriverProviderServer) Version(ctx context.Context, req *v1alpha1.VersionRequest) (*v1alpha1.VersionResponse, error) {
	return &v1alpha1.VersionResponse{
		Version:        "v1alpha1",
		RuntimeName:    auth.ProviderName,
		RuntimeVersion: Version,
	}, nil

}

func (s *CSIDriverProviderServer) Check(ctx context.Context, in *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

// List returns the health status of all available services.
func (s *CSIDriverProviderServer) List(ctx context.Context, in *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return nil, status.Error(codes.Unimplemented, "List is not supported")
}

// Watch for the serving status of the requested service.
func (s *CSIDriverProviderServer) Watch(req *grpc_health_v1.HealthCheckRequest, w grpc.ServerStreamingServer[grpc_health_v1.HealthCheckResponse]) error {
	return status.Error(codes.Unimplemented, "Watch is not supported")
}
