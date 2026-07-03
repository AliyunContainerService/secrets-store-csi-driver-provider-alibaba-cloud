package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/utils"
	kms "github.com/alibabacloud-go/kms-20160120/v3/client"
	oos "github.com/alibabacloud-go/oos-20190601/v4/client"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// An RE pattern to check for bad paths
var badPathRE = regexp.MustCompile("(/../)|(^../)|(/..$)")

// An individual record from the mount request indicating the secret to be
// fetched and mounted.
type SecretObject struct {

	// Name of the secret
	ObjectName string `json:"objectName"`

	// Optional base file name in which to store the secret (use ObjectName if nil).
	ObjectAlias string `json:"objectAlias"`

	// Optional version id of the secret (default to latest).
	ObjectVersion string `json:"objectVersion"`

	// Optional version/stage label of the secret (defaults to latest).
	ObjectVersionLabel string `json:"objectVersionLabel"`

	// Optional type of the secret (defaults to kms)
	ObjectType string `json:"objectType"`

	//Optional array to specify what json key value pairs to extract from a secret and mount as individual secrets
	JMESPath []JMESPathObject `json:"jmesPath"`

	// Optional endpoint to access KMS Service
	KmsEndpoint string `json:"kmsEndpoint"`

	// Optional cross-account RAM Role ARN for assuming role in target account
	CrossAccountRoleArn string `json:"crossAccountRoleArn"`

	// KMS service client (not part of YAML spec).
	KmsClient *kms.Client `json:"-"`

	// OOS service client (not part of YAML spec).
	OosClient *oos.Client `json:"-"`

	// Path translation character (not part of YAML spec).
	translate string `json:"-"`

	// Mount point directory (not part of YAML spec).
	mountDir string `json:"-"`
}

// An individual json key value pair to mount
type JMESPathObject struct {
	//JMES path to use for retrieval
	Path string `json:"path"`

	//File name in which to store the secret in.
	ObjectAlias string `json:"objectAlias"`
}

// Define validation state structure
type validationState struct {
	names     map[string]bool
	objects   []*SecretObject
	specObj   *SecretObject
	translate string
	mountDir  string
}

// Returns the file name where the secrets are to be written.
func (s *SecretObject) GetFileName() (path string) {
	fileName := s.ObjectName
	if len(s.ObjectAlias) != 0 {
		fileName = s.ObjectAlias
	}

	// Translate slashes to underscore if required.
	if len(s.translate) != 0 {
		fileName = strings.ReplaceAll(fileName, string(os.PathSeparator), s.translate)
	} else {
		fileName = strings.TrimLeft(fileName, string(os.PathSeparator)) // Strip leading slash
	}

	return fileName
}

// In NewSecretObjectList function:
func NewSecretObjectList(mountDir, translate, objectSpec string) ([]*SecretObject, error) {
	// See if we should substitite underscore for slash
	if len(translate) == 0 {
		translate = "_" // Use default
	} else if strings.ToLower(translate) == "false" {
		translate = "" // Turn it off.
	} else if len(translate) != 1 {
		return nil, fmt.Errorf("pathTranslation must be either 'False' or a single character string")
	}

	// Unpack the SecretProviderClass mount specification
	specObjects := make([]*SecretObject, 0)
	err := yaml.Unmarshal([]byte(objectSpec), &specObjects)
	if err != nil {
		return nil, fmt.Errorf("Failed to load SecretProviderClass: %+v", err)
	}

	// Initialize validation state
	state := &validationState{
		names:     make(map[string]bool),
		objects:   make([]*SecretObject, 0, len(specObjects)),
		translate: translate,
		mountDir:  mountDir,
	}

	// Process each specObj
	for _, specObj := range specObjects {
		state.specObj = specObj
		if err := processSecretObject(state); err != nil {
			return nil, err
		}
	}

	return state.objects, nil
}

// Process and validate a single SecretObject
func processSecretObject(state *validationState) error {
	// Set object properties
	state.specObj.translate = state.translate
	state.specObj.mountDir = state.mountDir

	// Validate basic object properties
	if err := state.specObj.validateSecretObject(); err != nil {
		return err
	}

	// Add to validated objects list
	state.objects = append(state.objects, state.specObj)

	// Determine object type (default to KMS)
	objType := state.specObj.ObjectType
	if objType == "" {
		objType = ObjectTypeKMS
	}

	// Check for duplicate object name + type combination
	typeNameKey := fmt.Sprintf("%s:%s:%s", state.specObj.ObjectName, state.specObj.ObjectAlias, objType)
	if state.names[typeNameKey] {
		return fmt.Errorf("duplicate object name: %s (type: %s)", state.specObj.ObjectName, objType)
	}
	state.names[typeNameKey] = true

	// Check for duplicate object alias
	if alias := state.specObj.ObjectAlias; alias != "" {
		if state.names[alias] {
			return fmt.Errorf("duplicate object alias: %s", alias)
		}
		state.names[alias] = true
	}

	// Process JMESPath entries if present
	if len(state.specObj.JMESPath) > 0 {
		klog.Infof("found JMES path defined in SPC: %s", state.specObj.ObjectName)
		for _, jmes := range state.specObj.JMESPath {
			jmesKey := fmt.Sprintf("jmes:%s", jmes.ObjectAlias)
			if state.names[jmesKey] {
				return fmt.Errorf("duplicate JMES path object alias: %s", jmes.ObjectAlias)
			}
			state.names[jmesKey] = true
		}
	}

	return nil
}

// validateSecretObject is used to validate input before it is used by the rest of the plugin.
func (s *SecretObject) validateSecretObject() error {

	if len(s.ObjectName) == 0 {
		return fmt.Errorf("Object name must be specified")
	}

	var objARN utils.ARN
	var err error
	hasARN := strings.HasPrefix(s.ObjectName, "acs:")
	if hasARN {
		objARN, err = utils.ParseARN(s.ObjectName)
		if err != nil {
			return fmt.Errorf("Invalid ARN format in object name: %s", s.ObjectName)
		}
		// Make sure the ARN is for a supported service
		if objARN.Service != "kms" {
			return fmt.Errorf("Invalid service in ARN: %s", objARN.Service)
		}
	}

	// Do not allow ../ in a path when translation is turned off
	if badPathRE.MatchString(s.GetFileName()) {
		return fmt.Errorf("path can not contain ../: %s", s.ObjectName)
	}

	if len(s.JMESPath) == 0 { //jmesPath not specified no more checks
		return nil
	}

	//ensure each jmesPath entry has a path and an objectalias
	for _, jmesPathEntry := range s.JMESPath {
		if len(jmesPathEntry.Path) == 0 {
			return fmt.Errorf("Path must be specified for JMES object")
		}

		if len(jmesPathEntry.ObjectAlias) == 0 {
			return fmt.Errorf("Object alias must be specified for JMES object")
		}
	}

	return nil
}

// GetMountDir return the mount point directory
func (s *SecretObject) GetMountDir() string {
	return s.mountDir
}

// GetMountPath return the full path name (mount point + file) of the file where the seret is stored.
func (s *SecretObject) GetMountPath() string {
	return filepath.Join(s.GetMountDir(), s.GetFileName())
}

func (p *SecretObject) getJmesEntrySecretObject(j *JMESPathObject) (d SecretObject) {
	return SecretObject{
		ObjectAlias: j.ObjectAlias,
		translate:   p.translate,
		mountDir:    p.mountDir,
	}
}
