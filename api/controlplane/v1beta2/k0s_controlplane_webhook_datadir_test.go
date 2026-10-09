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

func TestK0sControlPlaneValidateUpdateDataDirImmutable(t *testing.T) {
	kcp := func(args ...string) *K0sControlPlane {
		return &K0sControlPlane{
			Spec: K0sControlPlaneSpec{
				Version:       "v1.30.0+k0s.0",
				K0sConfigSpec: bootstrapv1.K0sConfigSpec{Args: args},
			},
		}
	}

	for _, tc := range []struct {
		name     string
		old, new *K0sControlPlane
		wantErr  bool
	}{
		{name: "unset to unset", old: kcp(), new: kcp()},
		{name: "unset to explicit default", old: kcp(), new: kcp("--data-dir=/var/lib/k0s")},
		{name: "custom value in different arg", old: kcp("--data-dir=/data"), new: kcp("--data-dir", "/data"), wantErr: true},
		{name: "unset to custom", old: kcp(), new: kcp("--data-dir=/data"), wantErr: true},
		{name: "custom to other", old: kcp("--data-dir=/data"), new: kcp("--data-dir=/other"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&K0sControlPlaneValidator{}).ValidateUpdate(context.Background(), tc.old, tc.new)
			if tc.wantErr {
				require.ErrorContains(t, err, "data directory cannot be changed")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
