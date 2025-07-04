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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteClusterAndWait deletes a cluster object and waits for it to be gone.
func deleteClusterAndWait(ctx context.Context, input capiframework.DeleteClusterAndWaitInput, interval Interval) error {

	err := input.Client.Delete(ctx, input.Cluster)
	if err != nil {
		return fmt.Errorf("error deleting cluster %s: %w", input.Cluster.Name, err)
	}

	fmt.Printf("Waiting for the Cluster %s to be deleted\n", klog.KObj(input.Cluster))
	waitForClusterDeleted(ctx, capiframework.WaitForClusterDeletedInput{
		Client:         input.Client,
		Cluster:        input.Cluster,
		ArtifactFolder: input.ArtifactFolder,
	}, interval)

	return nil
}

// waitForClusterDeleted waits until the cluster object has been deleted.
func waitForClusterDeleted(ctx context.Context, input capiframework.WaitForClusterDeletedInput, interval Interval) {
	clusterName := input.Cluster.GetName()
	clusterNamespace := input.Cluster.GetNamespace()
	// Note: dumpArtifactsOnDeletionTimeout is passed in as a func so it gets only executed if and after the Eventually failed.
	err := wait.PollUntilContextTimeout(ctx, interval.tick, interval.timeout, true, func(ctx context.Context) (done bool, err error) {
		cluster := &clusterv1.Cluster{}
		key := client.ObjectKey{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}

		return apierrors.IsNotFound(input.Client.Get(ctx, key, cluster)), nil
	})
	if err != nil {
		if input.ArtifactFolder != "" {
			// Dump all Cluster API related resources to artifacts.
			capiframework.DumpAllResources(ctx, capiframework.DumpAllResourcesInput{
				Lister:    input.Client,
				Namespace: clusterNamespace,
				LogPath:   filepath.Join(input.ArtifactFolder, "clusters-afterDeletionTimedOut", clusterName, "resources"),
			})
		}

		fmt.Println("waiting for cluster deletion timed out")
	}
}
