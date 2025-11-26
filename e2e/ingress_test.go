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
	"bytes"
	"fmt"
	"github.com/k0sproject/k0s/inttest/common"
	"k8s.io/client-go/kubernetes"
	"os/exec"
	"path/filepath"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"

	e2eutil "github.com/k0sproject/k0smotron/e2e/util"
	podexec "github.com/k0sproject/k0smotron/internal/exec"
	"github.com/k0sproject/k0smotron/internal/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

func TestIngress(t *testing.T) {
	setupAndRun(t, ingressSupportSpec)
}

func ingressSupportSpec(t *testing.T) {
	testName := "ingress"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	testNamespace, testCancelWatches := framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
		Creator:   bootstrapClusterProxy.GetClient(),
		ClientSet: bootstrapClusterProxy.GetClientSet(),
		Name:      testName,
		LogFolder: filepath.Join(artifactFolder, "clusters", "bootstrap"),
	})

	// Install HAProxy ingress controller
	installHAProxyIngress(t, bootstrapClusterProxy)

	workloadClusterName := fmt.Sprintf("%s-workload-%s", testName, util.RandomString(6))
	workloadClusterNamespace := testNamespace.Name

	// Detect kind IP for DNS names
	kindIP := detectKindIP(t)
	t.Logf("Detected kind IP: %s", kindIP)

	// Create test file secret for the cluster template
	testFileSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-file-secret",
			Namespace: workloadClusterNamespace,
		},
		Data: map[string][]byte{
			"value": []byte("test-content"),
		},
	}
	require.NoError(t, bootstrapClusterProxy.GetClient().Create(ctx, testFileSecret), "Should create test file secret")

	// Create a cluster with ingress configuration
	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		InfrastructureProvider:   "docker",
		Flavor:                   "ingress",
		Namespace:                workloadClusterNamespace,
		ClusterName:              workloadClusterName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: ptr.To[int64](1),
		LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME": workloadClusterName,
			"NAMESPACE":    workloadClusterNamespace,
			"KIND_IP":      kindIP,
			"HAPROXY_PORT": "32143", // HAProxy svc NodePort for HTTPS
		},
	})

	// Apply the cluster template yaml
	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	// Wait for the cluster to be ready using the utility function
	cluster, err := e2eutil.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: workloadClusterNamespace,
		Name:      workloadClusterName,
	}, e2eutil.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	defer func() {
		e2eutil.DumpSpecResourcesAndCleanup(
			ctx,
			testName,
			bootstrapClusterProxy,
			artifactFolder,
			testNamespace,
			testCancelWatches,
			cluster,
			e2eutil.GetInterval(e2eConfig, testName, "wait-delete-cluster"),
			skipCleanup,
			clusterctlConfigPath,
		)

		testCancelWatches()
	}()

	// Wait for the control plane to be initialized
	_, err = e2eutil.DiscoveryAndWaitForHCPToBeReady(ctx, e2eutil.DiscoveryAndWaitForHCPReadyInput{
		Cluster: cluster,
		Lister:  bootstrapClusterProxy.GetClient(),
		Getter:  bootstrapClusterProxy.GetClient(),
	}, e2eutil.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)

	fmt.Print("Waiting for MachineDeployment to be ready\n")
	require.Eventually(t, func() bool {
		md := &clusterv1.MachineDeployment{}
		err := bootstrapClusterProxy.GetClient().Get(ctx, client.ObjectKey{
			Namespace: workloadClusterNamespace,
			Name:      workloadClusterName,
		}, md)
		if err != nil {
			return false
		}
		return md.Status.ReadyReplicas == 2
	}, 5*time.Minute, 10*time.Second, "MachineDeployment failed to become ready")

	fmt.Println("Check kube api connection from the nodes through the proxy")
	machineList := &clusterv1.MachineList{}
	require.NoError(t, bootstrapClusterProxy.GetClient().List(ctx, machineList, client.InNamespace(workloadClusterNamespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel: workloadClusterName,
	}), "Should list machines")

	wrc, err := remote.RESTConfig(ctx, "ingress-test", bootstrapClusterProxy.GetClient(), client.ObjectKey{Namespace: workloadClusterNamespace, Name: workloadClusterName})
	require.NoError(t, err, "Should get workload rest config")
	wcs, err := kubernetes.NewForConfig(wrc)
	require.NoError(t, err, "Should get workload clientset")
	require.NoError(t, common.WaitForDaemonSet(ctx, wcs, "konnectivity-agent"))

	podList := &corev1.PodList{}
	err = bootstrapClusterProxy.GetClient().List(ctx, podList, client.InNamespace(testNamespace.Name))
	require.NoError(t, err, "Should list k0smotron pods")
	out, err := podexec.PodExecCmdOutput(ctx, bootstrapClusterProxy.GetClientSet(), bootstrapClusterProxy.GetRESTConfig(), podList.Items[0].Name, testNamespace.Name, "k0s kc logs -n kube-system ds/konnectivity-agent")
	require.NoError(t, err)
	t.Logf("Konnectivity agent logs:\n%s", string(out))

	for _, m := range machineList.Items {
		var (
			stdout bytes.Buffer
			stderr bytes.Buffer
		)
		cmd := exec.Command("docker", "exec", m.Name, "curl", "https://10.128.0.1/healthz", "--cacert", "/etc/haproxy/certs/ca.crt")
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		require.NoError(t, err, stderr.String())
		require.Equal(t, "ok", stdout.String(), "Expected to get 'ok' from the healthz endpoint")
	}
	fmt.Println("All good")
}

func detectKindIP(t *testing.T) string {
	// Get the kind cluster IP from the bootstrap cluster
	var nodes corev1.NodeList
	require.NoError(t, bootstrapClusterProxy.GetClient().List(ctx, &nodes), "Should list nodes")

	for _, node := range nodes.Items {
		if node.Spec.ProviderID != "" && len(node.Status.Addresses) > 0 {
			for _, addr := range node.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					// Extract IP from provider ID or use the address directly
					ip := addr.Address
					if ip != "" && ip != "127.0.0.1" {
						return ip
					}
				}
			}
		}
	}

	require.Fail(t, "Failed to detect kind IP")
	return ""
}

func installHAProxyIngress(t *testing.T, bootstrapClusterProxy capiframework.ClusterProxy) {
	out, err := exec.Command("kubectl", "--kubeconfig", bootstrapClusterProxy.GetKubeconfigPath(), "apply", "-f", "./data/haproxy-ingress.yaml").CombinedOutput()
	t.Log("haproxy ingress installation logs: ", string(out))
	require.NoError(t, err, "should apply HAProxy yaml")
}
