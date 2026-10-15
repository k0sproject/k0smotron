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

	"github.com/stretchr/testify/require"
)

func TestValidateAcceptsAnUnsetVersion(t *testing.T) {
	v := &K0sControlPlaneValidator{}
	unset := &K0sControlPlane{}
	set := &K0sControlPlane{Spec: K0sControlPlaneSpec{Version: "v1.27.9+k0s.0"}}

	t.Run("create", func(t *testing.T) {
		_, err := v.ValidateCreate(t.Context(), unset)
		require.NoError(t, err)
	})

	t.Run("update to the defaulted version", func(t *testing.T) {
		_, err := v.ValidateUpdate(t.Context(), unset, set)
		require.NoError(t, err)
	})

	t.Run("clearing a set version is still rejected", func(t *testing.T) {
		_, err := v.ValidateUpdate(t.Context(), set, unset)
		require.ErrorContains(t, err, "failed to parse new version")
	})
}
