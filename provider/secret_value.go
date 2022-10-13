package provider

import (
	"encoding/json"
	"fmt"
	"github.com/jmespath/go-jmespath"
)

type SecretValue struct {
	Value     []byte
	SecretObj SecretObject
}

func (sv *SecretValue) String() string { return "<REDACTED>" } // Do not log secrets

func (sv *SecretValue) getJsonSecrets() (s []*SecretValue, e error) {

	jsonValues := make([]*SecretValue, 0)
	if len(sv.SecretObj.JMESPath) == 0 {
		return jsonValues, nil
	}

	var data interface{}
	err := json.Unmarshal(sv.Value, &data)
	if err != nil {
		return nil, fmt.Errorf("Invalid JSON used with jmesPath in secret: %s.", sv.SecretObj.ObjectName)
	}
	//fetch all specified key value pairs`
	for _, jmesPathEntry := range sv.SecretObj.JMESPath {

		jsonSecret, err := jmespath.Search(jmesPathEntry.Path, data)

		if err != nil {
			return nil, fmt.Errorf("Invalid JMES Path: %s.", jmesPathEntry.Path)
		}

		if jsonSecret == nil {
			return nil, fmt.Errorf("JMES Path - %s for object alias - %s does not point to a valid object.",
				jmesPathEntry.Path, jmesPathEntry.ObjectAlias)
		}

		jsonSecretAsString, isString := jsonSecret.(string)
		if !isString {
			return nil, fmt.Errorf("Invalid JMES search result type for path:%s. Only string is allowed.", jmesPathEntry.Path)
		}

		secObj := sv.SecretObj.getJmesEntrySecretObject(&jmesPathEntry)

		secretValue := SecretValue{
			Value:     []byte(jsonSecretAsString),
			SecretObj: secObj,
		}
		jsonValues = append(jsonValues, &secretValue)

	}
	return jsonValues, nil
}
