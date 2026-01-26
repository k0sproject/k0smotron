package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kccv1beta1 *K0sControllerConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControllerConfig)
	dst.ObjectMeta = kccv1beta1.ObjectMeta

	dst.Spec = k0sControllerConfigV1beta1ToV1beta2Spec(kccv1beta1.Spec)
	return nil
}

func k0sControllerConfigV1beta1ToV1beta2Spec(spec K0sControllerConfigSpec) v1beta2.K0sControllerConfigSpec {
	res := v1beta2.K0sControllerConfigSpec{
		Version: spec.Version,
	}
	if spec.K0sConfigSpec != nil {
		res.K0sConfigSpec = k0sConfigSpecV1beta1ToV1beta2(spec.K0sConfigSpec)
	}
	return res
}

func k0sConfigSpecV1beta1ToV1beta2(spec *K0sConfigSpec) *v1beta2.K0sConfigSpec {
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
func (kccv1beta1 *K0sControllerConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControllerConfig)
	kccv1beta1.ObjectMeta = src.ObjectMeta

	kccv1beta1.Spec = k0sControllerConfigV1beta2ToV1beta1Spec(src.Spec)
	return nil
}

func k0sControllerConfigV1beta2ToV1beta1Spec(spec v1beta2.K0sControllerConfigSpec) K0sControllerConfigSpec {
	res := K0sControllerConfigSpec{
		Version: spec.Version,
	}
	if spec.K0sConfigSpec != nil {
		res.K0sConfigSpec = k0sConfigSpecV1beta2ToV1beta1(spec.K0sConfigSpec)
	}
	return res
}

func k0sConfigSpecV1beta2ToV1beta1(spec *v1beta2.K0sConfigSpec) *K0sConfigSpec {
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
		CustomUserDataRef: spec.CustomUserDataRef,
		WorkingDir:        spec.WorkingDir,
	}
	if spec.Provisioner.Type == provisioner.IgnitionProvisioningFormat {
		res.Ignition = spec.Provisioner.Ignition
	}
	return res
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kccv1beta1 *K0sControllerConfigList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControllerConfigList)
	dst.ListMeta = kccv1beta1.ListMeta
	for _, item := range kccv1beta1.Items {
		converted := v1beta2.K0sControllerConfig{}
		if err := item.ConvertTo(&converted); err != nil {
			return err
		}
		dst.Items = append(dst.Items, converted)
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (kccv1beta1 *K0sControllerConfigList) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControllerConfigList)
	kccv1beta1.ListMeta = src.ListMeta
	for _, item := range src.Items {
		converted := K0sControllerConfig{}
		if err := converted.ConvertFrom(&item); err != nil {
			return err
		}
		kccv1beta1.Items = append(kccv1beta1.Items, converted)
	}
	return nil
}
