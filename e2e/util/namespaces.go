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

	"github.com/k0sproject/k0smotron/internal/controller/util"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
)

// SetupSpecNamespace creates a namespace for the test spec and setups a watch for the namespace events.
func SetupSpecNamespace(ctx context.Context, specName string, clusterProxy capiframework.ClusterProxy, artifactFolder string) (*corev1.Namespace, context.CancelFunc) {
	fmt.Printf("Creating a namespace for hosting the %q test spec", specName)
	namespace, cancelWatches := framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
		Creator:   clusterProxy.GetClient(),
		ClientSet: clusterProxy.GetClientSet(),
		Name:      fmt.Sprintf("%s-%s", specName, util.RandomString(6)),
		LogFolder: filepath.Join(artifactFolder, "clusters", clusterProxy.GetName()),
	})

	return namespace, cancelWatches
}
