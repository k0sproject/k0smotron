package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kwcv1beta1 *K0sWorkerConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sWorkerConfig)
	dst.ObjectMeta = kwcv1beta1.ObjectMeta

	dst.Spec = k0sWorkerConfigV1beta1ToV1beta2Spec(kwcv1beta1.Spec)
	return nil
}

func k0sWorkerConfigV1beta1ToV1beta2Spec(spec K0sWorkerConfigSpec) v1beta2.K0sWorkerConfigSpec {
	res := v1beta2.K0sWorkerConfigSpec{
		Provisioner: v1beta2.ProvisionerSpec{
			// Default to CloudInit, will be overridden below if Ignition is set
			Type: provisioner.CloudInitProvisioningFormat,
		},
		K0sInstallDir:     spec.K0sInstallDir,
		Version:           spec.Version,
		UseSystemHostname: spec.UseSystemHostname,
		Files:             spec.Files,
		Args:              spec.Args,
		PreK0sCommands:    spec.PreStartCommands,
		PostK0sCommands:   spec.PostStartCommands,
		PreInstalledK0s:   spec.PreInstalledK0s,
		DownloadURL:       spec.DownloadURL,
		SecretMetadata:    spec.SecretMetadata,
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
func (kwcv1beta1 *K0sWorkerConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sWorkerConfig)
	kwcv1beta1.ObjectMeta = src.ObjectMeta

	kwcv1beta1.Spec = k0sWorkerConfigV1beta2ToV1beta1Spec(src.Spec)
	return nil
}

func k0sWorkerConfigV1beta2ToV1beta1Spec(spec v1beta2.K0sWorkerConfigSpec) K0sWorkerConfigSpec {
	res := K0sWorkerConfigSpec{
		K0sInstallDir:     spec.K0sInstallDir,
		Version:           spec.Version,
		UseSystemHostname: spec.UseSystemHostname,
		Files:             spec.Files,
		Args:              spec.Args,
		PreStartCommands:  spec.PreK0sCommands,
		PostStartCommands: spec.PostK0sCommands,
		PreInstalledK0s:   spec.PreInstalledK0s,
		DownloadURL:       spec.DownloadURL,
		SecretMetadata:    spec.SecretMetadata,
		WorkingDir:        spec.WorkingDir,
	}
	if spec.Provisioner.Type == provisioner.IgnitionProvisioningFormat {
		res.Ignition = spec.Provisioner.Ignition
	}

	return res
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kwcv1beta1 *K0sWorkerConfigList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sWorkerConfigList)
	dst.ListMeta = kwcv1beta1.ListMeta
	for _, item := range kwcv1beta1.Items {
		converted := v1beta2.K0sWorkerConfig{}
		if err := item.ConvertTo(&converted); err != nil {
			return err
		}
		dst.Items = append(dst.Items, converted)
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (kwcv1beta1 *K0sWorkerConfigList) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sWorkerConfigList)
	kwcv1beta1.ListMeta = src.ListMeta
	for _, item := range src.Items {
		converted := K0sWorkerConfig{}
		if err := converted.ConvertFrom(&item); err != nil {
			return err
		}
		kwcv1beta1.Items = append(kwcv1beta1.Items, converted)
	}
	return nil
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kwcv1beta1 *K0sWorkerConfigTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sWorkerConfigTemplate)
	dst.ObjectMeta = kwcv1beta1.ObjectMeta
	dst.Spec = v1beta2.K0sWorkerConfigTemplateSpec{
		Template: v1beta2.K0sWorkerConfigTemplateResource{
			ObjectMeta: kwcv1beta1.Spec.Template.ObjectMeta,
			Spec:       k0sWorkerConfigV1beta1ToV1beta2Spec(kwcv1beta1.Spec.Template.Spec),
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (kwcv1beta1 *K0sWorkerConfigTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sWorkerConfigTemplate)
	kwcv1beta1.ObjectMeta = src.ObjectMeta
	kwcv1beta1.Spec = K0sWorkerConfigTemplateSpec{
		Template: K0sWorkerConfigTemplateResource{
			ObjectMeta: src.Spec.Template.ObjectMeta,
			Spec:       k0sWorkerConfigV1beta2ToV1beta1Spec(src.Spec.Template.Spec),
		},
	}
	return nil
}
