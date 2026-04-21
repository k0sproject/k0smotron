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
//nolint:revive
package util

import (
	"context"
	"fmt"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	kutil "github.com/k0sproject/k0smotron/internal/controller/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileDynamicConfig updates the k0s ClusterConfig with the provided unstructured configuration.
func ReconcileDynamicConfig(ctx context.Context, cluster client.ObjectKey, cli client.Client, u unstructured.Unstructured, kcp *cpv1beta2.K0sControlPlane) error {
	u.SetName("k0s")
	u.SetNamespace("kube-system")

	// Remove fields that can be configured only via the local k0s config
	// See: https://docs.k0sproject.io/stable/dynamic-configuration/#cluster-configuration-vs-controller-node-configuration
	//unstructured.RemoveNestedField(u.Object, "spec", "api") // This field is not really should be removed, requires some investigation on the k0s side
	unstructured.RemoveNestedField(u.Object, "spec", "storage")
	unstructured.RemoveNestedField(u.Object, "spec", "network", "controlPlaneLoadBalancing")

	b, err := u.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal unstructured config: %w", err)
	}

	chCS, err := kutil.GetControllerRuntimeClient(ctx, cli, kcp, cluster)
	if err != nil {
		return fmt.Errorf("failed to create workload cluster client: %w", err)
	}

	err = retry.OnError(wait.Backoff{
		Steps:    2,
		Duration: 100 * time.Millisecond,
		Factor:   5.0,
		Jitter:   0.5,
	}, func(_ error) bool {
		return true
	}, func() error {
		return chCS.Patch(ctx, &u, client.RawPatch(client.Merge.Type(), b), []client.PatchOption{}...)
	})
	if err != nil {
		return fmt.Errorf("failed to patch k0s config: %w", err)
	}

	return nil
}
