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

package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	kapi "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

func newCertConditionsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kapi.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))
	return scheme
}

// TestReconcileCertificateConditions_MirrorsChildConditions verifies both the
// mirroring behaviour itself, and the client asymmetry the task deliberately
// introduced: the child k0smotron Cluster always lives in the management
// cluster and must be read with c.Client, never with scope.client (which is
// swapped to the remote host cluster's client when replicas run there). The
// scope.client used here is a distinct, empty fake client - if the code under
// test ever read the child through scope.client instead, the Get would 404
// and the function would fall through to the CA-only fallback, producing
// completely different (and in this test, empty) conditions.
func TestReconcileCertificateConditions_MirrorsChildConditions(t *testing.T) {
	scheme := newCertConditionsScheme(t)

	child := &kapi.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"},
	}
	child.SetConditions([]metav1.Condition{
		{
			Type:   kapi.ClusterCertificatesAvailableCondition,
			Status: metav1.ConditionTrue,
			Reason: kapi.CreatedReason,
		},
		{
			Type:    kapi.ClusterCertificatesExpiringCondition,
			Status:  metav1.ConditionTrue,
			Reason:  kapi.ClusterCertificatesRenewalDueReason,
			Message: "certificates due for renewal",
		},
	})

	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child).Build()
	// Deliberately distinct from mgmtClient and empty: proves the child is
	// read via c.Client, not scope.client.
	remoteClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	c := &K0smotronController{Client: mgmtClient}
	scope := &kmcScope{client: remoteClient}
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}

	c.reconcileCertificateConditions(context.Background(), cluster, kcp, scope)

	available := conditions.Get(kcp, kapi.ClusterCertificatesAvailableCondition)
	require.NotNil(t, available)
	assert.Equal(t, metav1.ConditionTrue, available.Status)
	assert.Equal(t, kapi.CreatedReason, available.Reason)

	expiring := conditions.Get(kcp, kapi.ClusterCertificatesExpiringCondition)
	require.NotNil(t, expiring)
	assert.Equal(t, metav1.ConditionTrue, expiring.Status)
	assert.Equal(t, kapi.ClusterCertificatesRenewalDueReason, expiring.Reason)
	assert.Equal(t, "certificates due for renewal", expiring.Message)
}

// TestReconcileCertificateConditions_ReadErrorSetsBothUnknown is the
// regression guard for the fix where a non-NotFound error reading the child
// Cluster left ClusterCertificatesExpiringCondition untouched, keeping
// whatever stale value (typically False/not-expiring) it held from the last
// successful reconcile. Against the pre-fix code, Expiring is never set here,
// so this test fails with a nil condition.
func TestReconcileCertificateConditions_ReadErrorSetsBothUnknown(t *testing.T) {
	scheme := newCertConditionsScheme(t)
	base := fake.NewClientBuilder().WithScheme(scheme).Build()

	forbidden := errors.New("boom")
	mgmtClient := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*kapi.Cluster); ok {
				return apierrors.NewForbidden(schema.GroupResource{Group: "k0smotron.io", Resource: "clusters"}, key.Name, forbidden)
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	})

	c := &K0smotronController{Client: mgmtClient}
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}

	c.reconcileCertificateConditions(context.Background(), cluster, kcp, nil)

	available := conditions.Get(kcp, kapi.ClusterCertificatesAvailableCondition)
	require.NotNil(t, available, "Available condition must be set on a read error")
	assert.Equal(t, metav1.ConditionUnknown, available.Status)
	assert.Equal(t, kapi.InternalErrorReason, available.Reason)

	expiring := conditions.Get(kcp, kapi.ClusterCertificatesExpiringCondition)
	require.NotNil(t, expiring, "Expiring condition must ALSO be set on a read error, not left stale")
	assert.Equal(t, metav1.ConditionUnknown, expiring.Status)
	assert.Equal(t, kapi.InternalErrorReason, expiring.Reason)

	assert.Equal(t, available.Message, expiring.Message, "both conditions should share the same explanatory message")
}

// TestReconcileCertificateConditions_ChildNotYetReported covers the race just
// after the child Cluster is created but before its own controller's first
// reconcile has set any certificate conditions. Against the pre-fix code the
// function returns having set nothing, so this test fails with nil
// conditions.
func TestReconcileCertificateConditions_ChildNotYetReported(t *testing.T) {
	scheme := newCertConditionsScheme(t)
	child := &kapi.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"},
	}
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child).Build()

	c := &K0smotronController{Client: mgmtClient}
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}

	c.reconcileCertificateConditions(context.Background(), cluster, kcp, nil)

	available := conditions.Get(kcp, kapi.ClusterCertificatesAvailableCondition)
	require.NotNil(t, available)
	assert.Equal(t, metav1.ConditionUnknown, available.Status)

	expiring := conditions.Get(kcp, kapi.ClusterCertificatesExpiringCondition)
	require.NotNil(t, expiring)
	assert.Equal(t, metav1.ConditionUnknown, expiring.Status)
}

// TestReconcileCertificateConditions_CAOnlyFallback covers the case where the
// child Cluster does not exist yet (NotFound): conditions must come from
// inspecting the CA secrets directly via scope.client, which is where
// ensureCertificates writes them.
func TestReconcileCertificateConditions_CAOnlyFallback(t *testing.T) {
	scheme := newCertConditionsScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	notAfter := time.Now().Add(10 * 365 * 24 * time.Hour).Truncate(time.Second)
	scopeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		caSecret(t, "kcp-ca", notAfter),
		caSecret(t, "kcp-etcd", notAfter),
		caSecret(t, "kcp-proxy", notAfter),
	).Build()

	c := &K0smotronController{Client: mgmtClient}
	scope := &kmcScope{client: scopeClient}
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}

	c.reconcileCertificateConditions(context.Background(), cluster, kcp, scope)

	available := conditions.Get(kcp, kapi.ClusterCertificatesAvailableCondition)
	require.NotNil(t, available)
	assert.Equal(t, metav1.ConditionTrue, available.Status)
	assert.Equal(t, kapi.CreatedReason, available.Reason)

	expiring := conditions.Get(kcp, kapi.ClusterCertificatesExpiringCondition)
	require.NotNil(t, expiring)
	assert.Equal(t, metav1.ConditionFalse, expiring.Status)
	assert.Equal(t, kapi.ClusterCertificatesValidReason, expiring.Reason)
}
