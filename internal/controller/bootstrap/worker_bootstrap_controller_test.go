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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
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
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
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
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
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
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
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
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
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

func Test_getWindowsCommands(t *testing.T) {
	tests := []struct {
		name  string
		scope *Scope
		want  []string
	}{
		{
			name: "with default config",
			scope: &Scope{
				Config: &bootstrapv2.K0sWorkerConfig{
					Spec: bootstrapv2.K0sWorkerConfigSpec{
						Provisioner: bootstrapv2.ProvisionerSpec{
							Platform: bootstrapv2.PlatformWindows,
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

func workerConfigRef(name string) clusterv1.ContractVersionedObjectReference {
	return clusterv1.ContractVersionedObjectReference{
		APIGroup: bootstrapv2.GroupVersion.Group,
		Kind:     "K0sWorkerConfig",
		Name:     name,
	}
}

func Test_machineToWorkerBootstrapMapFunc(t *testing.T) {
	tests := []struct {
		name    string
		machine *clusterv1.Machine
		want    []ctrl.Request
	}{
		{
			name: "machine bootstrapped by a K0sWorkerConfig",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-1", Namespace: "default"},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{ConfigRef: workerConfigRef("worker-config-1")},
				},
			},
			want: []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: "default", Name: "worker-config-1"}}},
		},
		{
			name: "machine bootstrapped by a different kind is ignored",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-2", Namespace: "default"},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{ConfigRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: bootstrapv2.GroupVersion.Group,
						Kind:     "KubeadmConfig",
						Name:     "kubeadm-config-1",
					}},
				},
			},
			want: []ctrl.Request{},
		},
		{
			name: "machine with no bootstrap config ref is ignored",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{Name: "machine-3", Namespace: "default"},
			},
			want: []ctrl.Request{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, machineToWorkerBootstrapMapFunc(context.Background(), tt.machine))
		})
	}
}

func Test_clusterToWorkerBootstrapMapFunc(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1", Namespace: "default"},
	}

	machineForCluster := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-1",
			Namespace: "default",
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "cluster-1"},
		},
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{ConfigRef: workerConfigRef("worker-config-1")},
		},
	}

	machineForOtherCluster := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-2",
			Namespace: "default",
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "cluster-2"},
		},
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{ConfigRef: workerConfigRef("worker-config-2")},
		},
	}

	machineWithoutWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-3",
			Namespace: "default",
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "cluster-1"},
		},
	}

	r := &Controller{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(machineForCluster, machineForOtherCluster, machineWithoutWorkerConfig).
			Build(),
	}

	got := r.clusterToWorkerBootstrapMapFunc(context.Background(), cluster)
	require.Equal(t, []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: "default", Name: "worker-config-1"}}}, got)
}
