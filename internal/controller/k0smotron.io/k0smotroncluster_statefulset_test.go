//go:build !envtest

/*
Copyright 2023.

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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

func Test_pathToVolumeName(t *testing.T) {
	tests := []struct {
		path       string
		want       string // exact expected name for simple/clean paths (no hash)
		wantPrefix string // expected prefix for sanitized paths (ends with "-")
	}{
		{
			// Simple path: already valid, no hash added (backward-compatible)
			path: "/etc/kubernetes/pki",
			want: "etc-kubernetes-pki",
		},
		{
			// Simple path used in e2e upgrade test - must stay stable across upgrades
			path: "/tmp/test",
			want: "tmp-test",
		},
		{
			// Underscore is invalid -> sanitized with hash
			path:       "/my_config/file.conf",
			wantPrefix: "my-config-file-conf-",
		},
		{
			// Dot is invalid -> sanitized with hash
			path:       "/etc/ssl/certs/ca-certificates.crt",
			wantPrefix: "etc-ssl-certs-ca-certificates-crt-",
		},
		{
			// Uppercase is invalid -> sanitized with hash
			path:       "/VAR/lib/K0s",
			wantPrefix: "var-lib-k0s-",
		},
		{
			// Single alphanumeric: valid, no hash
			path: "/a",
			want: "a",
		},
		{
			// Dot in path segment -> sanitized with hash
			path:       "/root/.aws/credentials",
			wantPrefix: "root-aws-credentials-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pathToVolumeName(tt.path)

			// Must be at most 63 chars (DNS label limit)
			assert.LessOrEqual(t, len(got), 63, "volume name exceeds DNS label limit")

			// Must be a valid DNS label
			assert.Regexp(t, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, got, "volume name must be a valid DNS label")

			if tt.want != "" {
				assert.Equal(t, tt.want, got)
			} else {
				assert.True(t, strings.HasPrefix(got, tt.wantPrefix), "expected prefix %q, got %q", tt.wantPrefix, got)
			}
		})
	}

	t.Run("unique names for paths that sanitize to the same string", func(t *testing.T) {
		paths := []string{
			"/my_path",
			"/my.path",
		}
		names := make(map[string]string)
		for _, p := range paths {
			name := pathToVolumeName(p)
			for prev, prevName := range names {
				assert.NotEqual(t, prevName, name, "paths %q and %q produced the same volume name %q", prev, p, name)
			}
			names[p] = name
		}
	})

	t.Run("long path is truncated to 63 chars", func(t *testing.T) {
		longPath := "/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/aa/bb/cc/dd/ee/ff"
		got := pathToVolumeName(longPath)
		assert.LessOrEqual(t, len(got), 63)
		assert.Regexp(t, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, got)
	})
}

// TestGenerateStatefulSet_doesNotEditClusterAnnotations is the regression test
// for the K0smotronControlPlane sync loop: generateStatefulSet drops the cluster
// spec hash from the StatefulSet annotations. When that map was the Cluster's
// own annotations, the delete was persisted on the Cluster by the deferred
// patch, the control plane controller lost its sync marker and rewrote the
// spec on every reconcile.
func TestGenerateStatefulSet_doesNotEditClusterAnnotations(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "kmc",
			Namespace:   "default",
			Annotations: map[string]string{km.AnnotationKeyClusterSpecHash: "abc123", "own": "cluster"},
		},
		Spec: km.ClusterSpec{
			Replicas:                 1,
			KubeconfigSecretMetadata: km.SecretMetadata{Annotations: map[string]string{"from": "kubeconfig-secret"}},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apps.AddToScheme(scheme))
	scope := &kmcScope{client: fake.NewClientBuilder().WithScheme(scheme).Build()}

	sts, _, err := scope.generateStatefulSet(context.Background(), kmc)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{km.AnnotationKeyClusterSpecHash: "abc123", "own": "cluster"}, kmc.Annotations,
		"generateStatefulSet must not edit the Cluster's own annotations")
	assert.NotContains(t, sts.Annotations, km.AnnotationKeyClusterSpecHash)
	assert.Equal(t, "cluster", sts.Annotations["own"])
	assert.Equal(t, "kubeconfig-secret", sts.Annotations["from"])
}
