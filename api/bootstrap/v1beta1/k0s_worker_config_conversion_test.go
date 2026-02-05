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
