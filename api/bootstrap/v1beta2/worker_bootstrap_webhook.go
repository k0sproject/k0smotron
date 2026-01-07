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
	"fmt"
	"github.com/k0sproject/k0smotron/internal/provisioner"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-bootstrap-cluster-x-k8s-io-v1beta2-k0sworkerconfig,mutating=true,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=create;update,versions=v1beta2,name=mutate-k0sworkerconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1beta2-k0sworkerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=create;update,versions=v1beta2,name=validate-k0sworkerconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0sWorkerConfigDefaulter implements a defaulting webhook for K0sWorkerConfig.
type K0sWorkerConfigDefaulter struct{}

// K0sWorkerConfigValidator implements a validation webhook for K0sWorkerConfig.
type K0sWorkerConfigValidator struct{}

var _ webhook.CustomDefaulter = &K0sWorkerConfigDefaulter{}
var _ webhook.CustomValidator = &K0sWorkerConfigValidator{}

// Default implements webhook.Defaulter so a webhook will be registered for the K0sWorkerConfig.
func (d *K0sWorkerConfigDefaulter) Default(_ context.Context, obj runtime.Object) error {
	c, ok := obj.(*bootstrapv1.K0sWorkerConfig)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a K0sWorkerConfig but got a %T", obj))
	}

	if c.Spec.Ignition != nil {
		c.Spec.Provisioner = v1beta1.ProvisionerSpec{
			Type:     provisioner.IgnitionProvisioningFormat,
			Ignition: c.Spec.Ignition,
		}
		c.Spec.Ignition = nil
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*K0sWorkerConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sWorkerConfig but got a %T", obj))
	}

	return nil, v.validate(c.Spec, c.Name)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	newC, ok := newObj.(*K0sWorkerConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sWorkerConfig but got a %T", newObj))
	}

	return nil, v.validate(newC.Spec, newC.Name)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sWorkerConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *K0sWorkerConfigValidator) validate(c K0sWorkerConfigSpec, name string) error {
	allErrs := c.Validate(field.NewPath("spec"))

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GroupVersion.WithKind("K0sWorkerConfig").GroupKind(), name, allErrs)
}

// SetupK0sWorkerConfigWebhookWithManager registers the webhook for K0sWorkerConfig in the manager.
func (v *K0sWorkerConfigValidator) SetupK0sWorkerConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&K0sWorkerConfig{}).
		WithValidator(v).
		Complete()
}
