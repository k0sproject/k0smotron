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

// Package metrics holds the k0smotron-specific Prometheus metrics. They are
// registered against controller-runtime's registry so they are served by the
// manager's existing, authentication-protected metrics endpoint.
//
//nolint:revive
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/k0sproject/k0smotron/v2/internal/certs"
)

// Renewal results reported on the renewal counter.
const (
	RenewalResultSuccess = "success"
	RenewalResultError   = "error"
)

var (
	certificateExpiration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "k0smotron_certificate_expiration_timestamp_seconds",
			Help: "Expiry of a certificate managed by k0smotron, as a Unix timestamp in seconds.",
		},
		[]string{"namespace", "cluster", "kind", "purpose"},
	)

	certificateRenewals = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "k0smotron_certificate_renewal_total",
			Help: "Number of certificate renewals attempted by k0smotron, by result.",
		},
		[]string{"namespace", "cluster", "purpose", "result"},
	)
)

func init() {
	metrics.Registry.MustRegister(certificateExpiration, certificateRenewals)
}

// RecordExpiry publishes the expiry of a single certificate.
func RecordExpiry(namespace, cluster, kind string, i certs.Info) {
	certificateExpiration.
		WithLabelValues(namespace, cluster, kind, i.Purpose).
		Set(float64(i.NotAfter.Unix()))
}

// RecordRenewal counts a renewal attempt.
func RecordRenewal(namespace, cluster, purpose, result string) {
	certificateRenewals.WithLabelValues(namespace, cluster, purpose, result).Inc()
}

// ResetCluster drops every certificate expiry series belonging to one cluster.
// Called on cluster deletion so that a deleted cluster does not keep reporting
// a certificate that will never be renewed.
func ResetCluster(namespace, cluster string) {
	certificateExpiration.DeletePartialMatch(prometheus.Labels{
		"namespace": namespace,
		"cluster":   cluster,
	})
	certificateRenewals.DeletePartialMatch(prometheus.Labels{
		"namespace": namespace,
		"cluster":   cluster,
	})
}
