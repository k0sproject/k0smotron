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
	"github.com/k0sproject/k0smotron/api/infrastructure/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &RemoteMachine{}
var _ conversion.Convertible = &PooledRemoteMachine{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (rm *RemoteMachine) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.RemoteMachine)
	dst.ObjectMeta = *rm.ObjectMeta.DeepCopy()
	dst.Spec = convertRemoteMachineSpecV1beta1ToV1beta2(rm.Spec)
	dst.Status = v1beta2.RemoteMachineStatus{
		Initialization: v1beta2.RemoteMachineInitializationStatus{
			Provisioned: &rm.Status.Ready,
		},
		Addresses:      rm.Status.Addresses,
		FailureReason:  rm.Status.FailureReason,
		FailureMessage: rm.Status.FailureMessage,
	}

	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (rm *RemoteMachine) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.RemoteMachine)
	rm.ObjectMeta = src.ObjectMeta
	rm.Spec = convertRemoteMachineSpecV1beta2ToV1beta1(src.Spec)
	ready := false
	if src.Status.Initialization.Provisioned != nil {
		ready = *src.Status.Initialization.Provisioned
	}
	rm.Status = RemoteMachineStatus{
		Ready:          ready,
		Addresses:      src.Status.Addresses,
		FailureReason:  src.Status.FailureReason,
		FailureMessage: src.Status.FailureMessage,
	}
	return nil
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (prm *PooledRemoteMachine) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.PooledRemoteMachine)
	dst.ObjectMeta = *prm.ObjectMeta.DeepCopy()
	dst.Spec = convertPooledRemoteMachineSpecV1beta1ToV1beta2(prm.Spec)
	dst.Status = *prm.Status.DeepCopy()
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (prm *PooledRemoteMachine) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.PooledRemoteMachine)
	prm.ObjectMeta = src.ObjectMeta
	prm.Spec = convertPooledRemoteMachineSpecV1beta2ToV1beta1(src.Spec)
	prm.Status = *src.Status.DeepCopy()
	return nil
}

func convertPooledRemoteMachineSpecV1beta1ToV1beta2(spec PooledRemoteMachineSpec) v1beta2.PooledRemoteMachineSpec {
	return v1beta2.PooledRemoteMachineSpec{
		Pool: spec.Pool,
		Machine: v1beta2.PooledMachineSpec{
			Address:          spec.Machine.Address,
			Port:             spec.Machine.Port,
			User:             spec.Machine.User,
			UseSudo:          spec.Machine.UseSudo,
			CommandsAsScript: spec.Machine.CommandsAsScript,
			WorkingDir:       spec.Machine.WorkingDir,
			CleanUpCommands:  spec.Machine.CustomCleanUpCommands,
			SSHKeyRef:        spec.Machine.SSHKeyRef,
		},
	}
}

func convertPooledRemoteMachineSpecV1beta2ToV1beta1(spec v1beta2.PooledRemoteMachineSpec) PooledRemoteMachineSpec {
	return PooledRemoteMachineSpec{
		Pool: spec.Pool,
		Machine: PooledMachineSpec{
			Address:               spec.Machine.Address,
			Port:                  spec.Machine.Port,
			User:                  spec.Machine.User,
			UseSudo:               spec.Machine.UseSudo,
			CommandsAsScript:      spec.Machine.CommandsAsScript,
			WorkingDir:            spec.Machine.WorkingDir,
			CustomCleanUpCommands: spec.Machine.CleanUpCommands,
			SSHKeyRef:             spec.Machine.SSHKeyRef,
		},
	}
}

func convertRemoteMachineSpecV1beta1ToV1beta2(spec RemoteMachineSpec) v1beta2.RemoteMachineSpec {
	return v1beta2.RemoteMachineSpec{
		Pool:             spec.Pool,
		ProviderID:       spec.ProviderID,
		Address:          spec.Address,
		Port:             spec.Port,
		User:             spec.User,
		UseSudo:          spec.UseSudo,
		CommandsAsScript: spec.CommandsAsScript,
		WorkingDir:       spec.WorkingDir,
		SSHKeyRef:        spec.SSHKeyRef,
		CleanUpCommands:  spec.CustomCleanUpCommands,
		ProvisionJob:     spec.ProvisionJob,
	}
}

func convertRemoteMachineSpecV1beta2ToV1beta1(spec v1beta2.RemoteMachineSpec) RemoteMachineSpec {
	return RemoteMachineSpec{
		Pool:                  spec.Pool,
		ProviderID:            spec.ProviderID,
		Address:               spec.Address,
		Port:                  spec.Port,
		User:                  spec.User,
		UseSudo:               spec.UseSudo,
		CommandsAsScript:      spec.CommandsAsScript,
		WorkingDir:            spec.WorkingDir,
		SSHKeyRef:             spec.SSHKeyRef,
		CustomCleanUpCommands: spec.CleanUpCommands,
		ProvisionJob:          spec.ProvisionJob,
	}
}
