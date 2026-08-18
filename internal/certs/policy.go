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
	"time"

	"github.com/cloudflare/cfssl/config"
)

const (
	// DefaultDuration is the validity period k0smotron requests for the leaf
	// certificates it signs. It matches the value cfssl's own DefaultConfig
	// used to supply implicitly, so existing clusters see no change.
	DefaultDuration = 8760 * time.Hour

	// DefaultRenewBefore is how long before expiry a certificate is renewed.
	DefaultRenewBefore = 720 * time.Hour

	// minLeafExpiry is the floor applied when the CA is already expired or is
	// about to be. Signing with a zero or negative expiry produces a
	// certificate that is invalid on arrival.
	minLeafExpiry = time.Hour
)

// SigningPolicy builds an explicit cfssl signing policy.
//
// Passing a nil policy to cfssl's local.NewSigner makes it fall back to
// config.DefaultConfig(), whose expiry is an implementation detail of the
// vendored library rather than a decision k0smotron makes. This function makes
// the decision explicit.
//
// The leaf expiry is clamped so that a leaf can never outlive its issuing CA.
func SigningPolicy(duration time.Duration, caNotAfter time.Time, now time.Time) *config.Signing {
	if duration <= 0 {
		duration = DefaultDuration
	}

	// The clamp always wins while the CA is still valid, even when that means a
	// very short-lived leaf: a leaf that outlives its issuer is invalid the
	// moment the CA expires. The floor engages only once the CA is already
	// expired, where every leaf is invalid anyway and the only remaining goal is
	// to avoid handing cfssl a zero or negative expiry.
	if remaining := caNotAfter.Sub(now); remaining < duration {
		duration = remaining
	}
	if duration <= 0 {
		duration = minLeafExpiry
	}

	return &config.Signing{
		Default: &config.SigningProfile{
			Expiry:       duration,
			ExpiryString: duration.String(),
			Usage: []string{
				"signing",
				"key encipherment",
				"digital signature",
				"server auth",
				"client auth",
			},
		},
	}
}
