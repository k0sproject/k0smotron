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

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

// K0sControllerConfig has no webhook of its own, so the control plane webhook is
// the only admission time check for the files it will generate.
func TestValidateK0sControlPlaneChecksFileOwners(t *testing.T) {
	kcp := func(owner string) *K0sControlPlane {
		return &K0sControlPlane{
			Spec: K0sControlPlaneSpec{
				Version: "v1.30.0+k0s.0",
				K0sConfigSpec: bootstrapv1.K0sConfigSpec{
					Files: []bootstrapv1.File{
						{File: provisioner.File{Path: "/etc/thing", Content: "x", Owner: owner}},
					},
				},
			},
		}
	}

	t.Run("a sane owner is accepted", func(t *testing.T) {
		require.NoError(t, validateK0sControlPlane(kcp("etcd:etcd")))
	})

	t.Run("no owner is accepted", func(t *testing.T) {
		require.NoError(t, validateK0sControlPlane(kcp("")))
	})

	t.Run("an owner that could be read as an argument is rejected", func(t *testing.T) {
		err := validateK0sControlPlane(kcp("root; rm -rf /"))
		require.ErrorContains(t, err, "spec.k0sConfigSpec.files[0].owner")
	})

	t.Run("command substitution is rejected", func(t *testing.T) {
		require.Error(t, validateK0sControlPlane(kcp("$(id -u)")))
	})
}
