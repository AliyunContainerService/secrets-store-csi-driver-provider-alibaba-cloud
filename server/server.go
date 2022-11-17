package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/auth"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/provider"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/utils"
	openapi "github.com/alibabacloud-go/darabonba-openapi/client"
	kms "github.com/alibabacloud-go/kms-20160120/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"os"
	"sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"
	"strings"
)

// Version filled in by Makefile durring build.
var Version string

const (
	namespaceAttrib  = "csi.storage.k8s.io/pod.namespace"
	acctAttrib       = "csi.storage.k8s.io/serviceAccount.name"
	podnameAttrib    = "csi.storage.k8s.io/pod.name"
	regionAttrib     = "region"          // The attribute name for the region in the SecretProviderClass
	transAttrib      = "pathTranslation" // Path translation char
	secProvAttrib    = "objects"         // The attributed used to pass the SecretProviderClass definition (with what to mount)
	defaultKmsDomain = "kms-vpc.%s.aliyuncs.com"
	//MetadataURL is the ECS metadata server addr
	metadataURL = "http://100.100.100.200/latest/meta-data/"
	regionID    = "region-id"
)

// A Secrets Store CSI Driver provider implementation for Alibaba Cloud Secrets Manager.
type CSIDriverProviderServer struct {
	*grpc.Server
}

// Factory function to create the server to handle incoming mount requests.
//
func NewServer() (srv *CSIDriverProviderServer, e error) {
	return &CSIDriverProviderServer{}, nil

}

// The provider will fetch the secret value from KMS Secrets Manager and write the secrets to the mount point. The
// version ids of the secrets are then returned to the driver.
func (s *CSIDriverProviderServer) Mount(ctx context.Context, req *v1alpha1.MountRequest) (response *v1alpha1.MountResponse, e error) {

	// Basic sanity check
	if len(req.GetTargetPath()) == 0 {
		return nil, fmt.Errorf("Missing mount path")
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

	// Lookup the region if one was not specified.
	if len(region) <= 0 {
		region, err = utils.GetRegion()
		if region == "" {
			return nil, fmt.Errorf("failed to retrieve region from node. error %+v", err)
		}
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

	// Get the pod's Alibaba Cloud creds.
	cred, err := auth.GetKMSAuthCred(req.GetSecrets())
	if err != nil {
		return nil, err
	}
	domain := defaultKmsDomain
	if strings.Contains(domain, "%s") {
		domain = fmt.Sprintf(domain, region)
	}
	kmsClient, err := kms.NewClient(&openapi.Config{
		Endpoint:   tea.String(domain),
		Credential: cred,
	})
	if err != nil {
		return nil, err
	}

	// Get the list of secrets to mount. These will be grouped together by type
	// in a map of slices (map[string][]*SecretDescriptor) keyed by secret type
	// so that requests can be batched if the implementation allows it.
	descriptors, err := provider.NewSecretObjectList(mountDir, translate, attrib[secProvAttrib])
	if err != nil {
		return nil, err
	}
	smProvider := provider.SecretsManagerProvider{kmsClient}
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

// Return the provider plugin version information to the driver.
//
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

// Watch for the serving status of the requested service.
func (s *CSIDriverProviderServer) Watch(req *grpc_health_v1.HealthCheckRequest, w grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not supported")
}

//func (s *CSIDriverProviderServer) writeFile(secret *provider.SecretValue, mode os.FileMode) (*v1alpha1.File, error) {
//	// Write to a tempfile first
//	tmpFile, err := ioutil.TempFile(secret.SecretObj.GetMountDir(), secret.SecretObj.GetFileName())
//	if err != nil {
//		return nil, err
//	}
//	defer os.Remove(tmpFile.Name())
//	defer tmpFile.Close()
//
//	err = tmpFile.Chmod(mode) // Set correct permissions
//	if err != nil {
//		return nil, err
//	}
//
//	_, err = tmpFile.Write(secret.Value) // Write the secret
//	if err != nil {
//		return nil, err
//	}
//
//	// Make sure to flush to disk
//	err = tmpFile.Sync()
//	if err != nil {
//		return nil, err
//	}
//
//	// Swap out the old secret for the new
//	err = os.Rename(tmpFile.Name(), secret.SecretObj.GetMountPath())
//	if err != nil {
//		return nil, err
//	}
//
//	return nil, nil
//}
