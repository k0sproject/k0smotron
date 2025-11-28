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
	"testing"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestHasControllerConfigChanged(t *testing.T) {
	var testCases = []struct {
		name             string
		machine          *clusterv2.Machine
		kcp              *cpv1beta1.K0sControlPlane
		bootstrapConfigs map[string]bootstrapv1.K0sControllerConfig
		configHasChanged bool
	}{
		{
			name: "equal configs",
			machine: &clusterv2.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv2.MachineStatus{
					Phase: string(clusterv2.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta1.K0sControlPlane{
				Spec: cpv1beta1.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1.K0sConfigSpec{
						K0s: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]interface{}{
									"name": "k0s",
								},
								"spec": map[string]interface{}{
									"api": map[string]interface{}{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]interface{}{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]interface{}{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]interface{}{
										"enabled": "false",
									},
									"storage": map[string]interface{}{
										"etcd": map[string]interface{}{
											"extraArgs": map[string]interface{}{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta1.K0sControlPlaneStatus{
					Ready: true,
				},
			},
			bootstrapConfigs: map[string]bootstrapv1.K0sControllerConfig{
				"test": {
					Spec: bootstrapv1.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{
							K0s: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]interface{}{
										"name": "k0s",
									},
									"spec": map[string]interface{}{
										"api": map[string]interface{}{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]interface{}{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]interface{}{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]interface{}{
											"enabled": "false",
										},
										"storage": map[string]interface{}{
											"etcd": map[string]interface{}{
												"extraArgs": map[string]interface{}{
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
			machine: &clusterv2.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv2.MachineStatus{
					Phase: string(clusterv2.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta1.K0sControlPlane{
				Spec: cpv1beta1.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1.K0sConfigSpec{
						K0sInstallDir: "/opt",
						K0s: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]interface{}{
									"name": "k0s",
								},
								"spec": map[string]interface{}{
									"api": map[string]interface{}{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]interface{}{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]interface{}{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]interface{}{
										"enabled": "false",
									},
									"storage": map[string]interface{}{
										"etcd": map[string]interface{}{
											"extraArgs": map[string]interface{}{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta1.K0sControlPlaneStatus{
					Ready: true,
				},
			},
			bootstrapConfigs: map[string]bootstrapv1.K0sControllerConfig{
				"test": {
					Spec: bootstrapv1.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{
							K0sInstallDir: "/usr/local/bin",
							K0s: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]interface{}{
										"name": "k0s",
									},
									"spec": map[string]interface{}{
										"api": map[string]interface{}{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]interface{}{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]interface{}{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]interface{}{
											"enabled": "false",
										},
										"storage": map[string]interface{}{
											"etcd": map[string]interface{}{
												"extraArgs": map[string]interface{}{
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
			machine: &clusterv2.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: clusterv2.MachineStatus{
					Phase: string(clusterv2.MachinePhaseRunning),
				},
			},
			kcp: &cpv1beta1.K0sControlPlane{
				Spec: cpv1beta1.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1.K0sConfigSpec{
						Args: []string{
							"--enable-worker",
						},
						K0s: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "k0s.k0sproject.io/v1beta1",
								"kind":       "ClusterConfig",
								"metadata": map[string]interface{}{
									"name": "k0s",
								},
								"spec": map[string]interface{}{
									"api": map[string]interface{}{
										"externalAddress": "172.18.0.3",
										"extraArgs": map[string]interface{}{
											"anonymous-auth": "true",
										},
									},
									"network": map[string]interface{}{
										"clusterDomain": "cluster.local",
										"podCIDR":       "192.168.0.0/16",
										"serviceCIDR":   "10.128.0.0/12",
									},
									"telemetry": map[string]interface{}{
										"enabled": "false",
									},
									"storage": map[string]interface{}{
										"etcd": map[string]interface{}{
											"extraArgs": map[string]interface{}{
												"name": "test",
											},
										},
									},
								},
							},
						},
					},
				},
				Status: cpv1beta1.K0sControlPlaneStatus{
					Ready: true,
				},
			},
			bootstrapConfigs: map[string]bootstrapv1.K0sControllerConfig{
				"test": {
					Spec: bootstrapv1.K0sControllerConfigSpec{
						K0sConfigSpec: &bootstrapv1.K0sConfigSpec{
							K0s: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "k0s.k0sproject.io/v1beta1",
									"kind":       "ClusterConfig",
									"metadata": map[string]interface{}{
										"name": "k0s",
									},
									"spec": map[string]interface{}{
										"api": map[string]interface{}{
											"externalAddress": "172.18.0.3",
											"extraArgs": map[string]interface{}{
												"anonymous-auth": "true",
											},
										},
										"network": map[string]interface{}{
											"clusterDomain": "cluster.local",
											"podCIDR":       "192.168.0.0/16",
											"serviceCIDR":   "10.128.0.0/12",
										},
										"telemetry": map[string]interface{}{
											"enabled": "false",
										},
										"storage": map[string]interface{}{
											"etcd": map[string]interface{}{
												"extraArgs": map[string]interface{}{
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
