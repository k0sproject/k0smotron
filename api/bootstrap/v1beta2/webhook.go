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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1beta2-k0sconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=k0sconfigs,verbs=create;update,versions=v1beta2,name=validate-k0sconfig-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0sConfigValidator implements a validation webhook for K0sConfig.
type K0sConfigValidator struct{}

var _ webhook.CustomValidator = &K0sConfigValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (v *K0sConfigValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*K0sConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sConfig but got a %T", obj))
	}

	return nil, v.validate(c.Spec, c.Name)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sConfigValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	newC, ok := newObj.(*K0sConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a K0sConfig but got a %T", newObj))
	}

	return nil, v.validate(newC.Spec, newC.Name)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (v *K0sConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *K0sConfigValidator) validate(c K0sConfigSpec, name string) error {
	allErrs := c.Validate(field.NewPath("spec"))

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GroupVersion.WithKind("K0sConfig").GroupKind(), name, allErrs)
}

// SetupK0sConfigWebhookWithManager registers the webhook for K0sConfig in the manager.
func (v *K0sConfigValidator) SetupK0sConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&K0sConfig{}).
		WithValidator(v).
		Complete()
}
