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

package bootstrap

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultTokenTTL is the default TTL used for bootstrap tokens.
	DefaultTokenTTL = 24 * time.Hour

	// bootstrapTokenIDAnnotation records on the bootstrap config the ID of the bootstrap token embedded in its
	// bootstrap data, so the token can be refreshed until the machine joins or rotated for MachinePools.
	bootstrapTokenIDAnnotation = "k0smotron.io/bootstrap-token-id"
)

// tokenCheckRefreshOrRotationInterval defines when to trigger a reconciliation loop again to refresh or rotate a token.
// `ttl / 3` means reconciliation gets triggered at least 3 times within the expiry time of the token, allowing for one
// temporary failure before the token expires.
func tokenCheckRefreshOrRotationInterval(ttl time.Duration) time.Duration {
	return ttl / 3
}

// skipTokenRefreshIfExpiringAfter returns a duration. If the token's expiry timestamp is after
// `now + skipTokenRefreshIfExpiringAfter()`, it does not yet need a refresh.
func skipTokenRefreshIfExpiringAfter(ttl time.Duration) time.Duration {
	return ttl * 5 / 6
}

func tokenExpiration(ttl time.Duration) string {
	return time.Now().UTC().Add(ttl).Format(time.RFC3339)
}

func getTokenSecret(ctx context.Context, c client.Client, tokenID string) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	key := client.ObjectKey{Name: bootstraputil.BootstrapTokenSecretName(tokenID), Namespace: metav1.NamespaceSystem}
	if err := c.Get(ctx, key, s); err != nil {
		return nil, err
	}
	return s, nil
}

// refreshBootstrapToken extends the expiration of the given bootstrap token if it is close to expire.
func refreshBootstrapToken(ctx context.Context, c client.Client, tokenID string, ttl time.Duration) error {
	log := log.FromContext(ctx).WithValues("tokenID", tokenID)

	s, err := getTokenSecret(ctx, c, tokenID)
	if err != nil {
		return fmt.Errorf("failed to get bootstrap token secret in order to refresh it: %w", err)
	}

	oldExpiration := string(s.Data[bootstrapapi.BootstrapTokenExpirationKey])
	if oldExpiration != "" {
		expiration, err := time.Parse(time.RFC3339, oldExpiration)
		if err != nil {
			return fmt.Errorf("can't parse expiration time of bootstrap token: %w", err)
		}
		if expiration.After(time.Now().UTC().Add(skipTokenRefreshIfExpiringAfter(ttl))) {
			log.V(3).Info("Token needs no refresh", "expiration", oldExpiration)
			return nil
		}
	}

	newExpiration := tokenExpiration(ttl)
	if s.Data == nil {
		s.Data = map[string][]byte{}
	}
	s.Data[bootstrapapi.BootstrapTokenExpirationKey] = []byte(newExpiration)
	log.Info("Refreshing token until the machine has a chance to consume it", "oldExpiration", oldExpiration, "newExpiration", newExpiration)
	if err := c.Update(ctx, s); err != nil {
		return fmt.Errorf("failed to refresh bootstrap token: %w", err)
	}
	return nil
}

// shouldRotateBootstrapToken returns true if the given token is missing or past half of its TTL.
func shouldRotateBootstrapToken(ctx context.Context, c client.Client, tokenID string, ttl time.Duration) (bool, error) {
	s, err := getTokenSecret(ctx, c, tokenID)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	expiration, err := time.Parse(time.RFC3339, string(s.Data[bootstrapapi.BootstrapTokenExpirationKey]))
	if err != nil {
		return false, err
	}
	return expiration.Before(time.Now().UTC().Add(ttl / 2)), nil
}
