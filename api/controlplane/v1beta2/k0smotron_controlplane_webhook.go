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

	k0smotroniov1beta2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/version"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta2-k0smotroncontrolplane,mutating=false,failurePolicy=fail,sideEffects=None,groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes,verbs=create;update,versions=v1beta2,name=validate-k0smotroncontrolplane-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0smotronControlPlaneValidator struct is responsible for validating the K0smotronControlPlane resource when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type K0smotronControlPlaneValidator struct {
	cv k0smotroniov1beta2.ClusterValidator
}

var _ admission.Validator[*K0smotronControlPlane] = &K0smotronControlPlaneValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateCreate(_ context.Context, kcp *K0smotronControlPlane) (admission.Warnings, error) {
	warnings, err := v.validate(kcp)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateUpdate(_ context.Context, oldKcp, newKcp *K0smotronControlPlane) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	if oldKcp.Spec.Version != newKcp.Spec.Version {
		// Skip validation if either version is empty
		if oldKcp.Spec.Version == "" || newKcp.Spec.Version == "" {
			return warnings, nil
		}

		oldV, err := version.NewVersion(oldKcp.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse old version: %v", err)
		}
		newV, err := version.NewVersion(newKcp.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse new version: %v", err)
		}

		// According to the Kubernetes skew policy, we can't upgrade more than one minor version at a time.
		if newV.Core().Segments()[1]-oldV.Core().Segments()[1] > 1 {
			return warnings, fmt.Errorf("upgrading more than one minor version at a time is not allowed by the Kubernetes skew policy")
		}
	}

	specWarnings, err := v.cv.ValidateClusterSpecUpdate(&oldKcp.Spec, &newKcp.Spec)
	if err != nil {
		return warnings, err
	}
	warnings = append(warnings, specWarnings...)

	return warnings, nil
}

func (v *K0smotronControlPlaneValidator) validate(kcp *K0smotronControlPlane) (admission.Warnings, error) {
	return v.cv.ValidateClusterSpec(&kcp.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateDelete(_ context.Context, _ *K0smotronControlPlane) (admission.Warnings, error) {
	return nil, nil
}

// SetupK0smotronControlPlaneWebhookWithManager registers the webhook for K0smotronControlPlane in the manager.
func (v *K0smotronControlPlaneValidator) SetupK0smotronControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &K0smotronControlPlane{}).
		WithValidator(v).
		Complete()
}
