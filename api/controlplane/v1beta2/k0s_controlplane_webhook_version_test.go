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
)

func TestValidateUpdateHonoursTheVersionSkewPolicy(t *testing.T) {
	kcp := func(v string) *K0sControlPlane {
		return &K0sControlPlane{
			Spec: K0sControlPlaneSpec{
				Version:         v,
				K0sConfigSpec:   bootstrapv1.K0sConfigSpec{},
				MachineTemplate: &K0sControlPlaneMachineTemplate{},
			},
		}
	}

	for _, tc := range []struct {
		name      string
		old, want string
		allowed   bool
	}{
		{name: "one minor forward", old: "v1.31.1+k0s.0", want: "v1.32.0+k0s.0", allowed: true},
		{name: "two minors forward", old: "v1.31.1+k0s.0", want: "v1.33.0+k0s.0", allowed: false},
		{name: "a patch", old: "v1.31.1+k0s.0", want: "v1.31.4+k0s.0", allowed: true},
		{name: "a downgrade, which upstream allows too", old: "v1.31.1+k0s.0", want: "v1.30.0+k0s.0", allowed: true},
		// A minor only comparison reads this as thirty one versions backwards.
		{name: "a major bump", old: "v1.31.1+k0s.0", want: "v2.0.0+k0s.0", allowed: false},
		{name: "a major bump carrying a low minor", old: "v1.31.1+k0s.0", want: "v2.1.0+k0s.0", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &K0sControlPlaneValidator{}

			_, err := v.ValidateUpdate(context.Background(), kcp(tc.old), kcp(tc.want))

			if tc.allowed {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "not allowed by the Kubernetes skew policy")
		})
	}
}
