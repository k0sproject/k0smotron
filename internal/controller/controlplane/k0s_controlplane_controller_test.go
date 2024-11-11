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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	"k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmConfig "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

func TestK0sConfigEnrichment(t *testing.T) {
	var testCases = []struct {
		cluster *clusterv1.Cluster
		kcp     *v1beta1.K0sControlPlane
		want    *unstructured.Unstructured
	}{
		{
			cluster: &clusterv1.Cluster{},
			kcp:     &v1beta1.K0sControlPlane{},
			want:    nil,
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"serviceCIDR": "10.96.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{
				Spec: v1beta1.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1.K0sConfigSpec{
						K0s: &unstructured.Unstructured{Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"network": map[string]interface{}{"serviceCIDR": "10.98.0.0/12"},
							},
						}},
					},
				},
			},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"serviceCIDR": "10.98.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						ServiceDomain: "cluster.local",
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"clusterDomain": "cluster.local"},
				},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			actual, err := enrichK0sConfigWithClusterData(tc.cluster, tc.kcp.Spec.K0sConfigSpec.K0s)
			require.NoError(t, err)
			require.Equal(t, tc.want, actual)
		})
	}
}

func TestReconcileReturnErrorWhenOwnerClusterIsMissing(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-return-error-cluster-owner-missing")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())
	g.Expect(testEnv.Create(ctx, gmt)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, gmt, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(BeComparableTo(ctrl.Result{RequeueAfter: 20 * time.Second, Requeue: true}))

	g.Expect(testEnv.CleanupAndWait(ctx, cluster)).To(Succeed())

	g.Eventually(func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
		return err
	}, 10*time.Second).Should(HaveOccurred())
}

func TestReconcileNoK0sControlPlane(t *testing.T) {
	g := NewWithT(t)
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-no-control-plane")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(BeComparableTo(ctrl.Result{}))
}

func TestReconcilePausedCluster(t *testing.T) {
	g := NewWithT(t)
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-paused-cluster")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)

	// Cluster 'paused'.
	cluster.Spec.Paused = true

	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(BeComparableTo(ctrl.Result{}))
}

func TestReconcilePausedK0sControlPlane(t *testing.T) {
	g := NewWithT(t)
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-paused-k0scontrolplane")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// K0sControlPlane with 'paused' annotation.
	kcp.Annotations = map[string]string{"cluster.x-k8s.io/paused": "true"}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(BeComparableTo(ctrl.Result{}))
}

func TestReconcileTunneling(t *testing.T) {
	g := NewWithT(t)
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-tunneling")
	g.Expect(err).ToNot(HaveOccurred())

	node := createNode()
	g.Expect(testEnv.Create(ctx, node)).To(Succeed())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	kcp.Spec.K0sConfigSpec = bootstrapv1.K0sConfigSpec{
		Tunneling: bootstrapv1.TunnelingSpec{
			Enabled: true,
		},
	}
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	g.Expect(err).ToNot(HaveOccurred())

	r := &K0sController{
		Client:    testEnv,
		ClientSet: clientSet,
	}
	err = r.reconcileTunneling(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())

	frpToken, err := clientSet.CoreV1().Secrets(ns.Name).Get(ctx, fmt.Sprintf(FRPTokenNameTemplate, cluster.Name), metav1.GetOptions{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(metav1.IsControlledBy(frpToken, kcp)).To(BeTrue())

	frpCM, err := clientSet.CoreV1().ConfigMaps(ns.Name).Get(ctx, fmt.Sprintf(FRPConfigMapNameTemplate, kcp.GetName()), metav1.GetOptions{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(metav1.IsControlledBy(frpCM, kcp)).To(BeTrue())

	frpDeploy, err := clientSet.AppsV1().Deployments(ns.Name).Get(ctx, fmt.Sprintf(FRPDeploymentNameTemplate, kcp.GetName()), metav1.GetOptions{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(metav1.IsControlledBy(frpDeploy, kcp)).To(BeTrue())

	frpService, err := clientSet.CoreV1().Services(ns.Name).Get(ctx, fmt.Sprintf(FRPServiceNameTemplate, kcp.GetName()), metav1.GetOptions{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(metav1.IsControlledBy(frpService, kcp)).To(BeTrue())
}

func TestReconcileKubeconfigEmptyAPIEndpoints(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-kubeconfig-empty-api-endpoints")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)

	// Host and Port with zero values.
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{}

	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}
	err = r.reconcileKubeconfig(ctx, cluster, kcp)
	g.Expect(err).To(HaveOccurred())

	kubeconfigSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      secret.Name(cluster.Name, secret.Kubeconfig),
	}
	g.Expect(testEnv.GetAPIReader().Get(ctx, secretKey, kubeconfigSecret)).To(MatchError(ContainSubstring("not found")))
}

func TestReconcileKubeconfigMissingCACertificate(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-kubeconfig-missing-ca-certificates")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}

	err = r.reconcileKubeconfig(ctx, cluster, kcp)
	g.Expect(err).To(HaveOccurred())

	kubeconfigSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      secret.Name(cluster.Name, secret.Kubeconfig),
	}
	g.Expect(testEnv.GetAPIReader().Get(ctx, secretKey, kubeconfigSecret)).To(MatchError(ContainSubstring("not found")))
}

func TestReconcileKubeconfigTunnelingModeNotEnabled(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-kubeconfig-tunneling-mode-not-enabled")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// Tunneling not enabled.
	kcp.Spec.K0sConfigSpec.Tunneling.Enabled = false

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{},
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: {},
		},
	}
	g.Expect(testEnv.Create(ctx, kubeconfigSecret)).To(Succeed())

	r := &K0sController{
		Client: testEnv,
	}

	err = r.reconcileKubeconfig(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestReconcileKubeconfigTunnelingModeProxy(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-kubeconfig-tunneling-mode-proxy")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// Tunneling mode = 'proxy'
	kcp.Spec.K0sConfigSpec.Tunneling = bootstrapv1.TunnelingSpec{
		Enabled:           true,
		Mode:              "proxy",
		ServerAddress:     "test.com",
		TunnelingNodePort: 9999,
	}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{},
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: {},
		},
	}
	g.Expect(testEnv.Create(ctx, kubeconfigSecret)).To(Succeed())

	clusterCerts := secret.NewCertificatesForInitialControlPlane(&kubeadmConfig.ClusterConfiguration{})
	g.Expect(clusterCerts.Generate()).To(Succeed())
	caCert := clusterCerts.GetByPurpose(secret.ClusterCA)
	caCertSecret := caCert.AsSecret(
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		*metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane")),
	)
	g.Expect(testEnv.Create(ctx, caCertSecret)).To(Succeed())

	r := &K0sController{
		Client: testEnv,
	}

	err = r.reconcileKubeconfig(ctx, cluster, kcp)
	g.Expect(err).To(HaveOccurred())

	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      secret.Name(cluster.Name+"-proxied", secret.Kubeconfig),
	}

	kubeconfigProxiedSecret := &corev1.Secret{}
	g.Expect(testEnv.Get(ctx, secretKey, kubeconfigProxiedSecret)).To(Succeed())

	kubeconfigProxiedSecretCrt, _ := runtime.Decode(clientcmdlatest.Codec, kubeconfigProxiedSecret.Data["value"])
	for _, v := range kubeconfigProxiedSecretCrt.(*api.Config).Clusters {
		g.Expect(v.Server).To(Equal("https://test.endpoint:6443"))
		g.Expect(v.ProxyURL).To(Equal("http://test.com:9999"))
	}
}

func TestReconcileKubeconfigTunnelingModeTunnel(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-kubeconfig-tunneling-mode-tunnel")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// Tunneling mode = 'tunnel'
	kcp.Spec.K0sConfigSpec.Tunneling = bootstrapv1.TunnelingSpec{
		Enabled:           true,
		Mode:              "tunnel",
		ServerAddress:     "test.com",
		TunnelingNodePort: 9999,
	}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{},
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: {},
		},
	}
	g.Expect(testEnv.Create(ctx, kubeconfigSecret)).To(Succeed())

	clusterCerts := secret.NewCertificatesForInitialControlPlane(&kubeadmConfig.ClusterConfiguration{})
	g.Expect(clusterCerts.Generate()).To(Succeed())
	caCert := clusterCerts.GetByPurpose(secret.ClusterCA)
	caCertSecret := caCert.AsSecret(
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		*metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane")),
	)
	g.Expect(testEnv.Create(ctx, caCertSecret)).To(Succeed())

	r := &K0sController{
		Client: testEnv,
	}

	err = r.reconcileKubeconfig(ctx, cluster, kcp)
	g.Expect(err).To(HaveOccurred())

	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      secret.Name(cluster.Name+"-tunneled", secret.Kubeconfig),
	}
	kubeconfigProxiedSecret := &corev1.Secret{}
	g.Expect(testEnv.Get(ctx, secretKey, kubeconfigProxiedSecret)).ToNot(HaveOccurred())

	kubeconfigProxiedSecretCrt, _ := runtime.Decode(clientcmdlatest.Codec, kubeconfigProxiedSecret.Data["value"])
	for _, v := range kubeconfigProxiedSecretCrt.(*api.Config).Clusters {
		g.Expect(v.Server).To(Equal("https://test.com:9999"))
	}
}

func TestReconcileK0sConfigNotProvided(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-k0sconfig-not-provided")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	kcp.Spec.K0sConfigSpec.K0s = nil
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}
	err = r.reconcileConfig(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(kcp.Spec.K0sConfigSpec.K0s).To(BeNil())
}

func TestReconcileK0sConfigWithNLLBEnabled(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-nllb-enabled")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// Enable '.spec.network.nodeLocalLoadBalancing'
	kcp.Spec.K0sConfigSpec = bootstrapv1.K0sConfigSpec{
		K0s: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"api": map[string]interface{}{
						"sans": []interface{}{
							"test.com",
						},
					},
					"network": map[string]interface{}{
						"nodeLocalLoadBalancing": map[string]interface{}{
							"enabled": true,
						},
					},
				},
			},
		},
	}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}
	err = r.reconcileConfig(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())

	expectedk0sConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k0s.k0sproject.io/v1beta1",
			"kind":       "ClusterConfig",
			"spec": map[string]interface{}{
				"api": map[string]interface{}{
					"sans": []interface{}{
						"test.endpoint",
						"test.com",
					},
				},
				"network": map[string]interface{}{
					"nodeLocalLoadBalancing": map[string]interface{}{
						"enabled": true,
					},
				},
			},
		},
	}
	g.Expect(kcp.Spec.K0sConfigSpec.K0s).To(Equal(expectedk0sConfig))
}

func TestReconcileK0sConfigWithNLLBDisabled(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-nllb-disabled")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// Disable '.spec.network.nodeLocalLoadBalancing'
	kcp.Spec.K0sConfigSpec = bootstrapv1.K0sConfigSpec{
		K0s: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"api": map[string]interface{}{
						"sans": []interface{}{
							"test.com",
						},
					},
				},
			},
		},
	}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}
	err = r.reconcileConfig(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())

	expectedk0sConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k0s.k0sproject.io/v1beta1",
			"kind":       "ClusterConfig",
			"spec": map[string]interface{}{
				"api": map[string]interface{}{
					"sans": []interface{}{
						"test.com",
					},
					"externalAddress": "test.endpoint",
				},
			},
		},
	}
	g.Expect(kcp.Spec.K0sConfigSpec.K0s).To(Equal(expectedk0sConfig))
}

func TestReconcileK0sConfigTunnelingServerAddressToApiSans(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-tunneling-serveraddress-to-api-sans")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())

	// With '.spec.k0sConfigSpec.Tunneling.ServerAddress'
	kcp.Spec.K0sConfigSpec = bootstrapv1.K0sConfigSpec{
		K0s: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"api": map[string]interface{}{
						"sans": []interface{}{
							"test.com",
						},
					},
				},
			},
		},
		Tunneling: bootstrapv1.TunnelingSpec{
			ServerAddress: "my-tunneling-server-address.com",
		},
	}

	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, cluster, ns)

	r := &K0sController{
		Client: testEnv,
	}
	err = r.reconcileConfig(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())

	expectedk0sConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k0s.k0sproject.io/v1beta1",
			"kind":       "ClusterConfig",
			"spec": map[string]interface{}{
				"api": map[string]interface{}{
					"sans": []interface{}{
						"test.com",
						"my-tunneling-server-address.com",
					},
					"externalAddress": "test.endpoint",
				},
			},
		},
	}
	g.Expect(kcp.Spec.K0sConfigSpec.K0s).To(Equal(expectedk0sConfig))
}

func TestReconcileMachinesScaleUp(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machine-scale-up")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, gmt)).To(Succeed())

	desiredReplicas := 5
	kcp.Spec.Replicas = int32(desiredReplicas)
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane"))

	r := &K0sController{
		Client: testEnv,
	}

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, firstMachineRelatedToControlPlane)).To(Succeed())

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, secondMachineRelatedToControlPlane)).To(Succeed())

	machineNotRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-machine",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
		},
	}
	g.Expect(testEnv.Create(ctx, machineNotRelatedToControlPlane)).To(Succeed())

	err = r.reconcileMachines(ctx, cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())

	machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(machines).To(HaveLen(desiredReplicas))
	for _, m := range machines {
		expectedLabels := map[string]string{
			clusterv1.ClusterNameLabel:         cluster.GetName(),
			clusterv1.MachineControlPlaneLabel: "true",
			generatedMachineRoleLabel:          "control-plane",
		}
		g.Expect(m.Labels).Should(Equal(expectedLabels))
		g.Expect(metav1.IsControlledBy(m, kcp)).To(BeTrue())
		g.Expect(*m.Spec.Version).Should(Equal(kcp.Spec.Version))
	}
}

func TestReconcileMachinesScaleDown(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-scale-down")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, gmt)).To(Succeed())

	desiredReplicas := 1
	kcp.Spec.Replicas = int32(desiredReplicas)
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane"))

	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(roundTripperForWorkloadClusterAPI),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	r := &K0sController{
		Client:                    testEnv,
		workloadClusterKubeClient: kubernetes.New(restClient),
	}

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, firstMachineRelatedToControlPlane)).To(Succeed())

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, secondMachineRelatedToControlPlane)).To(Succeed())

	thirdMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 2),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	thirdMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, thirdMachineRelatedToControlPlane)).To(Succeed())

	machineNotRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-machine",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
		},
	}
	g.Expect(testEnv.Create(ctx, machineNotRelatedToControlPlane)).To(Succeed())

	g.Eventually(func(g Gomega) {
		err = r.reconcileMachines(ctx, cluster, kcp)
		g.Expect(err).ToNot(HaveOccurred())

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(machines).To(HaveLen(desiredReplicas))

		k0sBootstrapConfigList := &bootstrapv1.K0sControllerConfigList{}
		g.Expect(testEnv.GetAPIReader().List(ctx, k0sBootstrapConfigList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(k0sBootstrapConfigList.Items).To(HaveLen(desiredReplicas))

		for _, m := range machines {
			expectedLabels := map[string]string{
				clusterv1.ClusterNameLabel:         cluster.GetName(),
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			}
			g.Expect(m.Labels).Should(Equal(expectedLabels))
			g.Expect(metav1.IsControlledBy(m, kcp)).To(BeTrue())
			g.Expect(*m.Spec.Version).Should(Equal(kcp.Spec.Version))

			// verify that the bootrap config related to the existing machines is present.
			bootstrapObjectKey := client.ObjectKey{
				Namespace: m.Namespace,
				Name:      m.Name,
			}
			kc := &bootstrapv1.K0sControllerConfig{}
			g.Expect(testEnv.GetAPIReader().Get(ctx, bootstrapObjectKey, kc)).ToNot(HaveOccurred())
			g.Expect(metav1.IsControlledBy(kc, m)).To(BeTrue())
		}
	}).Should(Succeed())
}

func TestReconcileMachinesSyncOldMachines(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-sync-old-machines")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	g.Expect(testEnv.Create(ctx, gmt)).To(Succeed())

	desiredReplicas := 3
	kcp.Spec.Replicas = int32(desiredReplicas)
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane"))

	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(roundTripperForWorkloadClusterAPI),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	g.Expect(err).ToNot(HaveOccurred())

	r := &K0sController{
		Client:                    testEnv,
		workloadClusterKubeClient: kubernetes.New(restClient),
		ClientSet:                 clientSet,
	}

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.29.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, firstMachineRelatedToControlPlane)).To(Succeed())

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.30.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, secondMachineRelatedToControlPlane)).To(Succeed())

	thirdMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName(kcp.Name, 2),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     ptr.To("v1.29.0"),
			InfrastructureRef: v1.ObjectReference{
				Kind:       "GenericInfrastructureMachineTemplate",
				Namespace:  ns.Name,
				Name:       gmt.GetName(),
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
		},
	}
	thirdMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	g.Expect(testEnv.Create(ctx, thirdMachineRelatedToControlPlane)).To(Succeed())

	g.Eventually(func(g Gomega) {
		err = r.reconcileMachines(ctx, cluster, kcp)
		g.Expect(err).ToNot(HaveOccurred())

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(machines).To(HaveLen(desiredReplicas))

		k0sBootstrapConfigList := &bootstrapv1.K0sControllerConfigList{}
		g.Expect(testEnv.GetAPIReader().List(ctx, k0sBootstrapConfigList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(k0sBootstrapConfigList.Items).To(HaveLen(desiredReplicas))

		for _, m := range machines {
			expectedLabels := map[string]string{
				clusterv1.ClusterNameLabel:         cluster.GetName(),
				clusterv1.MachineControlPlaneLabel: "true",
				generatedMachineRoleLabel:          "control-plane",
			}
			g.Expect(m.Labels).Should(Equal(expectedLabels))
			g.Expect(metav1.IsControlledBy(m, kcp)).To(BeTrue())
			g.Expect(*m.Spec.Version).Should(Equal(kcp.Spec.Version))

			// verify that the bootrap config related to the existing machines is present.
			bootstrapObjectKey := client.ObjectKey{
				Namespace: m.Namespace,
				Name:      m.Name,
			}
			kc := &bootstrapv1.K0sControllerConfig{}
			g.Expect(testEnv.GetAPIReader().Get(ctx, bootstrapObjectKey, kc)).ToNot(HaveOccurred())
			g.Expect(metav1.IsControlledBy(kc, m)).To(BeTrue())
		}
	}, 5*time.Second).Should(Succeed())
}

func TestReconcileInitializeControlPlanes(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-initialize-controlplanes")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	g.Expect(testEnv.Create(ctx, cluster)).To(Succeed())
	kcp.Spec.Replicas = 1
	g.Expect(testEnv.Create(ctx, kcp)).To(Succeed())
	g.Expect(testEnv.Create(ctx, gmt)).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(testEnv.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, gmt, cluster, ns)

	expectedLabels := map[string]string{clusterv1.ClusterNameLabel: cluster.Name}

	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(roundTripperForWorkloadClusterAPI),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	r := &K0sController{
		Client:                    testEnv,
		workloadClusterKubeClient: kubernetes.New(restClient),
	}

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(testEnv.GetAPIReader().Get(ctx, client.ObjectKey{Name: kcp.Name, Namespace: kcp.Namespace}, kcp)).To(Succeed())
	g.Expect(kcp.Status.Selector).NotTo(BeEmpty())
	g.Expect(kcp.Status.Version).To(Equal(fmt.Sprintf("%s+%s", kcp.Spec.Version, defaultK0sSuffix)))
	g.Expect(kcp.Status.Replicas).To(BeEquivalentTo(1))
	g.Expect(testEnv.GetAPIReader().Get(ctx, util.ObjectKey(gmt), gmt)).To(Succeed())
	g.Expect(gmt.GetOwnerReferences()).To(ContainElement(metav1.OwnerReference{
		APIVersion:         clusterv1.GroupVersion.String(),
		Kind:               "Cluster",
		Name:               cluster.Name,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
		UID:                cluster.UID,
	}))
	g.Expect(conditions.IsFalse(kcp, cpv1beta1.ControlPlaneReadyCondition)).To(BeTrue())

	// Expected secrets are created
	caSecret, err := secret.GetFromNamespacedName(ctx, testEnv, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, secret.ClusterCA)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(caSecret).NotTo(BeNil())
	g.Expect(caSecret.Data).NotTo(BeEmpty())
	g.Expect(caSecret.Labels).To(Equal(expectedLabels))

	etcdSecret, err := secret.GetFromNamespacedName(ctx, testEnv, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, secret.EtcdCA)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(etcdSecret).NotTo(BeNil())
	g.Expect(etcdSecret.Data).NotTo(BeEmpty())
	g.Expect(etcdSecret.Labels).To(Equal(expectedLabels))

	kubeconfigSecret, err := secret.GetFromNamespacedName(ctx, testEnv, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, secret.Kubeconfig)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(kubeconfigSecret).NotTo(BeNil())
	g.Expect(kubeconfigSecret.Data).NotTo(BeEmpty())
	g.Expect(kubeconfigSecret.Labels).To(Equal(expectedLabels))
	k, err := kubeconfig.FromSecret(ctx, testEnv, util.ObjectKey(cluster))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(k).NotTo(BeEmpty())

	proxySecret, err := secret.GetFromNamespacedName(ctx, testEnv, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, secret.FrontProxyCA)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(proxySecret).NotTo(BeNil())
	g.Expect(proxySecret.Data).NotTo(BeEmpty())
	g.Expect(proxySecret.Labels).To(Equal(expectedLabels))

	saSecret, err := secret.GetFromNamespacedName(ctx, testEnv, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, secret.ServiceAccount)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(saSecret).NotTo(BeNil())
	g.Expect(saSecret.Data).NotTo(BeEmpty())
	g.Expect(saSecret.Labels).To(Equal(expectedLabels))

	machineList := &clusterv1.MachineList{}
	g.Expect(testEnv.GetAPIReader().List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(machineList.Items).To(HaveLen(1))
	machine := machineList.Items[0]
	g.Expect(machine.Name).To(HavePrefix(kcp.Name))
	g.Expect(*machine.Spec.Version).To(Equal(fmt.Sprintf("%s+%s", kcp.Spec.Version, defaultK0sSuffix)))
	// Newly cloned infra objects should have the infraref annotation.
	infraObj, err := external.Get(ctx, r.Client, &machine.Spec.InfrastructureRef, machine.Spec.InfrastructureRef.Namespace)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(infraObj.GetAnnotations()).To(HaveKeyWithValue(clusterv1.TemplateClonedFromNameAnnotation, gmt.GetName()))
	g.Expect(infraObj.GetAnnotations()).To(HaveKeyWithValue(clusterv1.TemplateClonedFromGroupKindAnnotation, gmt.GroupVersionKind().GroupKind().String()))

	k0sBootstrapConfigList := &bootstrapv1.K0sControllerConfigList{}
	g.Expect(testEnv.GetAPIReader().List(ctx, k0sBootstrapConfigList, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(k0sBootstrapConfigList.Items).To(HaveLen(1))

	g.Expect(metav1.IsControlledBy(&k0sBootstrapConfigList.Items[0], &machine)).To(BeTrue())

}

func roundTripperForWorkloadClusterAPI(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)

	switch req.Method {
	case "GET":
		if strings.HasPrefix(req.URL.Path, "/apis/autopilot.k0sproject.io/v1beta2/controlnodes/") {
			res, err := json.Marshal(autopilot.ControlNode{})
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(res))}, nil

		}
	case "DELETE":
		if strings.HasPrefix(req.URL.Path, "/apis/autopilot.k0sproject.io/v1beta2/controlnodes/") {
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: nil}, nil
		}
	case "PATCH":
		switch {
		case strings.HasPrefix(req.URL.Path, "/apis/etcd.k0sproject.io/v1beta1/etcdmembers/"):
			{
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: nil}, nil
			}
		case strings.HasPrefix(req.URL.Path, "/apis/autopilot.k0sproject.io/v1beta2/controlnodes/"):
			{
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: nil}, nil
			}
		case strings.HasPrefix(req.URL.Path, "/apis/infrastructure.cluster.x-k8s.io/v1beta1/namespaces/"):
			{
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: nil}, nil
			}
		}
	}

	return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: nil}, nil
}

func newCluster(namespacedName *types.NamespacedName) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespacedName.Namespace,
			Name:      namespacedName.Name,
		},
	}
}

func createNode() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "foo",
			Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeExternalIP,
					Address: "1.1.1.1",
				},
			},
		},
	}
}

func createClusterWithControlPlane(namespace string) (*clusterv1.Cluster, *cpv1beta1.K0sControlPlane, *unstructured.Unstructured) {
	kcpName := fmt.Sprintf("kcp-foo-%s", util.RandomString(6))

	cluster := newCluster(&types.NamespacedName{Name: kcpName, Namespace: namespace})
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneRef: &v1.ObjectReference{
			Kind:       "K0sControlPlane",
			Namespace:  namespace,
			Name:       kcpName,
			APIVersion: cpv1beta1.GroupVersion.String(),
		},
		ControlPlaneEndpoint: clusterv1.APIEndpoint{
			Host: "test.endpoint",
			Port: 6443,
		},
	}

	kcp := &cpv1beta1.K0sControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cpv1beta1.GroupVersion.String(),
			Kind:       "K0sControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcpName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       kcpName,
					UID:        "1",
				},
			},
		},
		Spec: v1beta1.K0sControlPlaneSpec{
			MachineTemplate: &v1beta1.K0sControlPlaneMachineTemplate{
				InfrastructureRef: v1.ObjectReference{
					Kind:       "GenericInfrastructureMachineTemplate",
					Namespace:  namespace,
					Name:       "infra-foo",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
			},
			UpdateStrategy: cpv1beta1.UpdateRecreate,
			Replicas:       int32(1),
			Version:        "v1.30.0",
		},
	}

	genericMachineTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "GenericInfrastructureMachineTemplate",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta1",
			"metadata": map[string]interface{}{
				"name":      "infra-foo",
				"namespace": namespace,
				"annotations": map[string]interface{}{
					clusterv1.TemplateClonedFromNameAnnotation:      kcp.Spec.MachineTemplate.InfrastructureRef.Name,
					clusterv1.TemplateClonedFromGroupKindAnnotation: kcp.Spec.MachineTemplate.InfrastructureRef.GroupVersionKind().GroupKind().String(),
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"hello": "world",
					},
				},
			},
		},
	}
	return cluster, kcp, genericMachineTemplate
}
