/*
Copyright 2023.

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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-bootstrap-cluster-x-k8s-io-v1beta2-k0sworkerconfig,mutating=true,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=create;update,versions=v1beta2,name=mutate-k0sworkerconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1beta2-k0sworkerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=create;update,versions=v1beta2,name=validate-k0sworkerconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0sWorkerConfigDefaulter implements a defaulting webhook for K0sWorkerConfig.
type K0sWorkerConfigDefaulter struct{}

// K0sWorkerConfigValidator implements a validation webhook for K0sWorkerConfig.
type K0sWorkerConfigValidator struct{}

var _ admission.Defaulter[*K0sWorkerConfig] = &K0sWorkerConfigDefaulter{}
var _ admission.Validator[*K0sWorkerConfig] = &K0sWorkerConfigValidator{}

// Default implements webhook.Defaulter so a webhook will be registered for the K0sWorkerConfig.
func (d *K0sWorkerConfigDefaulter) Default(_ context.Context, c *K0sWorkerConfig) error {
	if c == nil {
		return apierrors.NewBadRequest("expected a K0sWorkerConfig but got nil")
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateCreate(_ context.Context, c *K0sWorkerConfig) (admission.Warnings, error) {
	if c == nil {
		return nil, apierrors.NewBadRequest("expected a K0sWorkerConfig but got nil")
	}

	prefix := field.NewPath("spec")

	return ProvisionerWarnings(c.Spec.Provisioner, prefix),
		v.validate(c.Name, ValidateProvisioner(c.Spec.Provisioner, prefix), c.Spec.Validate(prefix))
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateUpdate(_ context.Context, oldConfig, newConfig *K0sWorkerConfig) (admission.Warnings, error) {
	if newConfig == nil {
		return nil, apierrors.NewBadRequest("expected a K0sWorkerConfig but got nil")
	}

	prefix := field.NewPath("spec")

	// Ratcheted, or an object admitted before a rule existed can never be updated
	// again, which includes the controller stripping its own finalizer.
	provisionerErrs := ValidateProvisioner(newConfig.Spec.Provisioner, prefix)
	if oldConfig != nil {
		provisionerErrs = RatchetErrors(ValidateProvisioner(oldConfig.Spec.Provisioner, prefix), provisionerErrs)
	}

	return ProvisionerWarnings(newConfig.Spec.Provisioner, prefix),
		v.validate(newConfig.Name, provisionerErrs, newConfig.Spec.Validate(prefix))
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateDelete(_ context.Context, _ *K0sWorkerConfig) (admission.Warnings, error) {
	return nil, nil
}

func (v *K0sWorkerConfigValidator) validate(name string, errLists ...field.ErrorList) error {
	var allErrs field.ErrorList
	for _, errs := range errLists {
		allErrs = append(allErrs, errs...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GroupVersion.WithKind("K0sWorkerConfig").GroupKind(), name, allErrs)
}

// SetupK0sWorkerConfigWebhookWithManager registers the webhook for K0sWorkerConfig in the manager.
func SetupK0sWorkerConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &K0sWorkerConfig{}).
		WithValidator(&K0sWorkerConfigValidator{}).
		WithDefaulter(&K0sWorkerConfigDefaulter{}).
		Complete()
}
