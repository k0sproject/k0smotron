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
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

func Test_certificateAnnotations(t *testing.T) {
	assert.Nil(t, certificateAnnotations(""), "an empty fingerprint must not add an annotation, so existing clusters do not roll on upgrade")

	assert.Equal(t,
		map[string]string{certificateFingerprintAnnotation: "abc123"},
		certificateAnnotations("abc123"))
}

func Test_effectiveCertFingerprint(t *testing.T) {
	withAnnotation := &apps.StatefulSet{
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{certificateFingerprintAnnotation: "old-fingerprint"},
				},
			},
		},
	}
	withoutAnnotation := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "sts"},
	}
	// client-go's typed clientset never returns a nil StatefulSet, even when
	// one does not exist - only its Name is empty in that case.
	clientGoNotFound := &apps.StatefulSet{}

	tests := []struct {
		name        string
		existing    *apps.StatefulSet
		fingerprint string
		renewed     bool
		want        string
	}{
		{
			name:        "empty fingerprint is never stamped",
			existing:    withoutAnnotation,
			fingerprint: "",
			renewed:     true,
			want:        "",
		},
		{
			name:        "nil existing StatefulSet (does not exist yet) stamps unconditionally",
			existing:    nil,
			fingerprint: "abc123",
			renewed:     false,
			want:        "abc123",
		},
		{
			name:        "client-go empty-Name StatefulSet (does not exist yet) stamps unconditionally",
			existing:    clientGoNotFound,
			fingerprint: "abc123",
			renewed:     false,
			want:        "abc123",
		},
		{
			name:        "existing annotation present but not renewed is still stamped",
			existing:    withAnnotation,
			fingerprint: "abc123",
			renewed:     false,
			want:        "abc123",
		},
		{
			name:        "no existing annotation but a genuine renewal stamps it",
			existing:    withoutAnnotation,
			fingerprint: "abc123",
			renewed:     true,
			want:        "abc123",
		},
		{
			name:        "no existing annotation and no renewal adopts silently (the upgrade-roll regression)",
			existing:    withoutAnnotation,
			fingerprint: "abc123",
			renewed:     false,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveCertFingerprint(tt.existing, tt.fingerprint, tt.renewed)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test_effectiveCertFingerprintFromLookup covers the caller-side error
// handling that decides how a StatefulSet lookup's outcome feeds
// effectiveCertFingerprint: NotFound is a genuinely fresh cluster (stamp),
// any other error means the current state cannot be determined (never
// stamp, regardless of what the (possibly stale/zero) existing object looks
// like) - the fix for the transient-error regression from round 2.
func Test_effectiveCertFingerprintFromLookup(t *testing.T) {
	withoutAnnotation := &apps.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "sts"}}

	tests := []struct {
		name        string
		existing    *apps.StatefulSet
		getErr      error
		fingerprint string
		renewed     bool
		want        string
	}{
		{
			name:        "lookup succeeded, no annotation yet, not renewed -> adopt silently",
			existing:    withoutAnnotation,
			getErr:      nil,
			fingerprint: "abc123",
			renewed:     false,
			want:        "",
		},
		{
			name:        "NotFound -> genuinely fresh cluster, stamp unconditionally",
			existing:    withoutAnnotation, // must be ignored entirely on this path
			getErr:      apierrors.NewNotFound(apps.Resource("statefulsets"), "sts"),
			fingerprint: "abc123",
			renewed:     false,
			want:        "abc123",
		},
		{
			name:        "transient API error -> cannot tell, must not stamp",
			existing:    withoutAnnotation,
			getErr:      apierrors.NewInternalError(errors.New("etcdserver: request timed out")),
			fingerprint: "abc123",
			renewed:     false,
			want:        "",
		},
		{
			name:        "transient API error still must not stamp even if a renewal happened",
			existing:    withoutAnnotation,
			getErr:      apierrors.NewInternalError(errors.New("etcdserver: request timed out")),
			fingerprint: "abc123",
			renewed:     true,
			want:        "",
		},
		{
			name:        "empty fingerprint is never stamped regardless of lookup outcome",
			existing:    withoutAnnotation,
			getErr:      apierrors.NewInternalError(errors.New("boom")),
			fingerprint: "",
			renewed:     true,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveCertFingerprintFromLookup(tt.existing, tt.getErr, tt.fingerprint, tt.renewed)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newStatefulSetTestScope(t *testing.T, certFingerprints map[string]string) *kmcScope {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apps.AddToScheme(scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &kmcScope{client: c, certFingerprints: certFingerprints}
}

func TestGenerateStatefulSet_certificateFingerprintAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	scope := newStatefulSetTestScope(t, map[string]string{"controlplane": "abc123"})

	sts, _, err := scope.generateStatefulSet(context.Background(), kmc)
	require.NoError(t, err)
	assert.Equal(t, "abc123", sts.Spec.Template.Annotations[certificateFingerprintAnnotation])
}

func TestGenerateStatefulSet_emptyFingerprintOmitsAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	scope := newStatefulSetTestScope(t, map[string]string{"controlplane": ""})

	sts, _, err := scope.generateStatefulSet(context.Background(), kmc)
	require.NoError(t, err)
	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "an empty fingerprint must not add an annotation, so existing clusters do not roll on upgrade")
}

func TestGenerateStatefulSet_differentFingerprintsProduceDifferentTemplates(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	// generateStatefulSet mutates kmc.Annotations in place (via
	// util.AnnotationsForK0smotronCluster), so each call needs its own copy of
	// the cluster object, just as a real reconcile refetches a fresh one.
	scopeA := newStatefulSetTestScope(t, map[string]string{"controlplane": "abc123"})
	stsA, _, err := scopeA.generateStatefulSet(context.Background(), kmc.DeepCopy())
	require.NoError(t, err)

	scopeB := newStatefulSetTestScope(t, map[string]string{"controlplane": "def456"})
	stsB, _, err := scopeB.generateStatefulSet(context.Background(), kmc.DeepCopy())
	require.NoError(t, err)

	assert.NotEqual(t,
		stsA.Spec.Template.Annotations[certificateFingerprintAnnotation],
		stsB.Spec.Template.Annotations[certificateFingerprintAnnotation])
}

// TestGenerateStatefulSet_upgradeDoesNotRoll is the regression test for the
// upgrade-time restart: a cluster created by an older k0smotron has a
// StatefulSet whose pod template carries no certificate-fingerprint
// annotation. Certificates were not renewed this reconcile (they are
// perfectly valid). The newly generated pod template must therefore also
// carry no annotation - otherwise it differs from the existing template and
// Kubernetes rolls the control plane and etcd for nothing.
func TestGenerateStatefulSet_upgradeDoesNotRoll(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	scope := newStatefulSetTestScope(t, map[string]string{"controlplane": "abc123"})

	// Simulate a pre-existing StatefulSet, as created by a stable operator
	// that predates the fingerprint annotation.
	preExisting := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetStatefulSetName(),
			Namespace: kmc.Namespace,
		},
		Spec: apps.StatefulSetSpec{
			Selector: &metav1.LabelSelector{},
			Template: corev1.PodTemplateSpec{},
		},
	}
	require.NoError(t, scope.client.Create(context.Background(), preExisting)) //nolint:forbidigo // test setup: seeding the fake client directly, not a reconcile path

	// certificatesRenewed is left at its zero value (false): nothing was
	// renewed this reconcile.
	sts, _, err := scope.generateStatefulSet(context.Background(), kmc)
	require.NoError(t, err)

	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "adopting a pre-existing StatefulSet without a genuine renewal must not stamp the fingerprint, or the upgrade rolls the pods for nothing")
}
