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

package v1beta1

import (
	"testing"

	"github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/require"
)

func TestK0sControlPlaneConversionKeepsRemediation(t *testing.T) {
	hub := &v1beta2.K0sControlPlane{Spec: v1beta2.K0sControlPlaneSpec{
		Remediation: v1beta2.K0sControlPlaneRemediationSpec{
			MinHealthyPeriodSeconds: new(int32(120)),
			RetryPeriodSeconds:      new(int32(30)),
		},
	}}

	spoke := &K0sControlPlane{}
	require.NoError(t, spoke.ConvertFrom(hub))

	back := &v1beta2.K0sControlPlane{}
	require.NoError(t, spoke.ConvertTo(back))
	require.Equal(t, hub.Spec.Remediation, back.Spec.Remediation)
}
