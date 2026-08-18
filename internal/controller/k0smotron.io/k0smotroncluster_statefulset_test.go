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
	"strings"
	"testing"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func Test_k0sDataVolume(t *testing.T) {
	tests := []struct {
		name            string
		persistenceType string
		monitoring      bool
		wantNeeded      bool
		wantMountPath   string
	}{
		// Nothing in the data dir has to outlive the container, so no volume.
		{name: "default", persistenceType: "", wantNeeded: false},
		{name: "emptyDir", persistenceType: "emptyDir", wantNeeded: false},
		// Prometheus needs the k0s-generated admin cert. Only pki is covered,
		// so the binaries pre-unpacked into /var/lib/k0s/bin stay visible.
		{name: "monitoring", persistenceType: "emptyDir", monitoring: true, wantNeeded: true, wantMountPath: "/var/lib/k0s/pki"},
		// Deprecated persistence keeps covering the whole data directory.
		{name: "hostPath", persistenceType: "hostPath", wantNeeded: true, wantMountPath: "/var/lib/k0s"},
		{name: "pvc", persistenceType: "pvc", wantNeeded: true, wantMountPath: "/var/lib/k0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kmc := &km.Cluster{
				Spec: km.ClusterSpec{
					Persistence: km.PersistenceSpec{Type: tt.persistenceType},
					Monitoring:  km.MonitoringSpec{Enabled: tt.monitoring},
				},
			}

			name, mountPath, needed := k0sDataVolume(kmc)
			assert.Equal(t, tt.wantNeeded, needed)
			assert.Equal(t, tt.wantMountPath, mountPath)
			if needed {
				assert.NotEmpty(t, name)
			}
		})
	}
}

// The k0s image pre-unpacks the control plane binaries into /var/lib/k0s/bin,
// with cap_net_bind_service already set on kube-apiserver. A volume mounted
// over the data directory would shadow them, so by default there must not be
// one, and the certificates have to reach the pki directory without an init
// container carrying them across a shared volume.
func Test_generateStatefulSet_dataDirIsNotShadowed(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: km.ClusterSpec{
			CertificateRefs: []km.CertificateRef{{Type: "ca", Name: "test-cluster-ca"}},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, km.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	scope := &kmcScope{
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kmc).Build(),
	}

	sfs, _, err := scope.generateStatefulSet(t.Context(), kmc)
	require.NoError(t, err)

	assert.Empty(t, sfs.Spec.Template.Spec.InitContainers, "no init container should be needed")

	var mountPaths []string
	for _, m := range sfs.Spec.Template.Spec.Containers[0].VolumeMounts {
		mountPaths = append(mountPaths, m.MountPath)
	}
	assert.NotContains(t, mountPaths, "/var/lib/k0s", "the k0s data directory must not be shadowed by a mount")
	assert.Contains(t, mountPaths, certsMountPath, "certificates must be mounted for the entrypoint to copy")
}
