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
	"context"
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

// TestValidateK0sControlPlaneWarnsOnIgnoredProvisionerFields covers the hint reaching
// the control plane path as well, where the field is set far from the provisioner.
func TestValidateK0sControlPlaneWarnsOnIgnoredProvisionerFields(t *testing.T) {
	kcp := &K0sControlPlane{
		Spec: K0sControlPlaneSpec{
			Version: "v1.30.0+k0s.0",
			K0sConfigSpec: bootstrapv1.K0sConfigSpec{
				Provisioner: bootstrapv1.ProvisionerSpec{
					Type: provisioner.IgnitionProvisioningFormat,
					CustomUserDataRef: &bootstrapv1.ContentSource{
						SecretRef: &bootstrapv1.ContentSourceRef{Name: "extra", Key: "userdata"},
					},
				},
			},
		},
	}

	warnings, err := (&K0sControlPlaneValidator{}).ValidateCreate(context.Background(), kcp)

	require.NoError(t, err, "the control plane is still accepted")
	require.Contains(t, warnings, "spec.k0sConfigSpec.provisioner.customUserDataRef is ignored by the ignition provisioner, use provisioner.ignition.additionalConfig instead")
}

// TestValidateK0sControlPlaneWarnsOnUpdate covers the update path, where the warning has to
// survive being returned next to an error rather than instead of one.
func TestValidateK0sControlPlaneWarnsOnUpdate(t *testing.T) {
	const warning = "spec.k0sConfigSpec.provisioner.customUserDataRef is ignored by the ignition provisioner, use provisioner.ignition.additionalConfig instead"

	kcp := func(version string) *K0sControlPlane {
		return &K0sControlPlane{
			Spec: K0sControlPlaneSpec{
				Version: version,
				K0sConfigSpec: bootstrapv1.K0sConfigSpec{
					Provisioner: bootstrapv1.ProvisionerSpec{
						Type: provisioner.IgnitionProvisioningFormat,
						CustomUserDataRef: &bootstrapv1.ContentSource{
							SecretRef: &bootstrapv1.ContentSourceRef{Name: "extra", Key: "userdata"},
						},
					},
				},
			},
		}
	}

	t.Run("an accepted update still warns", func(t *testing.T) {
		warnings, err := (&K0sControlPlaneValidator{}).ValidateUpdate(t.Context(), kcp("v1.30.0+k0s.0"), kcp("v1.30.1+k0s.0"))

		require.NoError(t, err, "the control plane is still accepted")
		require.Contains(t, warnings, warning)
	})

	t.Run("a rejected version skew keeps the warning", func(t *testing.T) {
		warnings, err := (&K0sControlPlaneValidator{}).ValidateUpdate(t.Context(), kcp("v1.28.0+k0s.0"), kcp("v1.30.0+k0s.0"))

		require.ErrorContains(t, err, "more than one minor version at a time")
		require.Contains(t, warnings, warning, "warnings are reported alongside the rejection")
	})
}
