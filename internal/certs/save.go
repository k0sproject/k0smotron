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

package certs

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TLSKeyDataName is the secret key holding the PEM-encoded private key.
const TLSKeyDataName = "tls.key"

// SaveRenewed writes a re-signed keypair into an existing certificate secret.
//
// cluster-api's Certificates.SaveGenerated issues a bare Create and therefore
// fails with AlreadyExists once the secret is in place, which makes it unusable
// for renewal. This rewrites only the data keys, leaving labels, annotations and
// owner references untouched so that ownership and garbage collection behave
// exactly as they did before renewal.
// The read-modify-write is wrapped in a conflict retry: a stale informer read
// would otherwise let a concurrent writer's version win, silently discarding a
// renewal.
func SaveRenewed(ctx context.Context, c client.Client, key client.ObjectKey, crt, keyPEM []byte) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		s := &corev1.Secret{}
		if err := c.Get(ctx, key, s); err != nil {
			return err
		}

		if s.Data == nil {
			s.Data = map[string][]byte{}
		}
		s.Data[TLSCrtDataName] = crt
		s.Data[TLSKeyDataName] = keyPEM

		return c.Update(ctx, s)
	})
	if err != nil {
		return fmt.Errorf("saving renewed certificate secret %s: %w", key, err)
	}

	return nil
}
