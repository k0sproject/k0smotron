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

package k0smotronio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/util/conditions"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/certs"
)

func TestSetCertificatesConditions(t *testing.T) {
	tests := []struct {
		name          string
		certificates  []certs.Info
		refs          []km.CertificateRef
		wantAvailable metav1.ConditionStatus
		wantExpiring  metav1.ConditionStatus
	}{
		{
			// Nothing is known about any certificate, so "not expiring" is a
			// claim with no data behind it: Expiring must be Unknown.
			name:          "no certificate refs at all",
			refs:          nil,
			wantAvailable: metav1.ConditionFalse,
			wantExpiring:  metav1.ConditionUnknown,
		},
		{
			// storage.type kine with no ingress: the CA refs exist but
			// k0smotron signs no leaves, so an empty set is not a failure.
			name:          "refs present but k0smotron signs no leaves",
			refs:          []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}},
			certificates:  nil,
			wantAvailable: metav1.ConditionTrue,
			wantExpiring:  metav1.ConditionFalse,
		},
		{
			name:          "healthy certificates",
			refs:          []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}},
			certificates:  []certs.Info{{Purpose: "etcd-server", NotAfter: time.Now().Add(365 * 24 * time.Hour)}},
			wantAvailable: metav1.ConditionTrue,
			wantExpiring:  metav1.ConditionFalse,
		},
		{
			name:          "expired certificate flips Available to False",
			refs:          []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}},
			certificates:  []certs.Info{{Purpose: "etcd-server", NotAfter: time.Now().Add(-time.Hour)}},
			wantAvailable: metav1.ConditionFalse,
			wantExpiring:  metav1.ConditionTrue,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kmc := &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
				Spec:       km.ClusterSpec{CertificateRefs: tc.refs},
			}

			setCertificatesConditions(kmc, currentReconcileState{certificatesReconciled: true, certificates: tc.certificates})

			available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
			assert.NotNil(t, available)
			assert.Equal(t, tc.wantAvailable, available.Status)

			expiring := conditions.Get(kmc, km.ClusterCertificatesExpiringCondition)
			assert.NotNil(t, expiring)
			assert.Equal(t, tc.wantExpiring, expiring.Status)
		})
	}
}

func TestSetCertificatesConditions_doesNotAffectAvailableSummary(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
	}

	// A healthy control plane with an expired certificate must still summarise
	// as Available: certificate conditions are informational, not gating.
	conditions.Set(kmc, metav1.Condition{
		Type: km.ClusterControlPlaneFunctionalCondition, Status: metav1.ConditionTrue, Reason: "Functional"})
	conditions.Set(kmc, metav1.Condition{
		Type: km.ClusterKubeconfigSecretAvailableCondition, Status: metav1.ConditionTrue, Reason: km.CreatedReason})
	conditions.Set(kmc, metav1.Condition{
		Type: km.ClusterControlPlaneExposedCondition, Status: metav1.ConditionTrue, Reason: "Exposed"})
	conditions.Set(kmc, metav1.Condition{
		Type: km.ClusterDeletingCondition, Status: metav1.ConditionFalse, Reason: km.ClusterNotDeletingReason})

	setCertificatesConditions(kmc, currentReconcileState{
		certificatesReconciled: true,
		certificates:           []certs.Info{{Purpose: "etcd-server", NotAfter: time.Now().Add(-time.Hour)}},
	})
	setAvailableCondition(kmc)

	available := conditions.Get(kmc, km.ClusterAvailableCondition)
	assert.NotNil(t, available)
	assert.Equal(t, metav1.ConditionTrue, available.Status)
}

// TestSetCertificatesConditions_notReconciledLeavesConditionsAlone guards
// against a regression where an unrelated transient error earlier in
// Reconcile (before certificates get inspected this pass) would let
// setCertificatesConditions overwrite real, previously-established
// certificate status with a false "none found" / "not expiring". When
// certificatesReconciled is false, the function must not touch either
// condition at all — not even to set them for the first time.
func TestSetCertificatesConditions_notReconciledLeavesConditionsAlone(t *testing.T) {
	t.Run("fresh object: no conditions are set", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
			Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
		}

		setCertificatesConditions(kmc, currentReconcileState{
			certificatesReconciled: false,
			certificates:           []certs.Info{{Purpose: "etcd-server", NotAfter: time.Now().Add(365 * 24 * time.Hour)}},
		})

		assert.Nil(t, conditions.Get(kmc, km.ClusterCertificatesAvailableCondition))
		assert.Nil(t, conditions.Get(kmc, km.ClusterCertificatesExpiringCondition))
	})

	t.Run("a pre-set condition survives unchanged", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
			Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
		}
		// Simulate a previous, successful reconcile that found a certificate
		// genuinely expiring soon.
		conditions.Set(kmc, metav1.Condition{
			Type:    km.ClusterCertificatesAvailableCondition,
			Status:  metav1.ConditionTrue,
			Reason:  km.CreatedReason,
			Message: "previous pass",
		})
		conditions.Set(kmc, metav1.Condition{
			Type:    km.ClusterCertificatesExpiringCondition,
			Status:  metav1.ConditionTrue,
			Reason:  km.ClusterCertificatesRenewalDueReason,
			Message: "etcd-server (in 47h)",
		})

		// This pass hit an unrelated transient error before certificates were
		// inspected, so certificatesReconciled is false even though the state
		// carries a stale/irrelevant certificates slice.
		setCertificatesConditions(kmc, currentReconcileState{
			certificatesReconciled: false,
			certificates:           nil,
		})

		available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
		assert.NotNil(t, available)
		assert.Equal(t, metav1.ConditionTrue, available.Status)
		assert.Equal(t, "previous pass", available.Message)

		expiring := conditions.Get(kmc, km.ClusterCertificatesExpiringCondition)
		assert.NotNil(t, expiring)
		assert.Equal(t, metav1.ConditionTrue, expiring.Status)
		assert.Equal(t, "etcd-server (in 47h)", expiring.Message)
	})

	// An empty leaf set with certificate refs present is NOT a failure: with
	// storage.type kine and no ingress k0smotron signs no leaf certificates at
	// all, so reporting False/NotFound ("No certificates found for this
	// cluster") would be a permanent false alarm on a supported configuration.
	// The referenced certificates exist; there is simply nothing to track here.
	t.Run("reconciled with empty leaf set and refs present reports Available", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
			Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
		}

		setCertificatesConditions(kmc, currentReconcileState{
			certificatesReconciled: true,
			certificates:           nil,
		})

		available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
		assert.NotNil(t, available)
		assert.Equal(t, metav1.ConditionTrue, available.Status)
		assert.Equal(t, km.CreatedReason, available.Reason)

		expiring := conditions.Get(kmc, km.ClusterCertificatesExpiringCondition)
		assert.NotNil(t, expiring)
		assert.Equal(t, metav1.ConditionFalse, expiring.Status)
		assert.Equal(t, km.ClusterCertificatesValidReason, expiring.Reason)
	})

	// An unreadable certificate secret must still reach report.Build so it is
	// surfaced as Unknown, rather than being swallowed by the empty-leaf-set
	// guard above.
	t.Run("reconciled with empty leaf set but unparseable secrets reports Unknown", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
			Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
		}

		setCertificatesConditions(kmc, currentReconcileState{
			certificatesReconciled:  true,
			certificates:            nil,
			certificatesUnparseable: []string{"etcd-server"},
		})

		available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
		assert.NotNil(t, available)
		assert.Equal(t, metav1.ConditionUnknown, available.Status)
		assert.Equal(t, km.InternalErrorReason, available.Reason)

		expiring := conditions.Get(kmc, km.ClusterCertificatesExpiringCondition)
		assert.NotNil(t, expiring)
		assert.Equal(t, metav1.ConditionUnknown, expiring.Status)
	})

	// No refs at all means nothing is known: "not expiring" would be a
	// reassuring claim backed by zero data.
	t.Run("no refs reports Unknown for Expiring", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		}

		setCertificatesConditions(kmc, currentReconcileState{certificatesReconciled: true})

		available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
		assert.NotNil(t, available)
		assert.Equal(t, metav1.ConditionFalse, available.Status)
		assert.Equal(t, km.NotFoundReason, available.Reason)

		expiring := conditions.Get(kmc, km.ClusterCertificatesExpiringCondition)
		assert.NotNil(t, expiring)
		assert.Equal(t, metav1.ConditionUnknown, expiring.Status)
		assert.Equal(t, km.NotFoundReason, expiring.Reason)
	})

	t.Run("reconciled with a healthy certificate reports Available", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
			Spec:       km.ClusterSpec{CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "kmc-ca"}}},
		}

		setCertificatesConditions(kmc, currentReconcileState{
			certificatesReconciled: true,
			certificates:           []certs.Info{{Purpose: "etcd-server", NotAfter: time.Now().Add(365 * 24 * time.Hour)}},
		})

		available := conditions.Get(kmc, km.ClusterCertificatesAvailableCondition)
		assert.NotNil(t, available)
		assert.Equal(t, metav1.ConditionTrue, available.Status)
	})
}
