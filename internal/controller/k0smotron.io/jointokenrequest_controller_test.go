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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kutil "github.com/k0sproject/k0smotron/v2/internal/util"
)

func TestUpdateJoinTokenURL(t *testing.T) {
	token, err := kutil.CreateK0sJoinToken([]byte("ca"), "abcdef.0123456789abcdef", "https://kmc-wl1.default.svc:30443", "kubelet-bootstrap")
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		apiHost string
		want    string
	}{
		{name: "hostname", apiHost: "api.example.com", want: "https://api.example.com:443"},
		{name: "IPv4", apiHost: "192.0.2.10", want: "https://192.0.2.10:443"},
		{name: "IPv6", apiHost: "2001:db8::10", want: "https://[2001:db8::10]:443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kmc := km.Cluster{
				Spec: km.ClusterSpec{
					Ingress: &km.IngressSpec{APIHost: tc.apiHost, Port: 443},
				},
			}

			updated, err := updateJoinTokenURL(token, kmc)
			require.NoError(t, err)

			kubeconfig, err := tokenDecode(updated)
			require.NoError(t, err)
			cfg, err := clientcmd.Load(kubeconfig)
			require.NoError(t, err)
			require.Len(t, cfg.Clusters, 1)
			for _, cluster := range cfg.Clusters {
				require.Equal(t, tc.want, cluster.Server)
			}
		})
	}
}
