//go:build e2e

/*
Copyright 2026.

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

	cpv1beta1 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	clusterctlclient "sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// MoveClusterAndWaitInput is the input for MoveClusterAndWait.
type MoveClusterAndWaitInput struct {
	SourceProxy          capiframework.ClusterProxy
	TargetProxy          capiframework.ClusterProxy
	Cluster              *clusterv1.Cluster
	Namespace            string
	ArtifactFolder       string
	ClusterctlConfigPath string

	WaitForCluster      Interval
	WaitForControlPlane Interval
}

// MoveClusterAndWait runs `clusterctl move` from the source to the target management cluster, waits for the
// cluster and its control plane to be healthy in the target and checks that nothing is left in the source.
func MoveClusterAndWait(ctx context.Context, input MoveClusterAndWaitInput) error {
	if err := clusterctlMove(ctx, input); err != nil {
		return err
	}

	targetClient := input.TargetProxy.GetClient()

	cluster, err := DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    targetClient,
		Namespace: input.Namespace,
		Name:      input.Cluster.Name,
	}, input.WaitForCluster)
	if err != nil {
		return fmt.Errorf("cluster did not provision in the target management cluster: %w", err)
	}

	controlPlane, err := getK0sControlPlaneByCluster(ctx, GetK0sControlPlaneByClusterInput{
		Lister:      targetClient,
		ClusterName: cluster.Name,
		Namespace:   cluster.Namespace,
	})
	if err != nil {
		return err
	}
	if controlPlane == nil {
		return fmt.Errorf("no K0sControlPlane found in the target management cluster for cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	if err := WaitForControlPlaneReplicasWithDesiredState(ctx, targetClient, controlPlane, input.WaitForControlPlane); err != nil {
		return fmt.Errorf("control plane is not ready in the target management cluster: %w", err)
	}

	return verifyClusterGoneFromSource(ctx, input.SourceProxy.GetClient(), input.Namespace)
}

// clusterctlMove runs `clusterctl move` through the clusterctl client library. The framework's clusterctl.Move
// is not used because it calls ginkgo's By, which is not allowed outside of a running ginkgo spec.
func clusterctlMove(ctx context.Context, input MoveClusterAndWaitInput) error {
	c, err := clusterctlclient.New(ctx, input.ClusterctlConfigPath)
	if err != nil {
		return fmt.Errorf("failed to create the clusterctl client: %w", err)
	}

	fmt.Printf("clusterctl move --from-kubeconfig %s --to-kubeconfig %s --namespace %s\n",
		input.SourceProxy.GetKubeconfigPath(), input.TargetProxy.GetKubeconfigPath(), input.Namespace)
	if err := c.Move(ctx, clusterctlclient.MoveOptions{
		FromKubeconfig: clusterctlclient.Kubeconfig{Path: input.SourceProxy.GetKubeconfigPath()},
		ToKubeconfig:   clusterctlclient.Kubeconfig{Path: input.TargetProxy.GetKubeconfigPath()},
		Namespace:      input.Namespace,
	}); err != nil {
		return fmt.Errorf("clusterctl move failed: %w", err)
	}

	return nil
}

func verifyClusterGoneFromSource(ctx context.Context, c crclient.Client, namespace string) error {
	clusters := &clusterv1.ClusterList{}
	if err := c.List(ctx, clusters, crclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list Clusters in the source management cluster: %w", err)
	}
	if len(clusters.Items) > 0 {
		return fmt.Errorf("%d Cluster object(s) left in source namespace %s after move", len(clusters.Items), namespace)
	}

	controlPlanes := &cpv1beta1.K0sControlPlaneList{}
	if err := c.List(ctx, controlPlanes, crclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list K0sControlPlanes in the source management cluster: %w", err)
	}
	if len(controlPlanes.Items) > 0 {
		return fmt.Errorf("%d K0sControlPlane object(s) left in source namespace %s after move", len(controlPlanes.Items), namespace)
	}

	return nil
}
