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

	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
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
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "test"},
				}}},
			},
			want: base + ` --kubelet-extra-args="--hostname-override=test"`,
		},
		{
			name: "with args",
			scope: &Scope{
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{
						Spec: bootstrapv2.K0sWorkerConfigSpec{
							Args: []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
						},
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
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{
						Spec: bootstrapv2.K0sWorkerConfigSpec{
							UseSystemHostname: true,
							Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--hostname-override=test-from-arg"`},
						},
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
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{
						Spec: bootstrapv2.K0sWorkerConfigSpec{
							UseSystemHostname: false,
							Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
						},
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
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{
						Spec: bootstrapv2.K0sWorkerConfigSpec{
							UseSystemHostname: true,
							Args:              []string{"--debug", "--labels=k0sproject.io/foo=bar", `--kubelet-extra-args="--my-arg=value"`},
						},
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

func Test_getWindowsCommands(t *testing.T) {
	tests := []struct {
		name  string
		scope *Scope
		want  []string
	}{
		{
			name: "with default config",
			scope: &Scope{
				Config: &k0sWorkerConfig{
					K0sWorkerConfig: &bootstrapv2.K0sWorkerConfig{
						Spec: bootstrapv2.K0sWorkerConfigSpec{
							Provisioner: bootstrapv2.ProvisionerSpec{
								Platform: bootstrapv2.PlatformWindows,
							},
						},
					},
				},
				ConfigOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{}}},
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

func Test_resolveK0sWorkerVersion(t *testing.T) {
	machineOwner := func(version string) *bsutil.ConfigOwner {
		obj := map[string]any{"kind": "Machine", "spec": map[string]any{}}
		if version != "" {
			obj["spec"] = map[string]any{"version": version}
		}
		return &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: obj}}
	}

	tests := []struct {
		name          string
		configVersion string
		configOwner   *bsutil.ConfigOwner
		want          string
	}{
		{
			name:        "no version anywhere",
			configOwner: machineOwner(""),
			want:        "",
		},
		{
			name:          "config version takes precedence over owner version",
			configVersion: "v1.30.0+k0s.1",
			configOwner:   machineOwner("v1.31.0+k0s.0"),
			want:          "v1.30.0+k0s.1",
		},
		{
			name:          "config version without k0s suffix gets the default suffix",
			configVersion: "v1.30.0",
			configOwner:   machineOwner(""),
			want:          "v1.30.0+k0s.0",
		},
		{
			name:        "falls back to machine version",
			configOwner: machineOwner("v1.31.0"),
			want:        "v1.31.0+k0s.0",
		},
		{
			name:        "machine version with '-k0s.' is normalized",
			configOwner: machineOwner("v1.31.0-k0s.2"),
			want:        "v1.31.0+k0s.2",
		},
		{
			name: "falls back to machine pool template version",
			configOwner: &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: map[string]any{
				"kind": "MachinePool",
				"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"version": "v1.32.1+k0s.0"}}},
			}}},
			want: "v1.32.1+k0s.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &bootstrapv2.K0sWorkerConfig{
				Spec: bootstrapv2.K0sWorkerConfigSpec{Version: tt.configVersion},
			}
			require.Equal(t, tt.want, resolveK0sWorkerVersion(config, tt.configOwner))
			// The deprecated field must not be mutated, as the config gets patched back to the API.
			require.Equal(t, tt.configVersion, config.Spec.Version)
		})
	}
}
