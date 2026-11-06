//go:build !envtest

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k0smotronio

import (
	"bytes"
	"testing"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

const joinTokenSampleKubeconfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0FjZXJ0
    server: https://old-host:6443
  name: k0s
contexts:
- context:
    cluster: k0s
    user: admin
  name: k0s
current-context: k0s
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate-data: Q0xJRU5UQ0VSVA==
    client-key-data: Q0xJRU5US0VZ
`

func Test_updateJoinTokenURL(t *testing.T) {
	tests := []struct {
		name       string
		apiHost    string
		port       int64
		wantServer string
	}{
		{
			name:       "ipv4 host",
			apiHost:    "10.0.0.1",
			port:       6443,
			wantServer: "https://10.0.0.1:6443",
		},
		{
			name:       "ipv6 host is bracketed",
			apiHost:    "2001:db8:11:1103::3",
			port:       443,
			wantServer: "https://[2001:db8:11:1103::3]:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := tokenEncode(bytes.NewReader([]byte(joinTokenSampleKubeconfig)))
			require.NoError(t, err)

			kmc := km.Cluster{
				Spec: km.ClusterSpec{
					Ingress: &km.IngressSpec{
						APIHost: tt.apiHost,
						Port:    tt.port,
					},
				},
			}

			updatedToken, err := updateJoinTokenURL(token, kmc)
			require.NoError(t, err)

			decoded, err := tokenDecode(updatedToken)
			require.NoError(t, err)

			cfg, err := clientcmd.Load(decoded)
			require.NoError(t, err)

			for _, cluster := range cfg.Clusters {
				require.Equal(t, tt.wantServer, cluster.Server)
			}
		})
	}
}
