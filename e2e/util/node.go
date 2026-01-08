package util

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/util/patch"
)

// PatchNodeConditionInput is the input for PatchNodeCondition.
type PatchNodeConditionInput struct {
	ClusterProxy  framework.ClusterProxy
	Cluster       *clusterv1.Cluster
	NodeCondition corev1.NodeCondition
	Machine       clusterv1.Machine
}

// PatchNodeCondition patches a node condition to any one of the machines with a node ref.
func PatchNodeCondition(ctx context.Context, input PatchNodeConditionInput) error {
	if input.ClusterProxy == nil {
		return fmt.Errorf("failed to patch node condition: input.ClusterProxy is nil")
	}
	if input.Cluster == nil {
		return fmt.Errorf("failed to patch node condition: input.Cluster is nil")
	}
	if input.Machine.Status.NodeRef == nil {
		return fmt.Errorf("failed to patch node condition: machine %s/%s does not have a node ref", input.Machine.Namespace, input.Machine.Name)
	}

	fmt.Println("Patching the node condition to the node")

	workloadClient, err := getWorkloadClusterClient(ctx, input.ClusterProxy, input.Cluster)
	if err != nil {
		return err
	}

	node := &corev1.Node{}
	err = workloadClient.Get(ctx, types.NamespacedName{Name: input.Machine.Status.NodeRef.Name, Namespace: input.Machine.Status.NodeRef.Namespace}, node)
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", input.Machine.Status.NodeRef.Name, err)
	}

	patchHelper, err := patch.NewHelper(node, workloadClient)
	if err != nil {
		return fmt.Errorf("failed to create patch helper for node %s: %w", input.Machine.Status.NodeRef.Name, err)
	}

	node.Status.Conditions = append(node.Status.Conditions, input.NodeCondition)
	err = patchHelper.Patch(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to patch node %s: %w", input.Machine.Status.NodeRef.Name, err)
	}

	return nil
}
