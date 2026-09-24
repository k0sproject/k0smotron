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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeK0sVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "empty version is kept", version: "", want: ""},
		{name: "plain kubernetes version gets the default suffix", version: "v1.36.2", want: "v1.36.2+k0s.0"},
		{name: "k0s version is kept", version: "v1.36.2+k0s.0", want: "v1.36.2+k0s.0"},
		{name: "non-default k0s suffix is kept", version: "v1.36.2+k0s.1", want: "v1.36.2+k0s.1"},
		{name: "image tag suffix is converted", version: "v1.36.2-k0s.1", want: "v1.36.2+k0s.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeK0sVersion(tt.version))
		})
	}
}
