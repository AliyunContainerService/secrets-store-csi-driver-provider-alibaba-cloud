package provider

import (
	"fmt"
	"github.com/AliyunContainerService/secrets-store-csi-driver-provider-alibaba-cloud/utils"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"
)

// An RE pattern to check for bad paths
var badPathRE = regexp.MustCompile("(/\\.\\./)|(^\\.\\./)|(/\\.\\.$)")

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

	//Optional array to specify what json key value pairs to extract from a secret and mount as individual secrets
	JMESPath []JMESPathObject `json:"jmesPath"`

	// Path translation character (not part of YAML spec).
	translate string `json:"-"`

	// Mount point directory (not part of YAML spec).
	mountDir string `json:"-"`
}

//An individual json key value pair to mount
type JMESPathObject struct {
	//JMES path to use for retrieval
	Path string `json:"path"`

	//File name in which to store the secret in.
	ObjectAlias string `json:"objectAlias"`
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

func NewSecretObjectList(mountDir, translate, objectSpec string) (objects []*SecretObject, e error) {

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

	// Validate each record and check for duplicates
	names := make(map[string]bool)
	for _, specObj := range specObjects {
		specObj.translate = translate
		specObj.mountDir = mountDir
		err = specObj.validateSecretObject()
		if err != nil {
			return nil, err
		}

		// Group secrets of the same type together to allow batching requests
		objects = append(objects, specObj)

		// Check for duplicate names
		if names[specObj.ObjectName] {
			return nil, fmt.Errorf("Name already in use for objectName: %s", specObj.ObjectName)
		}
		names[specObj.ObjectName] = true

		if len(specObj.ObjectAlias) > 0 {
			if names[specObj.ObjectAlias] {
				return nil, fmt.Errorf("Name already in use for objectAlias: %s", specObj.ObjectAlias)
			}
			names[specObj.ObjectAlias] = true
		}

		if len(specObj.JMESPath) == 0 { //jmesPath not used. No more checks
			continue
		}
		klog.Infof("found jmes defined in spc %s", specObj.ObjectName)

		for _, JMESPathObject := range specObj.JMESPath {
			if names[JMESPathObject.ObjectAlias] {
				return nil, fmt.Errorf("Name already in use for objectAlias: %s", JMESPathObject.ObjectAlias)
			}

			names[JMESPathObject.ObjectAlias] = true
		}

	}
	return objects, nil
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
