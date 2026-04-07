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
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestK0sWorkerConfig_ConvertTo(t *testing.T) {
	oldObj := &K0sWorkerConfig{
		Spec: K0sWorkerConfigSpec{
			PreStartCommands: []string{
				"echo pre-start-1",
			},
		},
	}

	newObj := &v1beta2.K0sWorkerConfig{}
	err := oldObj.ConvertTo(newObj)
	require.NoError(t, err)
	require.Equal(t, []string{"echo pre-start-1"}, newObj.Spec.PreK0sCommands)
}
