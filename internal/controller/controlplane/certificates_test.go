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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func caSecret(t *testing.T, name string, notAfter time.Time) *corev1.Secret {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kubernetes"},
		NotBefore:             notAfter.Add(-24 * time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		},
	}
}

func TestInspectClusterCertificates(t *testing.T) {
	notAfter := time.Now().Add(10 * 365 * 24 * time.Hour).Truncate(time.Second)
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		caSecret(t, "kcp-ca", notAfter),
		caSecret(t, "kcp-etcd", notAfter),
	).Build()

	infos, unreadable := inspectClusterCertificates(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "kcp"})

	assert.Empty(t, unreadable)
	// front-proxy and sa secrets are absent; missing secrets are not errors,
	// they simply have nothing to report yet.
	require.Len(t, infos, 2)

	purposes := []string{infos[0].Purpose, infos[1].Purpose}
	assert.Contains(t, purposes, "ca")
	assert.Contains(t, purposes, "etcd")
}

func TestInspectClusterCertificates_unparseableIsReported(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "kcp-ca", Namespace: "default"},
			Data:       map[string][]byte{"tls.crt": []byte("garbage")},
		},
	).Build()

	infos, unreadable := inspectClusterCertificates(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "kcp"})

	assert.Empty(t, infos)
	assert.Equal(t, []string{"ca"}, unreadable)
}
