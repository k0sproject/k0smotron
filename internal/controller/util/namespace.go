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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// EnsureNamespaceExists ensures that a namespace exists in the remote cluster.
// It first checks if the namespace exists by calling Get. If the namespace is not found,
// it creates the namespace. If Get returns Forbidden, the namespace is assumed to exist.
func EnsureNamespaceExists(ctx context.Context, c client.Client, namespace string) error {
	ns := &corev1.Namespace{}
	key := client.ObjectKey{Name: namespace}

	err := c.Get(ctx, key, ns)
	if err == nil {
		// Namespace exists, we're done
		return nil
	}

	if apierrors.IsNotFound(err) {
		// Namespace doesn't exist, create it
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		createErr := c.Create(ctx, ns)
		if createErr != nil {
			if apierrors.IsAlreadyExists(createErr) {
				// Namespace was created by another process, that's fine
				return nil
			}
			return fmt.Errorf("failed to create namespace %s: %w", namespace, createErr)
		}
		return nil
	}

	if apierrors.IsForbidden(err) {
		// The identity may be scoped to existing namespaces only. Assume the namespace
		// exists; if it does not, the next namespaced create fails with a clear error.
		log.FromContext(ctx).Info("Cannot get namespace in remote cluster, assuming it exists", "namespace", namespace, "reason", err.Error())
		return nil
	}

	// Other errors are returned as-is
	return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
}
