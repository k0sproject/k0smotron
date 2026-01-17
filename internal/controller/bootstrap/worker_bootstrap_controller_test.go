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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func Test_getWindowsCommands(t *testing.T) {
	tests := []struct {
		name  string
		scope *Scope
		want  []string
	}{
		{
			name: "with default config",
			scope: &Scope{
				Config: &bootstrapv1.K0sWorkerConfig{
					Spec: bootstrapv1.K0sWorkerConfigSpec{
						Platform: bootstrapv1.PlatformWindows,
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]interface{}{}}},
			},
			want: []string{
				"powershell.exe -NoProfile -NonInteractive -File \"C:\\bootstrap\\k0s_install.ps1\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := getWindowsCommands(tt.scope)
			require.Equal(t, tt.want, got)
		})
	}

}
