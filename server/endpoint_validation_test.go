package server

import (
	"testing"
)

func TestValidateKmsEndpoint(t *testing.T) {
	cases := []struct {
		desc     string
		endpoint string
		wantErr  bool
	}{
		// Valid endpoints
		{
			desc:     "VPC shared gateway",
			endpoint: "kms-vpc.cn-hangzhou.aliyuncs.com",
			wantErr:  false,
		},
		{
			desc:     "public shared gateway",
			endpoint: "kms.cn-hangzhou.aliyuncs.com",
			wantErr:  false,
		},
		{
			desc:     "dedicated gateway with instance id",
			endpoint: "kst-hbb3xxx.cryptoservice.kms.aliyuncs.com",
			wantErr:  false,
		},
		{
			desc:     "VPC gateway with port",
			endpoint: "kms-vpc.cn-hangzhou.aliyuncs.com:443",
			wantErr:  true, // SDK uses raw value in signature; port causes IncompleteSignature
		},
		{
			desc:     "public gateway with port",
			endpoint: "kms.cn-hangzhou.aliyuncs.com:443",
			wantErr:  true, // SDK uses raw value in signature; port causes IncompleteSignature
		},
		{
			desc:     "https scheme with VPC gateway",
			endpoint: "https://kms-vpc.cn-hangzhou.aliyuncs.com",
			wantErr:  true, // SDK prepends https:// itself; causes double-prefix DNS error
		},
		{
			desc:     "https scheme with public gateway and port",
			endpoint: "https://kms.cn-hangzhou.aliyuncs.com:443",
			wantErr:  true, // scheme + port both rejected
		},
		{
			desc:     "uppercase VPC gateway",
			endpoint: "KMS-VPC.cn-hangzhou.aliyuncs.com",
			wantErr:  true, // SDK uses original case in signing; causes IncompleteSignature
		},
		{
			desc:     "mixed case public gateway",
			endpoint: "Kms.Cn-Hangzhou.Aliyuncs.com",
			wantErr:  true, // must be all lowercase
		},
		{
			desc:     "VPC gateway us-west-1",
			endpoint: "kms-vpc.us-west-1.aliyuncs.com",
			wantErr:  false,
		},
		{
			desc:     "VPC gateway ap-southeast-1",
			endpoint: "kms-vpc.ap-southeast-1.aliyuncs.com",
			wantErr:  false,
		},
		{
			desc:     "dedicated gateway finance region",
			endpoint: "kst-1234abcd.cryptoservice.kms.aliyuncs.com",
			wantErr:  false,
		},

		// Invalid: SSRF attacks
		{
			desc:     "attacker controlled domain",
			endpoint: "evil.attacker.com",
			wantErr:  true,
		},
		{
			desc:     "attacker domain with aliyuncs suffix in path",
			endpoint: "evil.attacker.com/aliyuncs.com",
			wantErr:  true,
		},
		{
			desc:     "non-KMS aliyuncs subdomain",
			endpoint: "evil.aliyuncs.com",
			wantErr:  true, // ends with .aliyuncs.com but doesn't match kms/kms-vpc/cryptoservice pattern
		},
		{
			desc:     "http scheme rejected",
			endpoint: "http://kms-vpc.cn-hangzhou.aliyuncs.com",
			wantErr:  true,
		},
		{
			desc:     "attacker domain with http",
			endpoint: "http://evil.attacker.com",
			wantErr:  true,
		},

		// Invalid: IP literals (SSRF to metadata)
		{
			desc:     "AWS metadata IP",
			endpoint: "169.254.169.254",
			wantErr:  true,
		},
		{
			desc:     "Alibaba metadata IP",
			endpoint: "100.100.100.200",
			wantErr:  true,
		},
		{
			desc:     "arbitrary IP address",
			endpoint: "10.0.0.1",
			wantErr:  true,
		},
		{
			desc:     "IPv6 loopback",
			endpoint: "::1",
			wantErr:  true,
		},
		{
			desc:     "IPv6 link-local (metadata)",
			endpoint: "fd00:ec2::254",
			wantErr:  true,
		},
		{
			desc:     "IP with port",
			endpoint: "169.254.169.254:8080",
			wantErr:  true,
		},

		// Invalid: metadata hostnames
		{
			desc:     "GCP metadata hostname",
			endpoint: "metadata.google.internal",
			wantErr:  true,
		},
		{
			desc:     "AWS metadata hostname",
			endpoint: "metadata.aws.internal",
			wantErr:  true,
		},

		// Invalid: non-aliyuncs domains
		{
			desc:     "localhost",
			endpoint: "localhost",
			wantErr:  true,
		},
		{
			desc:     "internal service name",
			endpoint: "kms-service.default.svc.cluster.local",
			wantErr:  true,
		},
		{
			desc:     "attacker with kms prefix but wrong domain",
			endpoint: "kms-vpc.evil.com",
			wantErr:  true,
		},
		{
			desc:     "attacker with cryptoservice but wrong domain",
			endpoint: "kst-xxx.cryptoservice.kms.evil.com",
			wantErr:  true,
		},

		// Invalid: aliyuncs.com but wrong KMS pattern
		{
			desc:     "ECS endpoint (not KMS)",
			endpoint: "ecs.cn-hangzhou.aliyuncs.com",
			wantErr:  true,
		},
		{
			desc:     "RDS endpoint (not KMS)",
			endpoint: "rds.cn-hangzhou.aliyuncs.com",
			wantErr:  true,
		},
		{
			desc:     "OSS endpoint (not KMS)",
			endpoint: "oss-cn-hangzhou.aliyuncs.com",
			wantErr:  true,
		},

		// Invalid: malformed inputs
		{
			desc:     "whitespace only",
			endpoint: "   ",
			wantErr:  true,
		},
		{
			desc:     "https with empty host",
			endpoint: "https://",
			wantErr:  true,
		},
		{
			desc:     "just a port",
			endpoint: ":443",
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateKmsEndpoint(tc.endpoint)
			if tc.wantErr && err == nil {
				t.Errorf("validateKmsEndpoint(%q) expected error, got nil", tc.endpoint)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateKmsEndpoint(%q) expected no error, got: %v", tc.endpoint, err)
			}
		})
	}
}
