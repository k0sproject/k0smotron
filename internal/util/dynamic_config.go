package util

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ReconcileDynamicConfig(ctx context.Context, cluster metav1.Object, cli client.Client, u *unstructured.Unstructured) error {
	u.SetName("k0s")
	u.SetNamespace("kube-system")

	b, err := u.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal unstructured config: %w", err)
	}

	chCS, err := remote.NewClusterClient(ctx, "k0smotron", cli, util.ObjectKey(cluster))
	if err != nil {
		return fmt.Errorf("failed to create workload cluster client: %w", err)
	}

	err = chCS.Patch(ctx, u, client.RawPatch(client.Merge.Type(), b), []client.PatchOption{}...)
	if err != nil {
		return fmt.Errorf("failed to patch k0s config: %w", err)
	}

	return nil
}
