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
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"testing"
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
				Config: &bootstrapv1.K0sWorkerConfig{},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
				}}},
			},
			want: base + ` --kubelet-extra-args="--hostname-override=test"`,
		},
		{
			name: "with args",
			scope: &Scope{
				Config: &bootstrapv1.K0sWorkerConfig{
					Spec: bootstrapv1.K0sWorkerConfigSpec{
						Args: []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test --hostname-override=test-from-arg"`,
		},
		{
			name: "with useSystemHostname set",
			scope: &Scope{
				Config: &bootstrapv1.K0sWorkerConfig{
					Spec: bootstrapv1.K0sWorkerConfigSpec{
						UseSystemHostname: true,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test-from-arg"`,
		},
		{
			name: "with extra args and useSystemHostname not set",
			scope: &Scope{
				Config: &bootstrapv1.K0sWorkerConfig{
					Spec: bootstrapv1.K0sWorkerConfigSpec{
						UseSystemHostname: false,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
				}}},
			},
			want: base + ` --debug --labels=k0sproject.io/foo=bar --kubelet-extra-args="--hostname-override=test --my-arg=value"`,
		},
		{
			name: "with extra args and useSystemHostname set",
			scope: &Scope{
				Config: &bootstrapv1.K0sWorkerConfig{
					Spec: bootstrapv1.K0sWorkerConfigSpec{
						UseSystemHostname: true,
						Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
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
