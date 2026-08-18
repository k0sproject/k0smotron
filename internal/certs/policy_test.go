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

package certs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigningPolicy_usesRequestedDuration(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	caNotAfter := now.Add(10 * 365 * 24 * time.Hour)

	p := SigningPolicy(48*time.Hour, caNotAfter, now)

	require.NotNil(t, p)
	require.NotNil(t, p.Default)
	assert.Equal(t, 48*time.Hour, p.Default.Expiry)
	assert.Contains(t, p.Default.Usage, "server auth")
	assert.Contains(t, p.Default.Usage, "client auth")
	assert.Contains(t, p.Default.Usage, "digital signature")
	assert.Contains(t, p.Default.Usage, "key encipherment")
}

func TestSigningPolicy_clampsToCAExpiry(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	// CA expires in 10 hours; a 48h leaf would outlive it.
	caNotAfter := now.Add(10 * time.Hour)

	p := SigningPolicy(48*time.Hour, caNotAfter, now)

	require.NotNil(t, p.Default)
	assert.Equal(t, 10*time.Hour, p.Default.Expiry)
}

func TestSigningPolicy_expiredCAYieldsPositiveExpiry(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	caNotAfter := now.Add(-time.Hour)

	p := SigningPolicy(48*time.Hour, caNotAfter, now)

	require.NotNil(t, p.Default)
	assert.Equal(t, time.Hour, p.Default.Expiry, "expired CA must produce exactly the floor expiry")
}

func TestSigningPolicy_zeroDurationFallsBackToDefault(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	caNotAfter := now.Add(10 * 365 * 24 * time.Hour)

	p := SigningPolicy(0, caNotAfter, now)

	require.NotNil(t, p.Default)
	assert.Equal(t, DefaultDuration, p.Default.Expiry)
}

func TestSigningPolicy_shortLivedCABeatsFloor(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	// CA is still valid but expires in 5 minutes, much shorter than minLeafExpiry.
	// The clamp must win, issuing a 5-minute leaf rather than the 1-hour floor.
	caNotAfter := now.Add(5 * time.Minute)

	p := SigningPolicy(48*time.Hour, caNotAfter, now)

	require.NotNil(t, p.Default)
	assert.Equal(t, 5*time.Minute, p.Default.Expiry, "clamp must not be overridden by floor while CA is still valid")
}

func TestSigningPolicy_zeroDurationWithShortLivedCA(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	// Zero duration falls back to DefaultDuration, but the clamp must still apply.
	caNotAfter := now.Add(5 * time.Minute)

	p := SigningPolicy(0, caNotAfter, now)

	require.NotNil(t, p.Default)
	assert.Equal(t, 5*time.Minute, p.Default.Expiry, "zero-duration fallback must be clamped to CA remaining lifetime")
}
