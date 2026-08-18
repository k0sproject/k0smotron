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

package report

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/certs"
)

func TestBuild(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	renewBefore := 30 * 24 * time.Hour

	tests := []struct {
		name            string
		infos           []certs.Info
		unparseable     []string
		wantAvailable   metav1.ConditionStatus
		wantExpiring    metav1.ConditionStatus
		wantExpReason   string
		wantAvailReason string
	}{
		{
			name:            "all healthy",
			infos:           []certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(200 * 24 * time.Hour)}},
			wantAvailable:   metav1.ConditionTrue,
			wantExpiring:    metav1.ConditionFalse,
			wantExpReason:   km.ClusterCertificatesValidReason,
			wantAvailReason: km.CreatedReason,
		},
		{
			name:            "inside renewal window",
			infos:           []certs.Info{{Purpose: "etcd-peer", NotAfter: now.Add(10 * 24 * time.Hour)}},
			wantAvailable:   metav1.ConditionTrue,
			wantExpiring:    metav1.ConditionTrue,
			wantExpReason:   km.ClusterCertificatesRenewalDueReason,
			wantAvailReason: km.CreatedReason,
		},
		{
			name:            "expired",
			infos:           []certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(-time.Hour)}},
			wantAvailable:   metav1.ConditionFalse,
			wantExpiring:    metav1.ConditionTrue,
			wantExpReason:   km.ClusterCertificatesExpiredReason,
			wantAvailReason: km.ClusterCertificatesExpiredReason,
		},
		{
			name:            "no certificates at all",
			infos:           nil,
			wantAvailable:   metav1.ConditionFalse,
			wantExpiring:    metav1.ConditionFalse,
			wantExpReason:   km.ClusterCertificatesValidReason,
			wantAvailReason: km.NotFoundReason,
		},
		{
			// A certificate we cannot read must not be reported as "Valid":
			// both conditions go Unknown together.
			name:            "unparseable secret",
			infos:           []certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(200 * 24 * time.Hour)}},
			unparseable:     []string{"etcd-peer"},
			wantAvailable:   metav1.ConditionUnknown,
			wantExpiring:    metav1.ConditionUnknown,
			wantExpReason:   km.InternalErrorReason,
			wantAvailReason: km.InternalErrorReason,
		},
		{
			name: "expired and due and unparseable together",
			infos: []certs.Info{
				{Purpose: "etcd-server", NotAfter: now.Add(-time.Hour)},
				{Purpose: "etcd-peer", NotAfter: now.Add(10 * 24 * time.Hour)},
			},
			unparseable:     []string{"admin-cert"},
			wantAvailable:   metav1.ConditionFalse,
			wantExpiring:    metav1.ConditionTrue,
			wantExpReason:   km.ClusterCertificatesExpiredReason,
			wantAvailReason: km.ClusterCertificatesExpiredReason,
		},
		{
			name:            "unparseable secret with no expired certificate",
			infos:           []certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(200 * 24 * time.Hour)}},
			unparseable:     []string{"admin-cert"},
			wantAvailable:   metav1.ConditionUnknown,
			wantExpiring:    metav1.ConditionUnknown,
			wantExpReason:   km.InternalErrorReason,
			wantAvailReason: km.InternalErrorReason,
		},
		{
			// Due outranks unparseable: a certificate we can prove is in its
			// renewal window still reports Expiring=True.
			name:            "due and unparseable together",
			infos:           []certs.Info{{Purpose: "etcd-peer", NotAfter: now.Add(10 * 24 * time.Hour)}},
			unparseable:     []string{"admin-cert"},
			wantAvailable:   metav1.ConditionUnknown,
			wantExpiring:    metav1.ConditionTrue,
			wantExpReason:   km.ClusterCertificatesRenewalDueReason,
			wantAvailReason: km.InternalErrorReason,
		},
		{
			// Nothing readable at all: Unknown on both, never "Valid".
			name:            "only unparseable certificates",
			infos:           nil,
			unparseable:     []string{"admin-cert", "etcd-peer"},
			wantAvailable:   metav1.ConditionUnknown,
			wantExpiring:    metav1.ConditionUnknown,
			wantExpReason:   km.InternalErrorReason,
			wantAvailReason: km.InternalErrorReason,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(tc.infos, tc.unparseable, renewBefore, now)
			assert.Equal(t, tc.wantAvailable, got.Available.Status)
			assert.Equal(t, tc.wantAvailReason, got.Available.Reason)
			assert.Equal(t, tc.wantExpiring, got.Expiring.Status)
			assert.Equal(t, tc.wantExpReason, got.Expiring.Reason)
			assert.Equal(t, km.ClusterCertificatesAvailableCondition, got.Available.Type)
			assert.Equal(t, km.ClusterCertificatesExpiringCondition, got.Expiring.Type)
		})
	}
}

func TestBuild_expiringMessageNamesThePurpose(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	got := Build([]certs.Info{{Purpose: "etcd-peer", NotAfter: now.Add(10 * 24 * time.Hour)}},
		nil, 30*24*time.Hour, now)
	assert.Contains(t, got.Expiring.Message, "etcd-peer")
}

func TestBuild_availableMessageNamesExpiredPurposeEvenWithUnparseable(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	got := Build(
		[]certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(-time.Hour)}},
		[]string{"admin-cert"},
		30*24*time.Hour, now,
	)
	assert.Equal(t, metav1.ConditionFalse, got.Available.Status)
	assert.Equal(t, km.ClusterCertificatesExpiredReason, got.Available.Reason)
	assert.Contains(t, got.Available.Message, "etcd-server")
}

func TestBuild_expiringIsUnknownWhenOnlyUnparseable(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	got := Build(
		[]certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(200 * 24 * time.Hour)}},
		[]string{"admin-cert"},
		30*24*time.Hour, now,
	)

	assert.Equal(t, metav1.ConditionUnknown, got.Expiring.Status,
		"a certificate we cannot read must not be reported as not expiring")
	assert.Equal(t, km.InternalErrorReason, got.Expiring.Reason)
	assert.Contains(t, got.Expiring.Message, "admin-cert")
}

func TestBuild_doesNotMutateCallerUnparseableSlice(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	unparseable := []string{"zzz", "aaa"}
	original := append([]string(nil), unparseable...)

	Build([]certs.Info{{Purpose: "etcd-server", NotAfter: now.Add(200 * 24 * time.Hour)}},
		unparseable, 30*24*time.Hour, now)

	assert.Equal(t, original, unparseable, "Build must not reorder the caller's unparseable slice in place")
}
