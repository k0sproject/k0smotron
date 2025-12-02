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

package util

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	dockerprovisioner "github.com/k0sproject/k0smotron/e2e/util/poolprovisioner/docker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/infrastructure/container"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	retryableOperationInterval = 3 * time.Second
	// retryableOperationTimeout requires a higher value especially for self-hosted upgrades.
	// Short unavailability of the Kube APIServer due to joining etcd members paired with unreachable conversion webhooks due to
	// failed leader election and thus controller restarts lead to longer taking retries.
	// The timeout occurs when listing machines in `GetControlPlaneMachinesByCluster`.
	retryableOperationTimeout = 3 * time.Minute
)

// DiscoveryAndWaitForCluster discovers a cluster object in a namespace and waits for the cluster infrastructure to be provisioned.
func DiscoveryAndWaitForCluster(ctx context.Context, input framework.DiscoveryAndWaitForClusterInput, interval Interval) (*clusterv1.Cluster, error) {
	var cluster *clusterv1.Cluster
	err := wait.PollUntilContextTimeout(ctx, retryableOperationInterval, retryableOperationTimeout, true, func(ctx context.Context) (done bool, err error) {
		cluster, err = GetClusterByName(ctx, framework.GetClusterByNameInput{
			Getter:    input.Getter,
			Name:      input.Name,
			Namespace: input.Namespace,
		})
		if err != nil {
			return false, err
		}

		return cluster != nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Cluster object %s: %w", klog.KRef(input.Namespace, input.Name), err)
	}

	return WaitForClusterToProvision(ctx, framework.WaitForClusterToProvisionInput{
		Getter:  input.Getter,
		Cluster: cluster,
	}, interval)
}

// GetClusterByName returns a Cluster object given his name.
func GetClusterByName(ctx context.Context, input framework.GetClusterByNameInput) (*clusterv1.Cluster, error) {
	cluster := &clusterv1.Cluster{}
	key := client.ObjectKey{
		Namespace: input.Namespace,
		Name:      input.Name,
	}

	err := wait.PollUntilContextTimeout(ctx, retryableOperationInterval, retryableOperationTimeout, true, func(ctx context.Context) (done bool, err error) {
		return input.Getter.Get(ctx, key, cluster) == nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Cluster object %s: %w", klog.KRef(input.Namespace, input.Name), err)
	}

	return cluster, nil
}

// WaitForClusterToProvision will wait for a cluster to have a phase status of provisioned.
func WaitForClusterToProvision(ctx context.Context, input framework.WaitForClusterToProvisionInput, interval Interval) (*clusterv1.Cluster, error) {
	cluster := &clusterv1.Cluster{}
	fmt.Println("Waiting for cluster to enter the provisioned phase")

	err := wait.PollUntilContextTimeout(ctx, interval.tick, interval.timeout, true, func(ctx context.Context) (done bool, err error) {
		key := client.ObjectKey{
			Namespace: input.Cluster.GetNamespace(),
			Name:      input.Cluster.GetName(),
		}
		if err := input.Getter.Get(ctx, key, cluster); err != nil {
			return false, err
		}
		return cluster.Status.Phase == string(clusterv1.ClusterPhaseProvisioned), nil
	})
	if err != nil {
		return nil, fmt.Errorf("timed out waiting for Cluster %s to provision: %w", klog.KObj(input.Cluster), err)
	}

	return cluster, nil
}

// GetWorkloadClusterClient returns a client for the workload cluster, handling k0smotron infrastructure specially. When the
// infrastructure is k0smotron, the workload cluster server runs locally, so we need to patch the client to point to localhost.
func getWorkloadClusterClient(ctx context.Context, clusterProxy framework.ClusterProxy, cluster *clusterv1.Cluster) (client.Client, error) {
	isK0smotronInfrastructure, err := isK0smotronInfrastructure(ctx, IsK0smotronInfrastructureInput{
		Getter:  clusterProxy.GetClient(),
		Cluster: cluster,
	})
	if err != nil {
		return nil, err
	}

	if isK0smotronInfrastructure {
		c, err := getLocalWorkloadClient(ctx, clusterProxy, cluster.Namespace, cluster.Name)
		if err != nil {
			return nil, err
		}
		return c, nil
	}

	return clusterProxy.GetWorkloadCluster(ctx, cluster.Namespace, cluster.Name).GetClient(), nil
}

// getLocalWorkloadClient retrieves the workload cluster client for k0smotron infrastructure clusters where controlplane url need to be modified
// to point to the local port-forwarded API server.
func getLocalWorkloadClient(ctx context.Context, clusterProxy capiframework.ClusterProxy, namespace, name string) (crclient.Client, error) {
	cl := clusterProxy.GetClient()

	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Name:      fmt.Sprintf("%s-kubeconfig", name),
		Namespace: namespace,
	}
	err := cl.Get(ctx, key, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", key, err)
	}

	config, err := clientcmd.Load(secret.Data["value"])
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from secret %s: %w", key, err)
	}

	currentCluster := config.Contexts[config.CurrentContext].Cluster

	containerRuntime, err := container.NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	ctx = container.RuntimeInto(ctx, containerRuntime)
	loadBalancerName := dockerprovisioner.GetLoadBalancerName()

	// Check if the container exists locally.
	filters := container.FilterBuilder{}
	filters.AddKeyValue("name", loadBalancerName)
	containers, err := containerRuntime.ListContainers(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("container %s not found", loadBalancerName)
	}
	port, err := containerRuntime.GetHostPort(ctx, loadBalancerName, "6443/tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to get load balancer port: %w", err)
	}

	controlPlaneURL := &url.URL{
		Scheme: "https",
		Host:   "127.0.0.1:" + port,
	}
	config.Clusters[currentCluster].Server = controlPlaneURL.String()

	// now create the client
	restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config from modified kubeconfig: %w", err)
	}

	workloadClient, err := crclient.New(restConfig, crclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create workload client from modified rest config: %w", err)
	}
	return workloadClient, nil
}

type IsK0smotronInfrastructureInput struct {
	Getter  capiframework.Getter
	Cluster *clusterv1.Cluster
}

func isK0smotronInfrastructure(ctx context.Context, input IsK0smotronInfrastructureInput) (bool, error) {
	clusterInfra := &unstructured.Unstructured{}
	clusterInfra.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1beta1")
	clusterInfra.SetKind("RemoteCluster")
	clusterKey := crclient.ObjectKey{
		Name:      input.Cluster.Name,
		Namespace: input.Cluster.Namespace,
	}

	err := input.Getter.Get(ctx, clusterKey, clusterInfra)
	if err != nil {
		if strings.Contains(err.Error(), "no matches for kind \"RemoteCluster\"") {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
