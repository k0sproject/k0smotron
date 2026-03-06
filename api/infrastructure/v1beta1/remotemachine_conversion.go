package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/infrastructure/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &RemoteMachine{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (rm *RemoteMachine) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.RemoteMachine)
	dst.ObjectMeta = *rm.ObjectMeta.DeepCopy()
	dst.Spec = convertRemoteMachineSpecV1beta1ToV1beta2(rm.Spec)
	dst.Status = *rm.Status.DeepCopy()
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (rm *RemoteMachine) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.RemoteMachine)
	rm.ObjectMeta = src.ObjectMeta
	rm.Spec = convertRemoteMachineSpecV1beta2ToV1beta1(src.Spec)
	rm.Status = *rm.Status.DeepCopy()
	return nil
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
