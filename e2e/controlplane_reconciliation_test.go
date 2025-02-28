//go:build e2e

/*
Copyright 2025.

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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/k0smotron/k0smotron/e2e/util"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	configMapName    = "mhc-test"
	configMapDataKey = "signal"
	failLabelValue   = "fail"
)

func TestControlPlaneReconciliation(t *testing.T) {
	setup(t, controlplaneReconciliationSpec)
}

// Verifies KCP to properly adopt existing control plane Machines.
func controlplaneReconciliationSpec(t *testing.T) {
	testName := "controlplane-remediation"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	namespace, _ := util.SetupSpecNamespace(ctx, testName, managementClusterProxy, artifactFolder)

	// creates the mhc-test ConfigMap that will be used to control machines bootstrap during the remediation tests.
	createConfigMapForMachinesBootstrapSignal(ctx, managementClusterProxy.GetClient(), namespace.Name)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	serverAddr, err := getServerAddr(ctx, managementClusterProxy)
	require.NoError(t, err)

	authToken, err := getAuthenticationToken(ctx, managementClusterProxy, namespace.GetName())
	require.NoError(t, err)

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath: clusterctlConfigPath,
		KubeconfigPath:       managementClusterProxy.GetKubeconfigPath(),
		// select cluster templates
		Flavor: "remediation",

		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        e2eConfig.GetVariable(KubernetesVersion),
		ControlPlaneMachineCount: ptr.To[int64](3),
		// TODO: make infra provider configurable
		InfrastructureProvider: "docker",
		LogFolder:              filepath.Join(artifactFolder, "clusters", managementClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME": clusterName,
			"NAMESPACE":    namespace.Name,
			"TOKEN":        authToken,
			"SERVER":       serverAddr,
		},
	})
	require.NotNil(t, workloadClusterTemplate)

	require.Eventually(t, func() bool {
		return managementClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	// The first CP machine comes up but it does not complete bootstrap

	fmt.Println("FIRST CONTROL PLANE MACHINE")

	fmt.Println("Wait for the cluster to get stuck with the first CP machine not completing the bootstrap")

	allMachines, newMachines, err := util.WaitForMachines(ctx, util.WaitForMachinesInput{
		Lister:                   managementClusterProxy.GetClient(),
		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		ExpectedReplicas:         1,
		WaitForMachinesIntervals: util.GetInterval(e2eConfig, testName, "wait-machines"),
	})
	require.NoError(t, err)
	require.Len(t, allMachines, 1)
	require.Len(t, newMachines, 1)
	firstMachineName := newMachines[0]
	firstMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      firstMachineName,
			Namespace: namespace.Name,
		},
	}

	require.NoError(t, managementClusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(firstMachine), firstMachine))
	require.Nil(t, firstMachine.Status.NodeRef)
	fmt.Printf("Machine %s is up but still bootstrapping\n", firstMachineName)

	// Intentionally trigger remediation on the first CP, and validate the first machine is deleted and a replacement should come up.

	fmt.Println("REMEDIATING FIRST CONTROL PLANE MACHINE")

	fmt.Printf("Add mhc-test:fail label to machine %s so it will be immediately remediated", firstMachineName)
	firstMachineWithLabel := firstMachine.DeepCopy()
	firstMachineWithLabel.Labels["mhc-test"] = failLabelValue
	require.NoError(t, managementClusterProxy.GetClient().Patch(ctx, firstMachineWithLabel, client.MergeFrom(firstMachine)), "Failed to patch machine %d", firstMachineName)

	fmt.Printf("Wait for the first CP machine to be remediated, and the replacement machine to come up, but again get stuck with the Machine not completing the bootstrap")
	allMachines, newMachines, err = util.WaitForMachines(ctx, util.WaitForMachinesInput{
		Lister:                   managementClusterProxy.GetClient(),
		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		ExpectedReplicas:         1,
		ExpectedDeletedMachines:  []string{firstMachineName},
		WaitForMachinesIntervals: util.GetInterval(e2eConfig, testName, "wait-machines"),
	})
	require.NoError(t, err)
	require.Len(t, allMachines, 1)
	require.Len(t, newMachines, 1)
	firstMachineReplacementName := newMachines[0]
	firstMachineReplacement := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      firstMachineReplacementName,
			Namespace: namespace.Name,
		},
	}
	require.NoError(t, managementClusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(firstMachineReplacement), firstMachineReplacement), "Failed to get machine %d", firstMachineReplacementName)
	require.Nil(t, firstMachineReplacement.Status.NodeRef)
	fmt.Printf("Machine %s is up but still bootstrapping \n", firstMachineReplacementName)

	// The firstMachine replacement is up, meaning that the test validated that remediation of the first CP machine works (note: first CP is a special case because the cluster is not initialized yet).
	// In order to test remediation of other machine while provisioning we unblock bootstrap of the first CP replacement
	// and wait for the second cp machine to come up.

	fmt.Println("aaa")
}

// getServerAddr returns the address to be used for accessing the management cluster from a workload cluster.
func getServerAddr(ctx context.Context, clusterProxy capiframework.ClusterProxy) (string, error) {
	// With CAPD, we can't just access the bootstrap cluster via 127.0.0.1:<port> from the
	// workload cluster. Instead we retrieve the server name from the cluster-info ConfigMap in the bootstrap
	// cluster (e.g. "https://test-z45p9k-control-plane:6443")
	clusterInfoCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-info",
			Namespace: metav1.NamespacePublic,
		},
	}

	err := clusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(clusterInfoCM), clusterInfoCM)
	if err != nil {
		return "", err
	}

	kubeConfigString, ok := clusterInfoCM.Data["kubeconfig"]
	if !ok {
		return "", errors.New("cluster-info configmap data does not contain kubedonfig key")
	}

	kubeConfig, err := clientcmd.Load([]byte(kubeConfigString))
	if err != nil {
		return "", err
	}

	return kubeConfig.Clusters[""].Server, nil
}

// getAuthenticationToken returns a bearer authenticationToken with minimal RBAC permissions to access the mhc-test ConfigMap that will be used
// to control machines bootstrap during the remediation tests.
func getAuthenticationToken(ctx context.Context, managementClusterProxy framework.ClusterProxy, namespace string) (string, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mhc-test",
			Namespace: namespace,
		},
	}
	err := managementClusterProxy.GetClient().Create(ctx, sa)
	if err != nil {
		return "", fmt.Errorf("failed to create mhc-test service account: %w", err)
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mhc-test",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:         []string{"get", "list", "patch"},
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				ResourceNames: []string{"mhc-test"},
			},
		},
	}
	err = managementClusterProxy.GetClient().Create(ctx, role)
	if err != nil {
		return "", fmt.Errorf("failed to create mhc-test role: %w", err)
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mhc-test",
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  "",
				Name:      "mhc-test",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.SchemeGroupVersion.Group,
			Kind:     "Role",
			Name:     "mhc-test",
		},
	}
	err = managementClusterProxy.GetClient().Create(ctx, roleBinding)
	if err != nil {
		return "", fmt.Errorf("failed to create mhc-test role binding: %w", err)
	}

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To[int64](2 * 60 * 60), // 2 hours.
		},
	}
	tokenRequest, err = managementClusterProxy.GetClientSet().CoreV1().ServiceAccounts(namespace).CreateToken(ctx, "mhc-test", tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	return tokenRequest.Status.Token, nil
}

func createConfigMapForMachinesBootstrapSignal(ctx context.Context, writer client.Writer, namespace string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			configMapDataKey: "hold",
		},
	}
	return writer.Create(ctx, cm)
}
