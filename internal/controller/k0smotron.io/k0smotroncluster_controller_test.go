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

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kcerts "github.com/k0sproject/k0smotron/v2/internal/certs"
)

func TestFilterPurposes(t *testing.T) {
	all := []kcerts.Info{
		{Purpose: "etcd-server", Serial: "1"},
		{Purpose: "etcd-peer", Serial: "2"},
		{Purpose: "apiserver-etcd-client", Serial: "3"},
		{Purpose: "ingress-haproxy", Serial: "4"},
	}

	tests := []struct {
		name     string
		infos    []kcerts.Info
		purposes []string
		want     []kcerts.Info
	}{
		{
			name:     "filters to a single purpose",
			infos:    all,
			purposes: []string{"etcd-server"},
			want:     []kcerts.Info{{Purpose: "etcd-server", Serial: "1"}},
		},
		{
			name:     "filters to two purposes",
			infos:    all,
			purposes: []string{"etcd-server", "etcd-peer"},
			want: []kcerts.Info{
				{Purpose: "etcd-server", Serial: "1"},
				{Purpose: "etcd-peer", Serial: "2"},
			},
		},
		{
			name:     "purpose not present yields empty, no panic",
			infos:    all,
			purposes: []string{"does-not-exist"},
			want:     []kcerts.Info{},
		},
		{
			name:     "empty input yields empty",
			infos:    nil,
			purposes: []string{"etcd-server"},
			want:     []kcerts.Info{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterPurposes(tt.infos, tt.purposes...)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFingerprintScoping_PerWorkloadIndependence proves that renewing a
// certificate mounted by one workload does not change the fingerprint of a
// workload that does not mount it. This is the scoping the controller relies
// on to avoid rolling the control plane when only etcd-peer is renewed (and
// vice versa) -- a real availability cost on every renewal if it regressed.
func TestFingerprintScoping_PerWorkloadIndependence(t *testing.T) {
	baseline := []kcerts.Info{
		{Purpose: "etcd-server", Serial: "aaa"},
		{Purpose: "etcd-peer", Serial: "bbb"},
		{Purpose: "apiserver-etcd-client", Serial: "ccc"},
	}

	fingerprints := func(infos []kcerts.Info) (etcd, controlplane string) {
		etcd = kcerts.Fingerprint(filterPurposes(infos, "etcd-server", "etcd-peer"))
		controlplane = kcerts.Fingerprint(filterPurposes(infos, "apiserver-etcd-client"))
		return
	}

	baseEtcdFP, baseCPFP := fingerprints(baseline)

	t.Run("changing only etcd-peer changes etcd fingerprint but not controlplane", func(t *testing.T) {
		changed := []kcerts.Info{
			{Purpose: "etcd-server", Serial: "aaa"},
			{Purpose: "etcd-peer", Serial: "new-serial"},
			{Purpose: "apiserver-etcd-client", Serial: "ccc"},
		}

		etcdFP, cpFP := fingerprints(changed)

		assert.NotEqual(t, baseEtcdFP, etcdFP, "etcd fingerprint must change when etcd-peer is renewed")
		assert.Equal(t, baseCPFP, cpFP, "control-plane fingerprint must NOT change when only etcd-peer is renewed")
	})

	t.Run("changing only apiserver-etcd-client changes controlplane fingerprint but not etcd", func(t *testing.T) {
		changed := []kcerts.Info{
			{Purpose: "etcd-server", Serial: "aaa"},
			{Purpose: "etcd-peer", Serial: "bbb"},
			{Purpose: "apiserver-etcd-client", Serial: "new-serial"},
		}

		etcdFP, cpFP := fingerprints(changed)

		assert.Equal(t, baseEtcdFP, etcdFP, "etcd fingerprint must NOT change when only apiserver-etcd-client is renewed")
		assert.NotEqual(t, baseCPFP, cpFP, "control-plane fingerprint must change when apiserver-etcd-client is renewed")
	})
}

func TestClearRenewCertificatesAnnotation(t *testing.T) {
	t.Run("annotation present", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					km.RenewCertificatesAnnotation: "",
					"other-annotation":             "keep-me",
				},
			},
		}

		got := clearRenewCertificatesAnnotation(kmc)

		assert.True(t, got)
		_, stillPresent := kmc.Annotations[km.RenewCertificatesAnnotation]
		assert.False(t, stillPresent)
		assert.Equal(t, "keep-me", kmc.Annotations["other-annotation"])
	})

	t.Run("annotation absent", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"other-annotation": "keep-me"},
			},
		}

		got := clearRenewCertificatesAnnotation(kmc)

		assert.False(t, got)
		assert.Equal(t, "keep-me", kmc.Annotations["other-annotation"])
	})

	t.Run("nil annotations map does not panic", func(t *testing.T) {
		kmc := &km.Cluster{}

		var got bool
		assert.NotPanics(t, func() {
			got = clearRenewCertificatesAnnotation(kmc)
		})
		assert.False(t, got)
		assert.Nil(t, kmc.Annotations)
	})
}
