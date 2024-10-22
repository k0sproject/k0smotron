/*
Copyright 2024.

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

package controlplane

import (
	"testing"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/collections"
)

func Test_computeStatus(t *testing.T) {
	t.Run("test no machines", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 3,
			},
		}
		machines := collections.Machines{}
		computeStatus(machines, kcp)

		require.Zero(t, kcp.Status.Replicas)
		require.Empty(t, kcp.Status.Version)
	})

	t.Run("test all machines are ready", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.31.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
		}

		computeStatus(machines, kcp)

		require.Equal(t, int32(2), kcp.Status.Replicas)
		require.Equal(t, int32(0), kcp.Status.UnavailableReplicas)
		require.Equal(t, int32(1), kcp.Status.UpdatedReplicas)
		require.Equal(t, int32(2), kcp.Status.ReadyReplicas)
		require.Equal(t, "v1.30.0", kcp.Status.Version)
	})

	t.Run("test all machines are ready but not using suffix", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0+k0s.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.31.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
		}

		computeStatus(machines, kcp)

		require.Equal(t, int32(2), kcp.Status.Replicas)
		require.Equal(t, int32(0), kcp.Status.UnavailableReplicas)
		require.Equal(t, int32(1), kcp.Status.UpdatedReplicas)
		require.Equal(t, int32(2), kcp.Status.ReadyReplicas)
		require.Equal(t, "v1.30.0+k0s.0", kcp.Status.Version)
	})

	t.Run("test some machines are not ready", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.31.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseProvisioning),
				},
			},
			"machine3": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseFailed),
				},
			},
		}

		computeStatus(machines, kcp)

		require.Equal(t, int32(3), kcp.Status.Replicas)
		require.Equal(t, int32(2), kcp.Status.UnavailableReplicas)
		require.Equal(t, int32(1), kcp.Status.UpdatedReplicas)
		require.Equal(t, int32(1), kcp.Status.ReadyReplicas)
		require.Equal(t, "v1.30.0", kcp.Status.Version)
	})

	t.Run("machines provisioned but kcp not using --enable-worker", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.31.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseProvisioned),
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseProvisioning),
				},
			},
			"machine3": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseFailed),
				},
			},
		}

		computeStatus(machines, kcp)
		require.Equal(t, int32(3), kcp.Status.Replicas)
		require.Equal(t, int32(2), kcp.Status.UnavailableReplicas)
		require.Equal(t, int32(1), kcp.Status.UpdatedReplicas)
		require.Equal(t, int32(1), kcp.Status.ReadyReplicas)
		require.Equal(t, "v1.30.0", kcp.Status.Version)

	})

	t.Run("some machines stuck as provisioned but kcp using --enable-worker", func(t *testing.T) {
		kcp := &cpv1beta1.K0sControlPlane{
			Spec: cpv1beta1.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
				K0sConfigSpec: bootstrapv1.K0sConfigSpec{
					Args: []string{"--enable-worker"},
				},
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.31.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseProvisioned),
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: ptr.To[string]("v1.30.0"),
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
		}

		computeStatus(machines, kcp)
		require.Equal(t, int32(2), kcp.Status.Replicas)
		require.Equal(t, int32(1), kcp.Status.UnavailableReplicas)
		require.Equal(t, int32(1), kcp.Status.UpdatedReplicas)
		require.Equal(t, int32(1), kcp.Status.ReadyReplicas)
		require.Equal(t, "v1.30.0", kcp.Status.Version)

	})
}

func Test_versionMatches(t *testing.T) {
	type args struct {
		machine *clusterv1.Machine
		ver     string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "version matches, both without suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To("v1.31.0"),
					},
				},
				ver: "v1.31.0",
			},
			want: true,
		},
		{
			name: "version does not match",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To("v1.31.0"),
					},
				},
				ver: "v1.30.0",
			},
			want: false,
		},
		{
			name: "semver version match, machine version is missing the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To("v1.31.0"),
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: true,
		},
		{
			name: "semver version match, kcp version is missing the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To("v1.31.0+k0s.0"),
					},
				},
				ver: "v1.31.0",
			},
			want: true,
		},
		{
			name: "versions match, both with the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To("v1.31.0+k0s.0"),
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: true,
		},
		{
			name: "versions do not match, machine version is missing",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: nil,
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: false,
		},
		{
			name: "versions do not match, machine version is empty",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: ptr.To(""),
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionMatches(tt.args.machine, tt.args.ver); got != tt.want {
				t.Errorf("versionMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
