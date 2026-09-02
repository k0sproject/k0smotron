//go:build !envtest

/*


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

package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/secret"

	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
)

func Test_createCPInstallCmd(t *testing.T) {
	base := "k0s install controller --force --enable-dynamic-config "
	tests := []struct {
		name  string
		scope *ControllerScope
		want  string
	}{
		{
			name: "with default config",
			scope: &ControllerScope{
				Config: &bootstrapv2.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: bootstrapv2.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test-machine"},
				}}},
			},
			want: base + "--env AUTOPILOT_HOSTNAME=test",
		},
		{
			name: "with args",
			scope: &ControllerScope{
				Config: &bootstrapv2.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: bootstrapv2.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							Args: []string{"--enable-worker", "--labels=k0sproject.io/foo=bar"},
						},
					},
				},
				installArgs: []string{"--enable-worker", "--labels=k0sproject.io/foo=bar"},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test-machine"},
				}}},
				WorkerEnabled: true,
			},
			want: base + "--env AUTOPILOT_HOSTNAME=test --labels=k0smotron.io/machine-name=test-machine --enable-worker --labels=k0sproject.io/foo=bar --kubelet-extra-args=\"--hostname-override=test-machine\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, createCPInstallCmd(tt.scope))
		})
	}
}

func TestController_genK0sCommands(t *testing.T) {
	tests := []struct {
		scope      *ControllerScope
		installCmd string
		want       []string
	}{
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.0"),
				Config: &bootstrapv2.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv2.K0sControllerConfigSpec{
						Version: "v1.31.0",
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							DownloadURL: util.DefaultK0sDownloadURL,
						},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.0 sh",
				"(command -v systemctl > /dev/null 2>&1 && (cp /etc/k0s/k0sleave.service /etc/systemd/system/k0sleave.service && systemctl daemon-reload && systemctl enable k0sleave.service && systemctl start --no-block k0sleave.service) || true)",
				"(command -v rc-service > /dev/null 2>&1 && (cp /etc/k0s/k0sleave-openrc /etc/init.d/k0sleave && rc-update add k0sleave shutdown) || true)",
				"(command -v service > /dev/null 2>&1 && (cp /etc/k0s/k0sleave-sysv /etc/init.d/k0sleave && update-rc.d k0sleave defaults && service k0sleave start) || true)",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.6"),
				Config: &bootstrapv2.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv2.K0sControllerConfigSpec{
						Version: "v1.31.6",
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							DownloadURL: util.DefaultK0sDownloadURL,
						},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.6 sh",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
		{
			scope: &ControllerScope{
				currentKCPVersion: version.MustParse("v1.31.6+k0s.0"),
				Config: &bootstrapv2.K0sControllerConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					Spec: bootstrapv2.K0sControllerConfigSpec{
						Version: "v1.31.6+k0s.0",
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							DownloadURL: util.DefaultK0sDownloadURL,
						},
					},
				},
			},
			installCmd: "k0s install controller --force --enable-dynamic-config",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/usr/local/bin K0S_VERSION=v1.31.6+k0s.0 sh",
				"k0s install controller --force --enable-dynamic-config",
				"k0s start",
			},
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			c := &ControlPlaneController{}
			commands, _, err := c.genK0sCommands(tt.scope, tt.installCmd)
			require.NoError(t, err)
			require.Equal(t, tt.want, commands)
		})
	}
}

func TestControlPlaneController_detectJoinHost(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(serverURL.Port(), 10, 64)
	require.NoError(t, err)

	trustedCACert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	untrustedCACert := selfSignedCertPEM(t)

	scope := &ControllerScope{
		Cluster: &clusterv1.Cluster{
			Spec: clusterv1.ClusterSpec{
				ControlPlaneEndpoint: clusterv1.APIEndpoint{Host: serverURL.Hostname()},
			},
		},
		Config: &bootstrapv2.K0sControllerConfig{
			Spec: bootstrapv2.K0sControllerConfigSpec{
				K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
					K0s: &unstructured.Unstructured{Object: map[string]any{
						"spec": map[string]any{
							"api": map[string]any{
								"k0sApiPort": port,
							},
						},
					}},
				},
			},
		},
	}

	firstControllerMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "first-controller"},
		Status: clusterv1.MachineStatus{
			Addresses: clusterv1.MachineAddresses{
				{Type: clusterv1.MachineExternalIP, Address: "203.0.113.10"},
			},
		},
	}

	c := &ControlPlaneController{}

	t.Run("trusted CA reaches the control plane endpoint", func(t *testing.T) {
		ca := &secret.Certificate{KeyPair: &certs.KeyPair{Cert: trustedCACert}}

		host, err := c.detectJoinHost(context.Background(), scope, firstControllerMachine, ca)

		require.NoError(t, err)
		require.Equal(t, server.URL, host)
	})

	t.Run("CA that did not sign the endpoint falls back to the first controller", func(t *testing.T) {
		ca := &secret.Certificate{KeyPair: &certs.KeyPair{Cert: untrustedCACert}}

		host, err := c.detectJoinHost(context.Background(), scope, firstControllerMachine, ca)

		require.NoError(t, err)
		require.Equal(t, "https://203.0.113.10:"+serverURL.Port(), host)
	})

	t.Run("missing CA is an error", func(t *testing.T) {
		_, err := c.detectJoinHost(context.Background(), scope, firstControllerMachine, nil)

		require.Error(t, err)
	})
}

func selfSignedCertPEM(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "untrusted-test-ca"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
