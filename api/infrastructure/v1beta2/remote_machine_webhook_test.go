/*

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
)

func TestPooledRemoteMachineValidator(t *testing.T) {
	validator := &PooledRemoteMachineValidator{}
	oldMachine := &PooledRemoteMachine{
		Spec: PooledRemoteMachineSpec{
			Pool: "workers",
			Machine: PooledMachineSpec{
				Address: "10.0.0.1",
				CleanUpCommands: []string{
					"kubeadm reset -f",
				},
			},
		},
		Status: PooledRemoteMachineStatus{Reserved: true},
	}

	t.Run("allows metadata-only updates for reserved machines", func(t *testing.T) {
		updated := oldMachine.DeepCopy()
		updated.Labels = map[string]string{"environment": "test"}

		_, err := validator.ValidateUpdate(context.Background(), oldMachine, updated)
		require.NoError(t, err)
	})

	t.Run("rejects spec updates for reserved machines", func(t *testing.T) {
		updated := oldMachine.DeepCopy()
		updated.Spec.Machine.CleanUpCommands = []string{"different cleanup"}

		_, err := validator.ValidateUpdate(context.Background(), oldMachine, updated)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be changed while the pooled remote machine is reserved")
	})

	t.Run("allows spec updates for available machines", func(t *testing.T) {
		available := oldMachine.DeepCopy()
		available.Status.Reserved = false
		updated := available.DeepCopy()
		updated.Spec.Machine.Address = "10.0.0.2"

		_, err := validator.ValidateUpdate(context.Background(), available, updated)
		require.NoError(t, err)
	})
}
