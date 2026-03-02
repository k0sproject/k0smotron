//go:build !envtest

package v1beta2

import (
	"testing"

	"github.com/stretchr/testify/require"
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
