package provider

import (
	"fmt"
	"github.com/AliyunContainerService/ack-secret-manager/pkg/utils"
	kms "github.com/alibabacloud-go/kms-20160120/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	sdkErr "github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"io/ioutil"
	"k8s.io/klog/v2"
	"math"
	"sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"
	"time"
)

const (
	MAX_RETRY_TIMES               = 5
	REJECTED_THROTTLING           = "Rejected.Throttling"
	SERVICE_UNAVAILABLE_TEMPORARY = "ServiceUnavailableTemporary"
	INTERNAL_FAILURE              = "InternalFailure"
)

var (
	BACKOFF_DEFAULT_RETRY_INTERVAL = time.Second
	BACKOFF_DEFAULT_CAPACITY       = time.Duration(10) * time.Second
)

type SecretsManagerProvider struct {
	KmsClient *kms.Client
}

type SecretFile struct {
	Value    []byte
	Path     string
	FileMode int32
	UID      string
	Version  string
}

// Get the secret from KMS secrets manager.
func (p *SecretsManagerProvider) GetSecretValues(
	secretObjs []*SecretObject,
	curMap map[string]*v1alpha1.ObjectVersion,
) (v []*SecretValue, e error) {

	// Fetch each secret
	var values []*SecretValue
	for _, secObj := range secretObjs {

		// Don't re-fetch if we already have the current version.
		isCurrent, version, err := p.isCurrent(secObj, curMap)
		if err != nil {
			return nil, err
		}

		// If version is current, read it back in, otherwise pull it down
		var secret *SecretValue
		if isCurrent {
			secret, err = p.reloadSecret(secObj)
			if err != nil {
				return nil, err
			}

		} else { // Fetch the latest version.
			version, secret, err = p.fetchSecret(secObj)
			if err != nil {
				return nil, err
			}

		}
		values = append(values, secret) // Build up the slice of values
		//support individual json key value pairs based on jmesPath
		jsonSecrets, err := secret.getJsonSecrets()
		if err != nil {
			return nil, err
		}
		if len(jsonSecrets) > 0 {
			values = append(values, jsonSecrets...)
			// Update the version in the current version map.
			for _, jsonSecret := range jsonSecrets {
				jsonObj := jsonSecret.SecretObj
				curMap[jsonObj.GetFileName()] = &v1alpha1.ObjectVersion{
					Id:      jsonObj.GetFileName(),
					Version: version,
				}
			}
		}

		// Update the version in the current version map.
		curMap[secObj.GetFileName()] = &v1alpha1.ObjectVersion{
			Id:      secObj.GetFileName(),
			Version: version,
		}
	}

	return values, nil
}

func (p *SecretsManagerProvider) isCurrent(
	secObj *SecretObject,
	curMap map[string]*v1alpha1.ObjectVersion,
) (cur bool, ver string, e error) {

	// If we don't have this version, it is not current.
	curVer := curMap[secObj.GetFileName()]
	if curVer == nil {
		return false, "", nil
	}

	// If the secret is pinned to a version see if that is what we have.
	if len(secObj.ObjectVersion) > 0 {
		return curVer.Version == secObj.ObjectVersion, curVer.Version, nil
	}
	return
}

// Private helper to fetch a given secret.
//
// This method builds up the GetSecretValue request using the objectName from
// the request and any objectVersion or objectVersionLabel parameters.
//
func (smp *SecretsManagerProvider) fetchSecret(secObj *SecretObject) (ver string, val *SecretValue, e error) {

	request := &kms.GetSecretValueRequest{
		SecretName: tea.String(secObj.ObjectName),
	}
	if secObj.ObjectVersion != "" {
		request.VersionId = tea.String(secObj.ObjectVersion)
	}
	if secObj.ObjectVersionLabel != "" {
		request.VersionStage = tea.String(secObj.ObjectVersionLabel)
	}
	response, err := smp.KmsClient.GetSecretValue(request)
	for retryTimes := 1; retryTimes < MAX_RETRY_TIMES; retryTimes++ {
		if err != nil {
			if !judgeNeedRetry(err) {
				klog.Error(err, "failed to get secret value from kms", "key", secObj.ObjectName)
				return "", nil, fmt.Errorf("Failed fetching secret %s: %s", secObj.ObjectName, err.Error())
			} else {
				time.Sleep(getWaitTimeExponential(retryTimes))
				response, err = smp.KmsClient.GetSecretValue(request)
				if err != nil && retryTimes == MAX_RETRY_TIMES-1 {
					klog.Error(err, "failed to get secret value from kms", "key", secObj.ObjectName)
					return "", nil, fmt.Errorf("Failed fetching secret %s: %s", secObj.ObjectName, err.Error())
				}
			}
		}
		break
	}
	if *response.Body.SecretDataType == utils.BinaryType {
		klog.Error(err, "not support binary type yet", "key", secObj.ObjectName)
		return "", nil, fmt.Errorf("Secret type not support at %s: %s", secObj.ObjectName, err.Error())

	}
	return *response.Body.VersionId, &SecretValue{Value: []byte(*response.Body.SecretData), SecretObj: *secObj}, nil
}

func judgeNeedRetry(err error) bool {
	respErr, is := err.(*sdkErr.ClientError)
	if is && (respErr.ErrorCode() == REJECTED_THROTTLING || respErr.ErrorCode() == SERVICE_UNAVAILABLE_TEMPORARY || respErr.ErrorCode() == INTERNAL_FAILURE) {
		return true
	}
	return false
}

func getWaitTimeExponential(retryTimes int) time.Duration {
	sleepInterval := time.Duration(math.Pow(2, float64(retryTimes))) * BACKOFF_DEFAULT_RETRY_INTERVAL
	if sleepInterval >= BACKOFF_DEFAULT_CAPACITY {
		return BACKOFF_DEFAULT_CAPACITY
	} else {
		return sleepInterval
	}
}

// Reload a secret from the file system.
func (p *SecretsManagerProvider) reloadSecret(secObj *SecretObject) (val *SecretValue, e error) {
	sValue, err := ioutil.ReadFile(secObj.GetMountPath())
	if err != nil {
		return nil, err
	}

	return &SecretValue{Value: sValue, SecretObj: *secObj}, nil
}
