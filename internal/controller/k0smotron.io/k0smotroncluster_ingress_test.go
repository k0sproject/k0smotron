//go:build !envtest

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

package k0smotronio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

func TestOverrideImageRepository(t *testing.T) {
	t.Run("empty repository returns original image", func(t *testing.T) {
		assert.Equal(t, "quay.io/k0sproject/foo:v1", overrideImageRepository("", "quay.io/k0sproject/foo:v1"))
	})

	t.Run("repository without path", func(t *testing.T) {
		repo := "my.registry"
		cases := []struct {
			input    string
			expected string
		}{
			{"repo/image", "my.registry/repo/image"},
			{"registry.com/repo/image", "my.registry/repo/image"},
			{"image", "my.registry/image"},
		}
		for _, tc := range cases {
			assert.Equal(t, tc.expected, overrideImageRepository(repo, tc.input), "input: %s", tc.input)
		}
	})

	t.Run("repository with path", func(t *testing.T) {
		repo := "my.registry/foo"
		cases := []struct {
			input    string
			expected string
		}{
			{"repo/image", "my.registry/foo/repo/image"},
			{"registry.com/repo/image", "my.registry/foo/repo/image"},
			{"image", "my.registry/foo/image"},
		}
		for _, tc := range cases {
			assert.Equal(t, tc.expected, overrideImageRepository(repo, tc.input), "input: %s", tc.input)
		}
	})

	t.Run("idempotent: double application is a no-op", func(t *testing.T) {
		repo := "my.registry/foo"
		cases := []string{"repo/image", "registry.com/repo/image", "image"}
		for _, input := range cases {
			once := overrideImageRepository(repo, input)
			twice := overrideImageRepository(repo, once)
			assert.Equal(t, once, twice, "input: %s", input)
		}
	})
}

func TestGetKonnectivityAgentImage(t *testing.T) {
	scope := &kmcScope{}

	t.Run("no k0sConfig returns default image", func(t *testing.T) {
		kmc := &km.Cluster{}
		assert.Equal(t, "quay.io/k0sproject/apiserver-network-proxy-agent:v0.33.0", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("custom image and version override", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"konnectivity": map[string]any{
									"image":   "custom-repo/my-konnectivity",
									"version": "v0.0.1",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "custom-repo/my-konnectivity:v0.0.1", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("custom image only, no version appended", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"konnectivity": map[string]any{
									"image": "custom-repo/my-konnectivity",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "custom-repo/my-konnectivity", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("repository replaces registry host in default image", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"repository": "my.repo",
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "my.repo/k0sproject/apiserver-network-proxy-agent:v0.33.0", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("repository also applied when custom image is set", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"repository": "my.repo",
								"konnectivity": map[string]any{
									"image":   "my-custom-image",
									"version": "v0.0.1",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "my.repo/my-custom-image:v0.0.1", scope.getKonnectivityAgentImage(kmc))
	})
}

func TestGetKonnectivityAgentPullPolicy(t *testing.T) {
	scope := &kmcScope{}

	t.Run("no k0sConfig returns IfNotPresent", func(t *testing.T) {
		kmc := &km.Cluster{}
		assert.Equal(t, "IfNotPresent", scope.getKonnectivityAgentPullPolicy(kmc))
	})

	t.Run("empty defaultPullPolicy returns IfNotPresent", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{},
					},
				},
			},
		}
		assert.Equal(t, "IfNotPresent", scope.getKonnectivityAgentPullPolicy(kmc))
	})

	t.Run("default_pull_policy Never is respected", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"default_pull_policy": "Never",
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "Never", scope.getKonnectivityAgentPullPolicy(kmc))
	})

	t.Run("default_pull_policy Always is respected", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"default_pull_policy": "Always",
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "Always", scope.getKonnectivityAgentPullPolicy(kmc))
	})
}
