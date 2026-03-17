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
