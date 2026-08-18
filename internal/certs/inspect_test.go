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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testCertPEM returns a self-signed certificate PEM valid until notAfter.
func testCertPEM(t *testing.T, cn string, serial int64, notAfter time.Time) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	// Self-signed: x509.CreateCertificate takes the issuer from the parent's
	// Subject, so setting an Issuer field here would be ignored. The issuer of
	// this certificate is its own CN.
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

func testSecret(name string, crt []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data:       map[string][]byte{"tls.crt": crt},
	}
}

func TestInspect(t *testing.T) {
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s := testSecret("kmc-etcd-server", testCertPEM(t, "etcd-server", 42, notAfter))

	got, err := Inspect(s)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, notAfter.Unix(), got[0].NotAfter.Unix())
	assert.Equal(t, "2a", got[0].Serial)
	assert.Equal(t, "etcd-server", got[0].Issuer, "a self-signed certificate is its own issuer")
}

func TestInspect_certificateChainUsesLeaf(t *testing.T) {
	leafNotAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	leaf := testCertPEM(t, "etcd-server", 42, leafNotAfter)

	otherNotAfter := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	other := testCertPEM(t, "some-ca", 99, otherNotAfter)

	chain := append(append([]byte{}, leaf...), other...)
	s := testSecret("kmc-etcd-server", chain)

	got, err := Inspect(s)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, leafNotAfter.Unix(), got[0].NotAfter.Unix())
	assert.Equal(t, "2a", got[0].Serial)
}

func TestInspect_nilSecret(t *testing.T) {
	_, err := Inspect(nil)
	assert.Error(t, err)
}

func TestInspect_missingTLSCrt(t *testing.T) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
		Data:       map[string][]byte{"tls.key": []byte("nope")},
	}
	_, err := Inspect(s)
	assert.Error(t, err)
}

func TestInspect_unparseable(t *testing.T) {
	_, err := Inspect(testSecret("x", []byte("not a pem")))
	assert.Error(t, err)
}

func TestNeedsRenewal(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		notAfter    time.Time
		renewBefore time.Duration
		want        bool
	}{
		{"far from expiry", now.Add(100 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"exactly at threshold", now.Add(30 * 24 * time.Hour), 30 * 24 * time.Hour, true},
		{"inside threshold", now.Add(10 * 24 * time.Hour), 30 * 24 * time.Hour, true},
		{"already expired", now.Add(-time.Hour), 30 * 24 * time.Hour, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NeedsRenewal(Info{NotAfter: tc.notAfter}, tc.renewBefore, now))
		})
	}
}

func TestEarliestRenewal(t *testing.T) {
	base := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	is := []Info{
		{Purpose: "a", NotAfter: base.Add(100 * 24 * time.Hour)},
		{Purpose: "b", NotAfter: base.Add(40 * 24 * time.Hour)},
	}
	got := EarliestRenewal(is, 30*24*time.Hour)
	assert.Equal(t, base.Add(10*24*time.Hour).Unix(), got.Unix())
}

func TestEarliestRenewal_empty(t *testing.T) {
	assert.True(t, EarliestRenewal(nil, time.Hour).IsZero())
}

func TestFingerprint(t *testing.T) {
	a := []Info{{Purpose: "etcd-server", Serial: "2a"}, {Purpose: "etcd-peer", Serial: "2b"}}
	// Order must not matter.
	b := []Info{{Purpose: "etcd-peer", Serial: "2b"}, {Purpose: "etcd-server", Serial: "2a"}}
	assert.Equal(t, Fingerprint(a), Fingerprint(b))

	c := []Info{{Purpose: "etcd-server", Serial: "99"}, {Purpose: "etcd-peer", Serial: "2b"}}
	assert.NotEqual(t, Fingerprint(a), Fingerprint(c))

	assert.NotEmpty(t, Fingerprint(a))
	assert.Equal(t, "", Fingerprint(nil))
}
