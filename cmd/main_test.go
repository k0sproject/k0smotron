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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestShouldRunCAPIControllers(t *testing.T) {
	tests := []struct {
		name      string
		resources []*metav1.APIResourceList
		want      bool
	}{
		{
			name:      "no cluster-api resources installed",
			resources: nil,
			want:      false,
		},
		{
			// Reproduces the bug reported for a standalone install where only
			// outdated v1beta1 Cluster API CRDs are present: the manager only
			// watches v1beta2 types, so it must not enable the CAPI controllers.
			name: "only outdated v1beta1 cluster-api CRDs installed",
			resources: []*metav1.APIResourceList{
				{
					GroupVersion: "cluster.x-k8s.io/v1beta1",
					APIResources: []metav1.APIResource{{Name: "clusters", Kind: "Cluster"}},
				},
			},
			want: false,
		},
		{
			name: "v1beta2 cluster-api CRDs installed",
			resources: []*metav1.APIResourceList{
				{
					GroupVersion: "cluster.x-k8s.io/v1beta2",
					APIResources: []metav1.APIResource{{Name: "clusters", Kind: "Cluster"}},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discoveryClient := &fakediscovery.FakeDiscovery{
				Fake: &clienttesting.Fake{Resources: tt.resources},
			}

			got := shouldRunCAPIControllers(discoveryClient, logf.Log)

			assert.Equal(t, tt.want, got)
		})
	}
}
