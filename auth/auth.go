package auth

import (
	"errors"
	"github.com/aliyun/credentials-go/credentials"
	"k8s.io/klog/v2"
	"os"
	"strconv"
)

const (
	ProviderName       = "secrets-store-csi-driver-provider-alibabacloud"
	RamRoleARNAuthType = "ram_role_arn"
	AKAuthType         = "access_key"
	EcsRamRoleAuthType = "ecs_ram_role"
	OidcAuthType       = "oidc_role_arn"
	roleSessionName    = "csi-secrets-store-provider-alibaba"
	oidcTokenFilePath  = "/var/run/secrets/tokens/csi-secrets-store-provider-alibabacloud"
)

type getCredential interface {
	NewCredential() (credentials.Credential, error)
}

type chainedCred interface {
	getCredential
	authNext(chainedCred) chainedCred
}

type chainedAuth struct {
	cred getCredential
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
	config := new(credentials.Config).
		SetType(OidcAuthType).
		SetOIDCProviderArn(c.oidcArn).
		SetOIDCTokenFilePath(oidcTokenFilePath).
		SetRoleArn(c.roleArn).
		SetRoleSessionName(roleSessionName)
	cred, err := credentials.NewCredential(config)
	if cred != nil {
		klog.Info("Using oidc rrsa auth..", "roleArn", c.roleArn, "oidcArn", c.oidcArn)
	}
	return cred, err
}

type ramRoleAuth struct{ *authConfig }

func (c *ramRoleAuth) NewCredential() (credentials.Credential, error) {
	//check if ram_role_arn auth type
	if c.accessKey == "" || c.accessSecretKey == "" || c.roleArn == "" {
		return nil, nil
	}
	config := new(credentials.Config).
		SetType(RamRoleARNAuthType).
		SetAccessKeyId(c.accessKey).
		SetAccessKeySecret(c.accessSecretKey).
		SetRoleArn(c.roleArn).
		SetRoleSessionName(roleSessionName)
	if c.roleSessionExpiration != "" {
		rseInt, err := strconv.Atoi(c.roleSessionExpiration)
		if err != nil {
			klog.Error(err, "failed to parse given roleSessionExpiration", "value", c.roleSessionExpiration)
		} else {
			config.SetRoleSessionExpiration(rseInt)
		}
	}
	cred, err := credentials.NewCredential(config)
	if cred != nil {
		klog.Info("Using ram role arn auth..", "roleArn", c.roleArn, "roleSessionName", c.roleSessionName)
	}
	return cred, err
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
	if cred != nil {
		klog.Info("Using ak/sk auth..")
	}
	return cred, err
}

type ecsRoleAuth struct{ *authConfig }

func (c *ecsRoleAuth) NewCredential() (credentials.Credential, error) {
	//use ecs ramrole auth type default if no auth config given
	config := new(credentials.Config).
		SetType(EcsRamRoleAuthType)
	cred, err := credentials.NewCredential(config)
	if cred != nil {
		klog.Info("Using ecs ram role auth..")
	}
	return cred, err
}

type authConfig struct {
	roleArn               string
	oidcArn               string
	accessKey             string
	accessSecretKey       string
	roleSessionName       string
	roleSessionExpiration string
}

func GetKMSAuthCred() (credentials.Credential, error) {
	aConfig := &authConfig{
		roleArn:               os.Getenv("ALICLOUD_ROLE_ARN"),
		oidcArn:               os.Getenv("ALICLOUD_OIDC_PROVIDER_ARN"),
		accessKey:             os.Getenv("ACCESS_KEY_ID"),
		accessSecretKey:       os.Getenv("SECRET_ACCESS_KEY"),
		roleSessionName:       os.Getenv("ALICLOUD_ROLE_SESSION_NAME"),
		roleSessionExpiration: os.Getenv("ALICLOUD_ROLE_SESSION_EXPIRATION"),
	}
	root := chainedAuth{cred: &oidcRoleAuth{authConfig: aConfig}}
	root.authNext(&chainedAuth{cred: &ramRoleAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{cred: &akAuth{authConfig: aConfig}}).
		authNext(&chainedAuth{cred: &ecsRoleAuth{authConfig: aConfig}})
	return root.NewCredential()
}
