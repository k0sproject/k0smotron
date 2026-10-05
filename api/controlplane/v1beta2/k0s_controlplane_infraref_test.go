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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

var (
	oldRef = corev1.ObjectReference{
		APIVersion: "infrastructure.cluster.x-k8s.io/v1beta2",
		Kind:       "DockerMachineTemplate",
		Name:       "cp-template",
	}
	otherRef = corev1.ObjectReference{
		APIVersion: "infrastructure.cluster.x-k8s.io/v1beta2",
		Kind:       "DockerMachineTemplate",
		Name:       "a-different-template",
	}
)

// The nested field follows the contract shape, so its fixture is the same reference converted
// rather than a second one that could drift from the first.
var oldContractRef = ContractRefFromObjectReference(oldRef)

func templateWith(deprecated corev1.ObjectReference, nested clusterv1.ContractVersionedObjectReference) *K0sControlPlaneMachineTemplate {
	return &K0sControlPlaneMachineTemplate{
		InfrastructureRef: deprecated,
		Spec:              K0sControlPlaneMachineTemplateSpec{InfrastructureRef: nested},
	}
}

// TestInfraRefFallsBackToTheDeprecatedField covers the objects the webhook can never reach, since
// one stored before the field moved is only re-admitted when something writes to it.
func TestInfraRefFallsBackToTheDeprecatedField(t *testing.T) {
	none := corev1.ObjectReference{}
	noneContract := clusterv1.ContractVersionedObjectReference{}

	tests := []struct {
		name       string
		deprecated corev1.ObjectReference
		nested     clusterv1.ContractVersionedObjectReference
		want       clusterv1.ContractVersionedObjectReference
	}{
		{"only the deprecated field, an object stored before the move", oldRef, noneContract, oldContractRef},
		{"only the nested field, the shape being moved to", none, oldContractRef, oldContractRef},
		{"both agree, which is what a defaulted object looks like", oldRef, oldContractRef, oldContractRef},
		{"the nested field wins on a pair the defaulter has not seen", otherRef, oldContractRef, oldContractRef},
		{"neither is set, which admission refuses separately", none, noneContract, noneContract},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, templateWith(tt.deprecated, tt.nested).InfraRef())
		})
	}
}

// TestDefaultMigratesTheDeprecatedInfrastructureRef covers the other half. The read helper keeps an
// old object working, this is what moves a written one onto the new shape.
func TestDefaultMigratesTheDeprecatedInfrastructureRef(t *testing.T) {
	defaulter := &K0sControlPlaneDefaulter{}

	t.Run("an empty nested field is filled from the deprecated one", func(t *testing.T) {
		kcp := &K0sControlPlane{Spec: K0sControlPlaneSpec{
			MachineTemplate: templateWith(oldRef, clusterv1.ContractVersionedObjectReference{}),
		}}
		require.NoError(t, defaulter.Default(t.Context(), kcp))

		require.Equal(t, oldContractRef, kcp.Spec.MachineTemplate.Spec.InfrastructureRef)
		require.Equal(t, oldRef, kcp.Spec.MachineTemplate.InfrastructureRef,
			"the deprecated field stays, since removing what the user wrote makes the next apply put it back")
	})

	// Setting the nested field says the deprecated one is a leftover from the migration, so a
	// disagreeing pair resolves to the new API rather than to whatever was left behind.
	t.Run("a set nested field is never overwritten by the deprecated one", func(t *testing.T) {
		kcp := &K0sControlPlane{Spec: K0sControlPlaneSpec{
			MachineTemplate: templateWith(otherRef, oldContractRef),
		}}
		require.NoError(t, defaulter.Default(t.Context(), kcp))

		require.Equal(t, oldContractRef, kcp.Spec.MachineTemplate.Spec.InfrastructureRef,
			"the nested field is the one the user is expected to maintain")
	})

	t.Run("a nested only object is left alone", func(t *testing.T) {
		kcp := &K0sControlPlane{Spec: K0sControlPlaneSpec{
			MachineTemplate: templateWith(corev1.ObjectReference{}, oldContractRef),
		}}
		require.NoError(t, defaulter.Default(t.Context(), kcp))

		require.Equal(t, oldContractRef, kcp.Spec.MachineTemplate.Spec.InfrastructureRef)
		require.Equal(t, corev1.ObjectReference{}, kcp.Spec.MachineTemplate.InfrastructureRef,
			"nothing writes backwards into the deprecated field")
	})

	t.Run("no machine template at all does not panic", func(t *testing.T) {
		require.NoError(t, defaulter.Default(t.Context(), &K0sControlPlane{}))
	})
}

// TestValidateInfrastructureRef covers what replaces the schema, which is the only thing left
// saying a machine template has to name a template at all.
func TestValidateInfrastructureRef(t *testing.T) {
	none := corev1.ObjectReference{}
	validator := &K0sControlPlaneValidator{}

	tests := []struct {
		name        string
		deprecated  corev1.ObjectReference
		nested      clusterv1.ContractVersionedObjectReference
		wantErr     string
		wantWarning bool
	}{
		{name: "the nested field alone is accepted in silence", nested: oldContractRef},
		{name: "the deprecated field alone is accepted with a warning", deprecated: oldRef, wantWarning: true},
		{name: "both set to the same template still warns", deprecated: oldRef, nested: oldContractRef, wantWarning: true},
		{
			// Defaulting leaves the nested field alone, so a pair that disagrees means the
			// deprecated one is a leftover rather than a conflict to refuse.
			name:       "a pair that still disagrees is not an error",
			deprecated: otherRef, nested: oldContractRef,
			wantWarning: true,
		},
		{
			name:       "neither is set, which the schema used to catch",
			deprecated: none, nested: clusterv1.ContractVersionedObjectReference{},
			wantErr: "is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kcp := &K0sControlPlane{Spec: K0sControlPlaneSpec{
				Version:         "v1.30.0+k0s.0",
				MachineTemplate: templateWith(tt.deprecated, tt.nested),
			}}

			err := denyMissingInfrastructureRef(kcp, field.NewPath("spec"))
			// Nil rather than NoError, since a nil *field.Error handed to an error
			// parameter is an interface that is not itself nil.
			if tt.wantErr == "" {
				require.Nil(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}

			warnings := validator.validateInfrastructureRef(kcp)
			if tt.wantWarning {
				require.NotEmpty(t, warnings, "using the deprecated field has to say so")
				require.Contains(t, warnings[0], "spec.machineTemplate.spec.infrastructureRef")
			} else {
				require.Empty(t, warnings)
			}
		})
	}
}
