package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &K0sWorkerConfig{}
var _ conversion.Convertible = &K0sWorkerConfigTemplate{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (k *K0sWorkerConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sWorkerConfig)
	dst.ObjectMeta = k.ObjectMeta
	dst.Spec = k0sWorkerConfigV1beta1ToV1beta2Spec(k.Spec)
	dst.Status = v1beta2.K0sWorkerConfigStatus{
		Ready:          k.Status.Ready,
		DataSecretName: k.Status.DataSecretName,
		Conditions:     k.Status.Conditions,
	}
	if k.Status.DataSecretName != nil && *k.Status.DataSecretName != "" {
		dst.Status.Initialization.DataSecretCreated = true
	}
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
func (k *K0sWorkerConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sWorkerConfig)
	k.ObjectMeta = src.ObjectMeta

	k.Spec = k0sWorkerConfigV1beta2ToV1beta1Spec(src.Spec)
	k.Status = K0sWorkerConfigStatus{
		Ready:          src.Status.Ready,
		DataSecretName: src.Status.DataSecretName,
		Conditions:     src.Status.Conditions,
	}
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
