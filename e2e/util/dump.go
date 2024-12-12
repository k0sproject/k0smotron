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
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DumpSpecResourcesAndCleanup dumps all the resources in the spec namespace and cleans up the spec namespace.
func DumpSpecResourcesAndCleanup(ctx context.Context, specName string, clusterProxy capiframework.ClusterProxy, artifactFolder string, namespace *corev1.Namespace, cancelWatches context.CancelFunc, cluster *clusterv1.Cluster, interval Interval, skipCleanup bool) {
	// Dump all the resources in the spec namespace and the workload cluster.
	dumpAllResourcesAndLogs(ctx, clusterProxy, artifactFolder, namespace, cluster)

	if !skipCleanup {
		err := deleteClusterAndWait(ctx, capiframework.DeleteClusterAndWaitInput{
			Client:         clusterProxy.GetClient(),
			Cluster:        cluster,
			ArtifactFolder: artifactFolder,
		}, interval)
		if err != nil {
			fmt.Println(err.Error())
		}

		capiframework.DeleteNamespace(ctx, capiframework.DeleteNamespaceInput{
			Deleter: clusterProxy.GetClient(),
			Name:    namespace.Name,
		})
	}
	cancelWatches()
}

// dumpAllResourcesAndLogs dumps all the resources in the spec namespace and the workload cluster.
func dumpAllResourcesAndLogs(ctx context.Context, clusterProxy capiframework.ClusterProxy, artifactFolder string, namespace *corev1.Namespace, cluster *clusterv1.Cluster) {
	// Dump all the logs from the workload cluster.
	clusterProxy.CollectWorkloadClusterLogs(ctx, cluster.Namespace, cluster.Name, filepath.Join(artifactFolder, "clusters", cluster.Name))

	// Dump all Cluster API related resources to artifacts.
	capiframework.DumpAllResources(ctx, capiframework.DumpAllResourcesInput{
		Lister:    clusterProxy.GetClient(),
		Namespace: namespace.Name,
		LogPath:   filepath.Join(artifactFolder, "clusters", clusterProxy.GetName(), "resources"),
	})

	// If the cluster still exists, dump pods and nodes of the workload cluster.
	if err := clusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), &clusterv1.Cluster{}); err == nil {
		capiframework.DumpResourcesForCluster(ctx, capiframework.DumpResourcesForClusterInput{
			Lister:  clusterProxy.GetWorkloadCluster(ctx, cluster.Namespace, cluster.Name).GetClient(),
			Cluster: cluster,
			LogPath: filepath.Join(artifactFolder, "clusters", cluster.Name, "resources"),
			Resources: []capiframework.DumpNamespaceAndGVK{
				{
					GVK: schema.GroupVersionKind{
						Version: corev1.SchemeGroupVersion.Version,
						Kind:    "Pod",
					},
				},
				{
					GVK: schema.GroupVersionKind{
						Version: corev1.SchemeGroupVersion.Version,
						Kind:    "Node",
					},
				},
			},
		})
	}
}
