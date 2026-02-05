package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &K0sControllerConfig{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (c *K0sControllerConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControllerConfig)
	dst.ObjectMeta = *c.ObjectMeta.DeepCopy()

	dst.Spec = k0sControllerConfigV1beta1ToV1beta2Spec(c.Spec)
	dst.Status = v1beta2.K0sControllerConfigStatus{
		Ready:          c.Status.Ready,
		DataSecretName: c.Status.DataSecretName,
		Conditions:     c.Status.Conditions,
	}
	if c.Status.DataSecretName != nil && *c.Status.DataSecretName != "" {
		dst.Status.Initialization.DataSecretCreated = true
	}

	return nil
}

func k0sControllerConfigV1beta1ToV1beta2Spec(spec K0sControllerConfigSpec) v1beta2.K0sControllerConfigSpec {
	res := v1beta2.K0sControllerConfigSpec{
		Version: spec.Version,
	}
	if spec.K0sConfigSpec != nil {
		res.K0sConfigSpec = ConvertK0sConfigSpecV1beta1ToV1beta2(spec.K0sConfigSpec)
	}
	return res
}

// ConvertK0sConfigSpecV1beta1ToV1beta2 converts K0sConfigSpec from v1beta1 to v1beta2, handling the changes in the provisioner field.
func ConvertK0sConfigSpecV1beta1ToV1beta2(spec *K0sConfigSpec) *v1beta2.K0sConfigSpec {
	if spec == nil {
		return nil
	}
	res := &v1beta2.K0sConfigSpec{
		Provisioner: v1beta2.ProvisionerSpec{
			// Default to CloudInit, will be overridden below if Ignition is set
			Type: provisioner.CloudInitProvisioningFormat,
		},
		K0sInstallDir:     spec.K0sInstallDir,
		K0s:               spec.K0s,
		UseSystemHostname: spec.UseSystemHostname,
		Files:             spec.Files,
		Args:              spec.Args,
		PreK0sCommands:    spec.PreStartCommands,
		PostK0sCommands:   spec.PostStartCommands,
		PreInstalledK0s:   spec.PreInstalledK0s,
		DownloadURL:       spec.DownloadURL,
		Tunneling:         spec.Tunneling,
		WorkingDir:        spec.WorkingDir,
	}
	if spec.Ignition != nil {
		res.Provisioner = v1beta2.ProvisionerSpec{
			Type:     provisioner.IgnitionProvisioningFormat,
			Ignition: spec.Ignition,
		}
	}
	if spec.CustomUserDataRef != nil {
		res.Provisioner.CustomUserDataRef = spec.CustomUserDataRef
	}
	return res
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (c *K0sControllerConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControllerConfig)
	c.ObjectMeta = src.ObjectMeta

	c.Spec = k0sControllerConfigSpecV1beta2ToV1beta1(src.Spec)
	c.Status = K0sControllerConfigStatus{
		Ready:          src.Status.Ready,
		DataSecretName: src.Status.DataSecretName,
		Conditions:     src.Status.Conditions,
	}

	return nil
}

func k0sControllerConfigSpecV1beta2ToV1beta1(spec v1beta2.K0sControllerConfigSpec) K0sControllerConfigSpec {
	res := K0sControllerConfigSpec{
		Version: spec.Version,
	}
	if spec.K0sConfigSpec != nil {
		res.K0sConfigSpec = ConvertK0sConfigSpecV1beta2ToV1beta1(spec.K0sConfigSpec)
	}
	return res
}

// ConvertK0sConfigSpecV1beta2ToV1beta1 converts K0sConfigSpec from v1beta2 to v1beta1, handling the changes in the provisioner field.
func ConvertK0sConfigSpecV1beta2ToV1beta1(spec *v1beta2.K0sConfigSpec) *K0sConfigSpec {
	if spec == nil {
		return nil
	}
	res := &K0sConfigSpec{
		K0sInstallDir:     spec.K0sInstallDir,
		K0s:               spec.K0s,
		UseSystemHostname: spec.UseSystemHostname,
		Files:             spec.Files,
		Args:              spec.Args,
		PreStartCommands:  spec.PreK0sCommands,
		PostStartCommands: spec.PostK0sCommands,
		PreInstalledK0s:   spec.PreInstalledK0s,
		DownloadURL:       spec.DownloadURL,
		Tunneling:         spec.Tunneling,
		WorkingDir:        spec.WorkingDir,
	}
	if spec.Provisioner.Type == provisioner.IgnitionProvisioningFormat {
		res.Ignition = spec.Provisioner.Ignition
	}
	if spec.Provisioner.CustomUserDataRef != nil {
		res.CustomUserDataRef = spec.Provisioner.CustomUserDataRef
	}
	return res
}
