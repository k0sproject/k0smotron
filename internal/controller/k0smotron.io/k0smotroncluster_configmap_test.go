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

import (
	"strings"
	"testing"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGenerateCM(t *testing.T) {
	r := ClusterReconciler{
		Scheme: &runtime.Scheme{},
	}
	t.Run("config merge", func(t *testing.T) {
		kmc := km.Cluster{
			Spec: km.ClusterSpec{
				ExternalAddress: "my.external.address",
				K0sConfig: &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "k0s.k0sproject.io/v1beta1",
					"kind":       "ClusterConfig",
					"spec": map[string]interface{}{
						"network": map[string]interface{}{
							"provider": "calico",
						},
					},
				}},
			},
		}

		cm, err := r.generateCM(&kmc)
		require.NoError(t, err)

		conf := cm.Data["K0SMOTRON_K0S_YAML"]

		assert.True(t, strings.Contains(conf, "my.external.address"), "The external address must be my.external.address")
		assert.True(t, strings.Contains(conf, "calico"), "The provider must be calico")
		assert.False(t, strings.Contains(conf, "kuberouter"), "The provider must not be kuberouter")
	})
}
