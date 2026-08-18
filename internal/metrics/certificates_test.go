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

// The package name deliberately shadows the standard library's runtime/metrics;
// see the package comment in certificates.go.
//
//nolint:revive
package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/k0sproject/k0smotron/v2/internal/certs"
)

func TestRecordExpiry(t *testing.T) {
	certificateExpiration.Reset()

	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	RecordExpiry("default", "kmc", "Cluster", certs.Info{Purpose: "etcd-server", NotAfter: notAfter})

	// Compare the value, not the rendered text: Prometheus trims trailing zeros
	// when formatting floats, so an exact-text expectation is needlessly brittle.
	assert.Equal(t, float64(notAfter.Unix()), testutil.ToFloat64(
		certificateExpiration.WithLabelValues("default", "kmc", "Cluster", "etcd-server")))
	assert.Equal(t, 1, testutil.CollectAndCount(certificateExpiration))
}

func TestRecordRenewal(t *testing.T) {
	certificateRenewals.Reset()

	RecordRenewal("default", "kmc", "etcd-server", RenewalResultSuccess)
	RecordRenewal("default", "kmc", "etcd-server", RenewalResultSuccess)
	RecordRenewal("default", "kmc", "etcd-peer", RenewalResultError)

	assert.Equal(t, float64(2), testutil.ToFloat64(
		certificateRenewals.WithLabelValues("default", "kmc", "etcd-server", RenewalResultSuccess)))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		certificateRenewals.WithLabelValues("default", "kmc", "etcd-peer", RenewalResultError)))
}

func TestResetCluster(t *testing.T) {
	certificateExpiration.Reset()
	certificateRenewals.Reset()

	// Record expiry for two clusters in the default namespace
	kmcExpiry := time.Now().Add(2 * time.Hour)
	otherExpiry := time.Now().Add(3 * time.Hour)
	RecordExpiry("default", "kmc", "Cluster",
		certs.Info{Purpose: "etcd-server", NotAfter: kmcExpiry})
	RecordExpiry("default", "other", "Cluster",
		certs.Info{Purpose: "etcd-server", NotAfter: otherExpiry})

	// Record renewals for both clusters
	RecordRenewal("default", "kmc", "etcd-server", RenewalResultSuccess)
	RecordRenewal("default", "other", "etcd-server", RenewalResultSuccess)

	require.Equal(t, 2, testutil.CollectAndCount(certificateExpiration))
	require.Equal(t, 2, testutil.CollectAndCount(certificateRenewals))

	// Reset only kmc in default namespace
	ResetCluster("default", "kmc")

	// Verify the right series was deleted: count is 1
	assert.Equal(t, 1, testutil.CollectAndCount(certificateExpiration),
		"deleting a cluster must drop only that cluster's series")
	assert.Equal(t, 1, testutil.CollectAndCount(certificateRenewals),
		"deleting a cluster must drop only that cluster's renewal records")

	// Verify the surviving expiry series is from the "other" cluster, not "kmc"
	assert.Equal(t, float64(otherExpiry.Unix()), testutil.ToFloat64(
		certificateExpiration.WithLabelValues("default", "other", "Cluster", "etcd-server")),
		"the wrong cluster's series was deleted")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		certificateRenewals.WithLabelValues("default", "other", "etcd-server", RenewalResultSuccess)),
		"the wrong cluster's renewal record was deleted")

	// Test cross-namespace case: same cluster name in a different namespace should survive
	otherNsExpiry := time.Now().Add(4 * time.Hour)
	RecordExpiry("other-ns", "kmc", "Cluster",
		certs.Info{Purpose: "etcd-server", NotAfter: otherNsExpiry})
	RecordRenewal("other-ns", "kmc", "etcd-server", RenewalResultSuccess)

	// Reset default/kmc again (should be a no-op for expiry, but clear any new renewals)
	ResetCluster("default", "kmc")

	// Verify other-ns/kmc series survived
	assert.Equal(t, float64(otherNsExpiry.Unix()), testutil.ToFloat64(
		certificateExpiration.WithLabelValues("other-ns", "kmc", "Cluster", "etcd-server")),
		"deleting a cluster in one namespace must not affect same cluster name in another namespace")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		certificateRenewals.WithLabelValues("other-ns", "kmc", "etcd-server", RenewalResultSuccess)),
		"deleting a cluster in one namespace must not affect renewals for same cluster name in another namespace")
}
