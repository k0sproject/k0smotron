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
package v1beta2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestClusterValidator_validateVersionSuffix(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    admission.Warnings
	}{
		{
			name:    "version without k0s suffix",
			version: "v1.23.4",
			want:    admission.Warnings{"The specified version 'v1.23.4' requires a k0s suffix (k0s.<number>). Using 'v1.23.4-k0s.0' instead."},
		},
		{
			name:    "version with k0s suffix",
			version: "v1.23.4-k0s.2",
			want:    admission.Warnings{},
		},
		{
			name:    "empty version",
			version: "",
			want:    admission.Warnings{},
		},
		{
			name:    "version with +k0s. suffix",
			version: "v1.23.4+k0s.2",
			want:    admission.Warnings{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c ClusterValidator
			require.Equal(t, tt.want, c.validateVersionSuffix(tt.version))
		})
	}
}

func TestValidateEtcdVersionUpgrade(t *testing.T) {
	tests := []struct {
		name        string
		oldImage    string
		newImage    string
		wantError   bool
		wantWarning bool
	}{
		{
			name:     "same version",
			oldImage: "quay.io/k0sproject/etcd:v3.5.13",
			newImage: "quay.io/k0sproject/etcd:v3.5.13",
		},
		{
			name:     "one minor version upgrade",
			oldImage: "quay.io/k0sproject/etcd:v3.5.13",
			newImage: "quay.io/k0sproject/etcd:v3.6.0",
		},
		{
			name:     "patch version upgrade",
			oldImage: "quay.io/k0sproject/etcd:v3.5.13",
			newImage: "quay.io/k0sproject/etcd:v3.5.14",
		},
		{
			name:     "downgrade is not blocked by this check",
			oldImage: "quay.io/k0sproject/etcd:v3.6.0",
			newImage: "quay.io/k0sproject/etcd:v3.5.13",
		},
		{
			name:      "skipping a minor version is rejected",
			oldImage:  "quay.io/k0sproject/etcd:v3.5.13",
			newImage:  "quay.io/k0sproject/etcd:v3.7.1",
			wantError: true,
		},
		{
			name:        "unparsable old tag skips the check with a warning",
			oldImage:    "quay.io/k0sproject/etcd:latest",
			newImage:    "quay.io/k0sproject/etcd:v3.7.1",
			wantWarning: true,
		},
		{
			name:        "unparsable new tag skips the check with a warning",
			oldImage:    "quay.io/k0sproject/etcd:v3.5.13",
			newImage:    "quay.io/k0sproject/etcd:latest",
			wantWarning: true,
		},
		{
			name:        "custom image without a tag skips the check with a warning",
			oldImage:    "myregistry.example.com:5000/etcd",
			newImage:    "myregistry.example.com:5000/etcd",
			wantWarning: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings, err := validateEtcdVersionUpgrade(tt.oldImage, tt.newImage)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.wantWarning {
				require.NotEmpty(t, warnings)
			} else {
				require.Empty(t, warnings)
			}
		})
	}
}

func TestCluster_ValidateCertificates(t *testing.T) {
	dur := func(d time.Duration) *metav1.Duration { return &metav1.Duration{Duration: d} }

	tests := []struct {
		name        string
		certs       *CertificatesSpec
		wantErr     bool
		errContains string
	}{
		{name: "nil is valid", certs: nil},
		{name: "defaults are valid", certs: &CertificatesSpec{
			Duration: dur(8760 * time.Hour), RenewBefore: dur(720 * time.Hour)}},
		{
			name:        "renewBefore equal to duration is rejected",
			certs:       &CertificatesSpec{Duration: dur(48 * time.Hour), RenewBefore: dur(48 * time.Hour)},
			wantErr:     true,
			errContains: "renewBefore must be less than duration",
		},
		{
			name:        "renewBefore greater than duration is rejected",
			certs:       &CertificatesSpec{Duration: dur(24 * time.Hour), RenewBefore: dur(48 * time.Hour)},
			wantErr:     true,
			errContains: "renewBefore must be less than duration",
		},
		{
			name:        "zero duration is rejected",
			certs:       &CertificatesSpec{Duration: dur(0), RenewBefore: dur(time.Hour)},
			wantErr:     true,
			errContains: "duration must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
				Spec:       ClusterSpec{Certificates: tc.certs},
			}
			err := c.validateCertificates()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			assert.NoError(t, err)
		})
	}
}
