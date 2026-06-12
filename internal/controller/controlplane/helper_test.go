//go:build !envtest

/*
Copyright 2023.

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
	"encoding/json"
	"testing"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestHasControllerConfigChanged(t *testing.T) {
	var testCases = []struct {
		name             string
		machine          *clusterv1.Machine
		kcp              *cpv1beta2.K0sControlPlane
		bootstrapConfigs map[string]bootstrapv2.K0sControllerConfig
		configHasChanged bool
	}{
		{
			name: "equal configs",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{
				Spec: cpv1beta2.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv2.K0sConfigSpec{
						K0s: &unstructured.Unstructured{
							Object: map[string]any{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]any{
									"name": "k0s",
								},
								"spec": map[string]any{
									"api": map[string]any{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]any{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]any{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]any{
										"enabled": "false",
									},
									"storage": map[string]any{
										"etcd": map[string]any{
											"extraArgs": map[string]any{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta2.K0sControlPlaneStatus{
					Initialization: cpv1beta2.Initialization{
						ControlPlaneInitialized: new(true),
					},
				},
			},
			bootstrapConfigs: map[string]bootstrapv2.K0sControllerConfig{
				"test": {
					Spec: bootstrapv2.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							K0s: &unstructured.Unstructured{
								Object: map[string]any{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]any{
										"name": "k0s",
									},
									"spec": map[string]any{
										"api": map[string]any{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]any{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]any{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]any{
											"enabled": "false",
										},
										"storage": map[string]any{
											"etcd": map[string]any{
												"extraArgs": map[string]any{
													"name": "test",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			configHasChanged: false,
		},
		{
			name: "not equal configs: new k0sInstallDir",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{
				Spec: cpv1beta2.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv2.K0sConfigSpec{
						K0sInstallDir: "/opt",
						K0s: &unstructured.Unstructured{
							Object: map[string]any{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]any{
									"name": "k0s",
								},
								"spec": map[string]any{
									"api": map[string]any{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]any{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]any{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]any{
										"enabled": "false",
									},
									"storage": map[string]any{
										"etcd": map[string]any{
											"extraArgs": map[string]any{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta2.K0sControlPlaneStatus{
					Initialization: cpv1beta2.Initialization{
						ControlPlaneInitialized: new(true),
					},
				},
			},
			bootstrapConfigs: map[string]bootstrapv2.K0sControllerConfig{
				"test": {
					Spec: bootstrapv2.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							K0sInstallDir: "/usr/local/bin",
							K0s: &unstructured.Unstructured{
								Object: map[string]any{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]any{
										"name": "k0s",
									},
									"spec": map[string]any{
										"api": map[string]any{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]any{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]any{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]any{
											"enabled": "false",
										},
										"storage": map[string]any{
											"etcd": map[string]any{
												"extraArgs": map[string]any{
													"name": "test",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			configHasChanged: true,
		},
		{
			name: "not equal configs: enabling worker in kcp",
			machine: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv1.MachineStatus{
					Phase: string(clusterv1.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{
				Spec: cpv1beta2.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv2.K0sConfigSpec{
						Args: []string{
							"--enable-worker",
						},
						K0s: &unstructured.Unstructured{
							Object: map[string]any{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]any{
									"name": "k0s",
								},
								"spec": map[string]any{
									"api": map[string]any{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]any{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]any{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]any{
										"enabled": "false",
									},
									"storage": map[string]any{
										"etcd": map[string]any{
											"extraArgs": map[string]any{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta2.K0sControlPlaneStatus{
					Initialization: cpv1beta2.Initialization{
						ControlPlaneInitialized: new(true),
					},
				},
			},
			bootstrapConfigs: map[string]bootstrapv2.K0sControllerConfig{
				"test": {
					Spec: bootstrapv2.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
							K0s: &unstructured.Unstructured{
								Object: map[string]any{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]any{
										"name": "k0s",
									},
									"spec": map[string]any{
										"api": map[string]any{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]any{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]any{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]any{
											"enabled": "false",
										},
										"storage": map[string]any{
											"etcd": map[string]any{
												"extraArgs": map[string]any{
													"name": "test",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			configHasChanged: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.configHasChanged, hasControllerConfigChanged(tc.bootstrapConfigs, tc.kcp, tc.machine))
		})
	}
}

// TestGetMachineK0sConfig_LegacyAnnotation ensures that a machine carrying a v1beta1-formatted
// k0s config annotation (written by k0smotron < v2.0.0) is converted to v1beta2 on read, so that
// the comparison in hasControllerConfigChanged does not report a spurious change and trigger a
// control plane rollout after upgrading k0smotron. See https://github.com/k0sproject/k0smotron/issues/1478
func TestGetMachineK0sConfig_LegacyAnnotation(t *testing.T) {
	// Serialize a K0sConfigSpec the way old k0smotron versions did: using the v1beta1 schema,
	// which has no "provisioner" field and uses preStartCommands/postStartCommands.
	legacySpec := bootstrapv1.K0sConfigSpec{
		K0sInstallDir:     "/usr/local/bin",
		DownloadURL:       "https://get.k0s.sh",
		PreStartCommands:  []string{"echo pre"},
		PostStartCommands: []string{"echo post"},
		Args:              []string{"--enable-worker"},
	}
	legacyJSON, err := json.Marshal(legacySpec)
	require.NoError(t, err)
	require.NotContains(t, string(legacyJSON), "provisioner")
	require.Contains(t, string(legacyJSON), "preStartCommands")

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: string(legacyJSON),
			},
		},
	}

	got, err := getMachineK0sConfig(machine)
	require.NoError(t, err)

	// The provisioner must be defaulted exactly as the API server defaults the K0sControlPlane
	// spec it is compared against, otherwise the diff is non-empty and the machine is recreated.
	require.Equal(t, provisioner.CloudInitProvisioningFormat, got.Provisioner.Type)
	require.Equal(t, bootstrapv2.PlatformLinux, got.Provisioner.Platform)
	// Renamed command fields must be carried over.
	require.Equal(t, []string{"echo pre"}, got.PreK0sCommands)
	require.Equal(t, []string{"echo post"}, got.PostK0sCommands)
}
