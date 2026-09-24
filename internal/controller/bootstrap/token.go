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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultTokenTTL is the default TTL used for bootstrap tokens.
	DefaultTokenTTL = 24 * time.Hour

	// bootstrapTokenIDAnnotation records on the bootstrap config the ID of the bootstrap token embedded in its
	// bootstrap data, so the token can be refreshed until the machine joins.
	bootstrapTokenIDAnnotation = "k0smotron.io/bootstrap-token-id"
)

// tokenRefreshJitterFactor is the maximum fraction of the refresh interval added as jitter.
const tokenRefreshJitterFactor = 0.1

// tokenCheckRefreshInterval defines when to trigger a reconciliation loop again to refresh a token.
// `ttl / 3` means reconciliation gets triggered at least 3 times within the expiry time of the token, allowing for one
// temporary failure before the token expires.
// Up to 10% of jitter is added so tokens created together do not all get refreshed at the same time.
func tokenCheckRefreshInterval(ttl time.Duration) time.Duration {
	return wait.Jitter(ttl/3, tokenRefreshJitterFactor)
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

// refreshBootstrapTokenIfNeeded extends the expiration of the given bootstrap token if it is close to expire.
func refreshBootstrapTokenIfNeeded(ctx context.Context, c client.Client, tokenID string, ttl time.Duration) (bool, error) {
	s, err := getTokenSecret(ctx, c, tokenID)
	if err != nil {
		return false, fmt.Errorf("failed to get bootstrap token secret in order to refresh it: %w", err)
	}

	oldExpiration := string(s.Data[bootstrapapi.BootstrapTokenExpirationKey])
	if oldExpiration != "" {
		expiration, err := time.Parse(time.RFC3339, oldExpiration)
		if err != nil {
			return false, fmt.Errorf("can't parse expiration time of bootstrap token: %w", err)
		}
		if expiration.After(time.Now().Add(skipTokenRefreshIfExpiringAfter(ttl))) {
			return false, nil
		}
	}

	newExpiration := tokenExpiration(ttl)
	if s.Data == nil {
		s.Data = map[string][]byte{}
	}
	s.Data[bootstrapapi.BootstrapTokenExpirationKey] = []byte(newExpiration)
	if err := c.Update(ctx, s); err != nil {
		return false, fmt.Errorf("failed to refresh bootstrap token: %w", err)
	}
	return true, nil
}
