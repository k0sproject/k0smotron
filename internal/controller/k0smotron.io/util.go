/*
Copyright 2023.

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

package k0smotronio

import km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"

func labelsForCluster(kmc *km.Cluster) map[string]string {
	labels := map[string]string{"app": "k0smotron", "cluster": kmc.Name}
	for k, v := range kmc.Labels {
		labels[k] = v
	}
	return labels
}

func annotationsForCluster(kmc *km.Cluster) map[string]string {
	return kmc.Annotations
}
