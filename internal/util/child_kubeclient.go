/*


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

	"k8s.io/client-go/kubernetes/scheme"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const K0smotronController = "k0smotron-controlplane-controller"

func LoadChildClusterKubeClient(ctx context.Context, cluster *capi.Cluster, c client.Client) (client.Client, error) {
	// remote.RESTConfig(ctx context.Context, sourceName string, c client.Reader, cluster types.NamespacedName)
	restConfig, err := remote.RESTConfig(ctx, K0smotronController, c, util.ObjectKey(cluster))
	if err != nil {
		return nil, err
	}

	childClient, err := client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client for cluster %s: %w", cluster.Name, err)
	}

	return childClient, nil
}
