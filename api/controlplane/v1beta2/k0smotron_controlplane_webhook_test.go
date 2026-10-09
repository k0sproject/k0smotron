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

	k0smotroniov1beta2 "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

// A version change from or to an empty value must not skip the rest of the update validation.
func TestK0smotronControlPlaneValidateUpdateDataDirImmutable(t *testing.T) {
	kcp := func(version string, flags ...string) *K0smotronControlPlane {
		return &K0smotronControlPlane{
			Spec: k0smotroniov1beta2.ClusterSpec{Version: version, ControlPlaneFlags: flags},
		}
	}

	_, err := (&K0smotronControlPlaneValidator{}).ValidateUpdate(context.Background(),
		kcp(""), kcp("v1.30.0+k0s.0", "--data-dir=/data"))
	require.ErrorContains(t, err, "data directory cannot be changed")
}
