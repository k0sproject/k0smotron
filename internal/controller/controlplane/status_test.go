//go:build !envtest

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
)

func Test_machineStatusCompute(t *testing.T) {
	t.Run("test no machines", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 3,
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: collections.Machines{},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Zero(t, ptr.Deref(kcp.Status.Replicas, 0))
		require.Empty(t, kcp.Status.Version)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
	})

	t.Run("test all machines are ready and available", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
			},
		}
		activeMachines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: activeMachines,
			upToDateMachines: collections.Machines{
				"machine1": activeMachines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(2), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
		require.Equal(t, "v1.30.0", kcp.Status.Version)
	})

	t.Run("test all ready and available are ready but not using suffix", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0+k0s.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: machines,
			upToDateMachines: collections.Machines{
				"machine1": machines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(2), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
		require.Equal(t, "v1.30.0+k0s.0", kcp.Status.Version)
	})

	t.Run("test non existent machines are unavailable and external managed is true", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 3,
				K0sConfigSpec: bootstrapv2.K0sConfigSpec{
					Args: []string{"--enable-worker"},
				},
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: machines,
			upToDateMachines: collections.Machines{
				"machine1": machines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(1), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.Nil(t, kcp.Status.ExternalManagedControlPlane)
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
						Version: "v1.31.0",
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
						Version: "v1.31.0",
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
						Version: "v1.31.0",
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
						Version: "v1.31.0+k0s.0",
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
						Version: "v1.31.0+k0s.0",
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
						Version: "",
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
						Version: "",
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
