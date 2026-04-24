package util

import (
	"context"
	"fmt"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"k8s.io/client-go/rest"
	clusterinventoryapi "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/access"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func restConfigFromClusterProfileRef(ctx context.Context, hubClient client.Client, clusterProfileRef *kapi.ClusterProfileRef) (*rest.Config, error) {
	clusterProfile := clusterinventoryapi.ClusterProfile{}
	err := hubClient.Get(ctx, client.ObjectKey{Name: clusterProfileRef.Name, Namespace: clusterProfileRef.Namespace}, &clusterProfile)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster profile: %w", err)
	}

	accessCfg := access.New(clusterProfileRef.AccessProviders)
	if accessCfg == nil {
		return nil, fmt.Errorf("no access provider configured for cluster profile %s/%s", clusterProfileRef.Namespace, clusterProfileRef.Name)
	}

	restConfigForMyCluster, err := accessCfg.BuildConfigFromCP(&clusterProfile)
	if err != nil {
		return nil, fmt.Errorf("failed to generate restConfig: %w", err)
	}

	return restConfigForMyCluster, nil
}
