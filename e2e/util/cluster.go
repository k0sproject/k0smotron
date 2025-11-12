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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
func DiscoveryAndWaitForCluster(ctx context.Context, input framework.DiscoveryAndWaitForClusterInput, interval Interval) (*clusterv2.Cluster, error) {
	var cluster *clusterv2.Cluster
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
func GetClusterByName(ctx context.Context, input framework.GetClusterByNameInput) (*clusterv2.Cluster, error) {
	cluster := &clusterv2.Cluster{}
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
func WaitForClusterToProvision(ctx context.Context, input framework.WaitForClusterToProvisionInput, interval Interval) (*clusterv2.Cluster, error) {
	cluster := &clusterv2.Cluster{}
	fmt.Println("Waiting for cluster to enter the provisioned phase")

	err := wait.PollUntilContextTimeout(ctx, interval.tick, interval.timeout, true, func(ctx context.Context) (done bool, err error) {
		key := client.ObjectKey{
			Namespace: input.Cluster.GetNamespace(),
			Name:      input.Cluster.GetName(),
		}
		if err := input.Getter.Get(ctx, key, cluster); err != nil {
			return false, err
		}
		return cluster.Status.Phase == string(clusterv2.ClusterPhaseProvisioned), nil
	})
	if err != nil {
		return nil, fmt.Errorf("timed out waiting for Cluster %s to provision: %w", klog.KObj(input.Cluster), err)
	}

	return cluster, nil
}
