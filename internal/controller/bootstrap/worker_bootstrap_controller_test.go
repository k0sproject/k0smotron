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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
)

func Test_createInstallCmd(t *testing.T) {
	base := "k0s install worker --token-file /etc/k0s.token --labels=k0smotron.io/machine-name=test"
	tests := []struct {
		name  string
		scope *Scope
		want  string
	}{
		{
			name: "with default config",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test"},
				}}},
			},
			want: base + ` --kubelet-extra-args="--hostname-override=test"`,
		},
		{
			name: "with args",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						Args: []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test --hostname-override=test-from-arg"`,
		},
		{
			name: "with useSystemHostname set",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						UseSystemHostname: true,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test-from-arg"`,
		},
		{
			name: "with extra args and useSystemHostname not set",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						UseSystemHostname: false,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test --my-arg=value"`,
		},
		{
			name: "with extra args and useSystemHostname set",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						UseSystemHostname: true,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{
					"metadata": map[string]interface{}{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--my-arg=value"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, createInstallCmd(tt.scope))
		})
	}
}

func Test_createBootstrapSecret(t *testing.T) {
	tests := []struct {
		name                string
		scope               *Scope
		bootstrapData       []byte
		expectedLabels      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name: "with custom metadata",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "test-ns",
						UID:       "test-uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "K0sWorkerConfig",
					},
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						SecretMetadata: &bootstrapv2.SecretMetadata{
							Labels: map[string]string{
								"custom-label": "foo",
							},
							Annotations: map[string]string{
								"custom-anno": "bar",
							},
						},
					},
				},
				Cluster: &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster",
					},
				},
			},
			bootstrapData: []byte("test-bootstrap-data"),
			expectedLabels: map[string]string{
				"custom-label":             "foo",
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			expectedAnnotations: map[string]string{
				"custom-anno": "bar",
			},
		},
		{
			name: "without custom metadata",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "test-ns",
						UID:       "test-uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "K0sWorkerConfig",
					},
				},
				Cluster: &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster",
					},
				},
			},
			bootstrapData: []byte("test-bootstrap-data"),
			expectedLabels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "with nil secret metadata",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "test-ns",
						UID:       "test-uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "K0sWorkerConfig",
					},
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						SecretMetadata: nil,
					},
				},
				Cluster: &clusterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster",
					},
				},
			},
			bootstrapData: []byte("test-bootstrap-data"),
			expectedLabels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
			expectedAnnotations: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := createBootstrapSecret(tt.scope, tt.bootstrapData, "cloud-config")

			// Check basic properties
			require.Equal(t, tt.scope.Config.Name, secret.Name)
			require.Equal(t, tt.scope.Config.Namespace, secret.Namespace)
			require.Equal(t, clusterv1.ClusterSecretType, secret.Type)
			require.Equal(t, tt.bootstrapData, secret.Data["value"])
			require.Equal(t, "cloud-config", string(secret.Data["format"]))

			// Check labels
			require.Equal(t, tt.expectedLabels, secret.Labels)

			// Check annotations
			require.Equal(t, tt.expectedAnnotations, secret.Annotations)

			// Check owner references
			require.Len(t, secret.OwnerReferences, 1)
			require.Equal(t, bootstrapv2.GroupVersion.String(), secret.OwnerReferences[0].APIVersion)
			require.Equal(t, tt.scope.Config.Kind, secret.OwnerReferences[0].Kind)
			require.Equal(t, tt.scope.Config.Name, secret.OwnerReferences[0].Name)
			require.Equal(t, tt.scope.Config.UID, secret.OwnerReferences[0].UID)
			require.True(t, *secret.OwnerReferences[0].Controller)
		})
	}
}
