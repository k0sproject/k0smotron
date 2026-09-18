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

// Package report turns certificate inspection results into the conditions and
// metrics every k0smotron controller publishes, so that all three cluster types
// report certificate state identically.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/certs"
	"github.com/k0sproject/k0smotron/v2/internal/metrics"
)

// Info is re-exported so callers need only import this package.
type Info = certs.Info

// Status carries the two certificate conditions.
type Status struct {
	// Available has positive polarity: True means every managed certificate is
	// present and unexpired.
	Available metav1.Condition
	// Expiring has negative polarity: True means at least one certificate is in
	// its renewal window or already expired.
	Expiring metav1.Condition
}

// Build constructs both certificate conditions from inspection results.
// unparseable holds the purposes of secrets that could not be read; those are
// reported as Unknown rather than assumed healthy, because assuming health on
// an unreadable certificate is the failure mode this work exists to remove.
func Build(is []Info, unparseable []string, renewBefore time.Duration, now time.Time) Status {
	var expired, due []string
	for _, i := range is {
		switch {
		case !now.Before(i.NotAfter):
			expired = append(expired, i.Purpose)
		case certs.NeedsRenewal(i, renewBefore, now):
			due = append(due, fmt.Sprintf("%s (in %s)", i.Purpose, i.NotAfter.Sub(now).Round(time.Hour)))
		}
	}
	sort.Strings(expired)
	sort.Strings(due)

	return Status{
		Available: buildAvailable(is, unparseable, expired),
		Expiring:  buildExpiring(expired, due, unparseable),
	}
}

func buildAvailable(is []Info, unparseable, expired []string) metav1.Condition {
	switch {
	// Expired outranks unparseable deliberately. An expired certificate is a
	// proven failure; an unreadable one is only an unknown. Reporting Unknown
	// when we can already prove something is broken would suppress an alert
	// that should fire.
	case len(expired) > 0:
		return metav1.Condition{
			Type:    km.ClusterCertificatesAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  km.ClusterCertificatesExpiredReason,
			Message: fmt.Sprintf("Expired certificates: %s", strings.Join(expired, ", ")),
		}
	case len(unparseable) > 0:
		// Copy before sorting: the slice belongs to the caller.
		sorted := append([]string(nil), unparseable...)
		sort.Strings(sorted)
		return metav1.Condition{
			Type:    km.ClusterCertificatesAvailableCondition,
			Status:  metav1.ConditionUnknown,
			Reason:  km.InternalErrorReason,
			Message: fmt.Sprintf("Unable to read certificates: %s", strings.Join(sorted, ", ")),
		}
	case len(is) == 0:
		return metav1.Condition{
			Type:    km.ClusterCertificatesAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  km.NotFoundReason,
			Message: "No certificates found for this cluster",
		}
	default:
		return metav1.Condition{
			Type:   km.ClusterCertificatesAvailableCondition,
			Status: metav1.ConditionTrue,
			Reason: km.CreatedReason,
		}
	}
}

// buildExpiring mirrors buildAvailable's precedence: expired outranks due, and
// both outrank unparseable. An unreadable certificate must never yield
// False/Valid - "not expiring" is a claim we cannot make about a certificate we
// cannot read, and reporting it would be exactly the reassuring lie this
// package exists to remove. Both conditions therefore go Unknown together.
func buildExpiring(expired, due, unparseable []string) metav1.Condition {
	switch {
	case len(expired) > 0:
		return metav1.Condition{
			Type:    km.ClusterCertificatesExpiringCondition,
			Status:  metav1.ConditionTrue,
			Reason:  km.ClusterCertificatesExpiredReason,
			Message: fmt.Sprintf("Expired certificates: %s", strings.Join(expired, ", ")),
		}
	case len(due) > 0:
		return metav1.Condition{
			Type:    km.ClusterCertificatesExpiringCondition,
			Status:  metav1.ConditionTrue,
			Reason:  km.ClusterCertificatesRenewalDueReason,
			Message: fmt.Sprintf("Certificates due for renewal: %s", strings.Join(due, ", ")),
		}
	case len(unparseable) > 0:
		// Copy before sorting: the slice belongs to the caller.
		sorted := append([]string(nil), unparseable...)
		sort.Strings(sorted)
		return metav1.Condition{
			Type:    km.ClusterCertificatesExpiringCondition,
			Status:  metav1.ConditionUnknown,
			Reason:  km.InternalErrorReason,
			Message: fmt.Sprintf("Unable to read certificates: %s", strings.Join(sorted, ", ")),
		}
	default:
		return metav1.Condition{
			Type:   km.ClusterCertificatesExpiringCondition,
			Status: metav1.ConditionFalse,
			Reason: km.ClusterCertificatesValidReason,
		}
	}
}

// Emit publishes the expiry of every inspected certificate as a metric.
func Emit(namespace, cluster, kind string, is []Info) {
	for _, i := range is {
		metrics.RecordExpiry(namespace, cluster, kind, i)
	}
}
