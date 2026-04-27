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

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"k8s.io/client-go/rest"
	clusterinventoryapi "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/access"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func restConfigFromClusterProfileRef(ctx context.Context, hubClient client.Client, clusterProfileRef *kapi.ClusterProfileRef, accessCfg *access.Config) (*rest.Config, error) {
	clusterProfile := clusterinventoryapi.ClusterProfile{}
	err := hubClient.Get(ctx, client.ObjectKey{Name: clusterProfileRef.Name, Namespace: clusterProfileRef.Namespace}, &clusterProfile)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster profile: %w", err)
	}

	restConfigForMyCluster, err := accessCfg.BuildConfigFromCP(&clusterProfile)
	if err != nil {
		return nil, fmt.Errorf("failed to generate restConfig: %w", err)
	}

	return restConfigForMyCluster, nil
}
