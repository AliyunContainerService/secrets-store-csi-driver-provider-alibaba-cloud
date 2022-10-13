package server

import (
	"context"
	"testing"

	"sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"
)

func TestMount(t *testing.T) {
	cases := []struct {
		desc         string
		mountRequest *v1alpha1.MountRequest
		expectedErr  bool
	}{
		{
			desc:         "failed to unmarshal attributes",
			mountRequest: &v1alpha1.MountRequest{},
			expectedErr:  true,
		},
		{
			desc: "failed to unmarshal secrets",
			mountRequest: &v1alpha1.MountRequest{
				Attributes: `{"keyvaultName":"kv"}`,
			},
			expectedErr: true,
		},
		{
			desc: "failed to unmarshal file permission",
			mountRequest: &v1alpha1.MountRequest{
				Attributes: `{"keyvaultName":"kv"}`,
				Secrets:    `{"clientid":"foo","clientsecret":"bar"}`,
			},
			expectedErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			testServer := &CSIDriverProviderServer{}
			_, err := testServer.Mount(context.TODO(), tc.mountRequest)
			if tc.expectedErr && err == nil || !tc.expectedErr && err != nil {
				t.Fatalf("expected error: %v, got error: %v", tc.expectedErr, err)
			}
		})
	}
}
