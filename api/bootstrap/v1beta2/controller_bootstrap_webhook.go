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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1beta2-k0scontrollerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs,verbs=create;update,versions=v1beta2,name=validate-k0scontrollerconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0sControllerConfigValidator implements a validation webhook for K0sControllerConfig.
type K0sControllerConfigValidator struct{}

var _ webhook.CustomValidator = &K0sControllerConfigValidator{}

// Validate validates the K0sControllerConfigSpec. The embedded config is a pointer, so
// a config carrying nothing but a version has no fields left to check.
func (cs *K0sControllerConfigSpec) Validate(pathPrefix *field.Path) field.ErrorList {
	if cs.K0sConfigSpec == nil {
		return nil
	}

	return ValidateFileOwners(cs.Files, cs.Provisioner, pathPrefix)
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sControllerConfigValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*K0sControllerConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sControllerConfig but got a %T", obj))
	}

	return nil, v.validate(c.Spec, c.Name)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sControllerConfigValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	newC, ok := newObj.(*K0sControllerConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sControllerConfig but got a %T", newObj))
	}

	return nil, v.validate(newC.Spec, newC.Name)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sControllerConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *K0sControllerConfigValidator) validate(c K0sControllerConfigSpec, name string) error {
	allErrs := c.Validate(field.NewPath("spec"))

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GroupVersion.WithKind("K0sControllerConfig").GroupKind(), name, allErrs)
}

// SetupK0sControllerConfigWebhookWithManager registers the webhook for K0sControllerConfig in the manager.
func SetupK0sControllerConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&K0sControllerConfig{}).
		WithValidator(&K0sControllerConfigValidator{}).
		Complete()
}
