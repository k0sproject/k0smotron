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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/certs"
)

func TestCertificateSettings(t *testing.T) {
	dur := func(d time.Duration) *metav1.Duration { return &metav1.Duration{Duration: d} }

	tests := []struct {
		name            string
		spec            *km.CertificatesSpec
		wantDuration    time.Duration
		wantRenewBefore time.Duration
	}{
		{"nil spec uses defaults", nil, certs.DefaultDuration, certs.DefaultRenewBefore},
		{"empty spec uses defaults", &km.CertificatesSpec{}, certs.DefaultDuration, certs.DefaultRenewBefore},
		{
			"duration only",
			&km.CertificatesSpec{Duration: dur(48 * time.Hour)},
			48 * time.Hour, certs.DefaultRenewBefore,
		},
		{
			"both set",
			&km.CertificatesSpec{Duration: dur(48 * time.Hour), RenewBefore: dur(12 * time.Hour)},
			48 * time.Hour, 12 * time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kmc := &km.Cluster{Spec: km.ClusterSpec{Certificates: tc.spec}}
			gotDuration, gotRenewBefore := certificateSettings(kmc)
			assert.Equal(t, tc.wantDuration, gotDuration)
			assert.Equal(t, tc.wantRenewBefore, gotRenewBefore)
		})
	}
}

func TestShouldRenew(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	renewBefore := 30 * 24 * time.Hour

	t.Run("forced by annotation regardless of threshold", func(t *testing.T) {
		kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{km.RenewCertificatesAnnotation: ""},
		}}
		info := certs.Info{NotAfter: now.Add(365 * 24 * time.Hour)}
		assert.True(t, shouldRenew(kmc, info, renewBefore, now))
	})

	t.Run("not forced and far from expiry", func(t *testing.T) {
		kmc := &km.Cluster{}
		info := certs.Info{NotAfter: now.Add(365 * 24 * time.Hour)}
		assert.False(t, shouldRenew(kmc, info, renewBefore, now))
	})

	t.Run("inside threshold", func(t *testing.T) {
		kmc := &km.Cluster{}
		info := certs.Info{NotAfter: now.Add(10 * 24 * time.Hour)}
		assert.True(t, shouldRenew(kmc, info, renewBefore, now))
	})
}

func TestIsManagedSecret(t *testing.T) {
	kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default", UID: "cluster-uid"}}
	externalOwner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "kmc-root-owner", Namespace: "default", UID: "external-owner-uid",
	}}

	t.Run("nil secret is a first issuance", func(t *testing.T) {
		assert.True(t, isManagedSecret(nil, kmc, nil))
	})

	t.Run("owned by this cluster, no external owner in play", func(t *testing.T) {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{UID: "cluster-uid"}},
		}}
		assert.True(t, isManagedSecret(s, kmc, nil))
	})

	t.Run("owned by the external owner", func(t *testing.T) {
		// This is the case a hosted control plane on a remote host cluster hits:
		// ensureEtcdCertificates stamps the external owner, not the Cluster, so
		// this must count as managed or renewal never fires.
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{UID: "external-owner-uid"}},
		}}
		assert.True(t, isManagedSecret(s, kmc, externalOwner))
	})

	t.Run("owned by neither the cluster nor the external owner", func(t *testing.T) {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{UID: "someone-elses-uid"}},
		}}
		assert.False(t, isManagedSecret(s, kmc, externalOwner))
	})

	t.Run("user supplied, not owned", func(t *testing.T) {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "my-own-cert",
		}}
		assert.False(t, isManagedSecret(s, kmc, nil),
			"a certificate the user brought must never be overwritten")
	})
}

// --- signLeaves ---

// testCA returns a self-signed CA certificate and key, PEM encoded, valid
// until notAfter.
func testCA(t *testing.T, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

// testLeafCertPEM returns a self-signed leaf certificate PEM valid until
// notAfter. It is never actually chained to a CA: signLeaves' renewal path
// only reads expiry/serial metadata off of it via kcerts.Inspect, so a
// self-signed placeholder is sufficient to stand in for "the certificate
// already on file".
func testLeafCertPEM(t *testing.T, cn string, serial int64, notAfter time.Time) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newCertTestScope(objs ...client.Object) *kmcScope {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &kmcScope{client: c, secretCachingClient: c}
}

func newCertTestScopeWithInterceptor(funcs interceptor.Funcs, objs ...client.Object) *kmcScope {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	c := interceptor.NewClient(base, funcs)
	return &kmcScope{client: c, secretCachingClient: c}
}

// TestSignLeaves_renewsSecretOwnedByExternalOwner is the regression guard for
// the Critical finding: a hosted control plane on a remote host cluster
// stamps the external owner (a ConfigMap), not the Cluster, on its
// certificate secrets. Against the pre-fix isManagedSecret (which checked
// only kmc.UID), this secret would be classified as user-supplied and never
// renewed — reproducing the etcd expiry bug with no error and no metric. This
// test fails against that pre-fix behavior.
func TestSignLeaves_renewsSecretOwnedByExternalOwner(t *testing.T) {
	now := time.Now()
	caCertPEM, caKeyPEM := testCA(t, now.Add(10*365*24*time.Hour))

	externalOwner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "kmc-root-owner", Namespace: "default", UID: "external-owner-uid",
	}}

	oldNotAfter := now.Add(5 * 24 * time.Hour) // inside the default 720h renewal window
	oldCert := testLeafCertPEM(t, "etcd-server", 1, oldNotAfter)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kmc-etcd-server",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "kmc-root-owner", UID: "external-owner-uid"},
			},
		},
		Data: map[string][]byte{"tls.crt": oldCert, "tls.key": []byte("old-key")},
	}

	scope := newCertTestScope(existing)
	scope.externalOwner = externalOwner

	kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default", UID: "cluster-uid"}}

	infos, err := scope.signLeaves(context.Background(), kmc, caCertPEM, caKeyPEM,
		secret.Certificates{&secret.Certificate{Purpose: "etcd-server"}},
		func(string) []string { return []string{"localhost"} },
		func(purpose string) string { return purpose },
		metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "kmc-root-owner", UID: "external-owner-uid"},
	)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Empty(t, scope.currentReconcileState.certificateRenewalErrors)

	// The certificate must have moved forward in time relative to the old one.
	assert.True(t, infos[0].NotAfter.After(oldNotAfter),
		"a renewed certificate must report a later expiry than the one it replaced")

	got := &corev1.Secret{}
	require.NoError(t, scope.client.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"}, got))
	assert.NotEqual(t, oldCert, got.Data["tls.crt"],
		"the secret must be rewritten with the renewed certificate")
}

// TestSignLeaves_renewalFailureDoesNotAbort verifies that a failure to persist
// a renewal is reported through certificateRenewalErrors rather than aborting
// reconciliation: the existing, still-valid certificate must be left in place.
func TestSignLeaves_renewalFailureDoesNotAbort(t *testing.T) {
	now := time.Now()
	caCertPEM, caKeyPEM := testCA(t, now.Add(10*365*24*time.Hour))

	oldNotAfter := now.Add(5 * 24 * time.Hour)
	oldCert := testLeafCertPEM(t, "etcd-server", 2, oldNotAfter)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kmc-etcd-server",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "cluster-uid"},
			},
		},
		Data: map[string][]byte{"tls.crt": oldCert, "tls.key": []byte("old-key")},
	}

	scope := newCertTestScopeWithInterceptor(interceptor.Funcs{
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
			return errors.New("simulated update failure")
		},
	}, existing)

	kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default", UID: "cluster-uid"}}

	infos, err := scope.signLeaves(context.Background(), kmc, caCertPEM, caKeyPEM,
		secret.Certificates{&secret.Certificate{Purpose: "etcd-server"}},
		func(string) []string { return []string{"localhost"} },
		func(purpose string) string { return purpose },
		metav1.OwnerReference{Name: "kmc", UID: "cluster-uid"},
	)

	require.NoError(t, err, "a renewal failure must not abort reconciliation")
	require.Len(t, infos, 1)
	assert.Equal(t, oldNotAfter.Unix(), infos[0].NotAfter.Unix(),
		"the old certificate must still be reported when the renewal write fails")
	require.Len(t, scope.currentReconcileState.certificateRenewalErrors, 1)

	got := &corev1.Secret{}
	require.NoError(t, scope.client.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"}, got))
	assert.Equal(t, oldCert, got.Data["tls.crt"], "the old certificate must be left in place")
}

// TestSignLeaves_firstIssuanceFailureIsFatal verifies that a failure to
// persist a first-issued certificate (which has no prior valid certificate to
// fall back to) is returned as an error, unlike a renewal failure.
func TestSignLeaves_firstIssuanceFailureIsFatal(t *testing.T) {
	now := time.Now()
	caCertPEM, caKeyPEM := testCA(t, now.Add(10*365*24*time.Hour))

	scope := newCertTestScopeWithInterceptor(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("simulated create failure")
		},
	})

	kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default", UID: "cluster-uid"}}

	_, err := scope.signLeaves(context.Background(), kmc, caCertPEM, caKeyPEM,
		secret.Certificates{&secret.Certificate{Purpose: "etcd-server"}},
		func(string) []string { return []string{"localhost"} },
		func(purpose string) string { return purpose },
		metav1.OwnerReference{Name: "kmc", UID: "cluster-uid"},
	)

	require.Error(t, err, "a first-issuance failure must abort reconciliation: nothing can start without it")
	assert.Empty(t, scope.currentReconcileState.certificateRenewalErrors,
		"a first-issuance failure is not a renewal failure and must not be reported as one")
}

// TestSignLeaves_userSuppliedSecretIsNeverRewritten verifies that a
// certificate secret owned by neither the Cluster nor the external owner
// (i.e. supplied by the user through spec.certificateRefs) is reported as-is
// and never re-signed, even when it is past its renewal threshold.
func TestSignLeaves_userSuppliedSecretIsNeverRewritten(t *testing.T) {
	now := time.Now()
	caCertPEM, caKeyPEM := testCA(t, now.Add(10*365*24*time.Hour))

	oldNotAfter := now.Add(5 * 24 * time.Hour) // inside the renewal window, if it were allowed
	oldCert := testLeafCertPEM(t, "ingress-haproxy", 7, oldNotAfter)
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kmc-ingress-haproxy",
			Namespace: "default",
			// No owner references: this secret was brought by the user.
		},
		Data: map[string][]byte{"tls.crt": oldCert, "tls.key": []byte("user-key")},
	}

	scope := newCertTestScope(userSecret)
	kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default", UID: "cluster-uid"}}

	infos, err := scope.signLeaves(context.Background(), kmc, caCertPEM, caKeyPEM,
		secret.Certificates{&secret.Certificate{Purpose: "ingress-haproxy"}},
		func(string) []string { return []string{"localhost"} },
		func(string) string { return "kubernetes" },
		metav1.OwnerReference{Name: "kmc", UID: "cluster-uid"},
	)

	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, oldNotAfter.Unix(), infos[0].NotAfter.Unix())
	assert.Equal(t, "7", infos[0].Serial, "the user-supplied certificate's serial must be unchanged")

	got := &corev1.Secret{}
	require.NoError(t, scope.client.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "kmc-ingress-haproxy"}, got))
	assert.Equal(t, oldCert, got.Data["tls.crt"], "a user-supplied certificate must never be overwritten")
}
