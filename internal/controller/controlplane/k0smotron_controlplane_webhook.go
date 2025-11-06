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

package controlplane

import (
	"context"
	"fmt"
	k0smotronio "github.com/k0sproject/k0smotron/internal/controller/k0smotron.io"
	"github.com/k0sproject/version"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta1-k0smotroncontrolplane,mutating=false,failurePolicy=fail,sideEffects=None,groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes,verbs=create;update,versions=v1beta1,name=validate-k0smotroncontrolplane-v1beta1.k0smotron.io,admissionReviewVersions=v1

// K0smotronControlPlaneValidator struct is responsible for validating the K0smotronControlPlane resource when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type K0smotronControlPlaneValidator struct {
	cv k0smotronio.ClusterValidator
}

var _ webhook.CustomValidator = &K0smotronControlPlaneValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	kcp, ok := obj.(*v1beta1.K0smotronControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a K0smotronControlPlane object but got %T", obj)
	}

	warnings, err := v.validate(kcp)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newKCP, ok := newObj.(*v1beta1.K0smotronControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a new K0smotronControlPlane object but got %T", newObj)
	}
	oldKCP, ok := oldObj.(*v1beta1.K0smotronControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a old K0smotronControlPlane object but got %T", oldObj)
	}

	warnings := admission.Warnings{}

	if oldKCP.Spec.Version != newKCP.Spec.Version {
		// Skip validation if either version is empty
		if oldKCP.Spec.Version == "" || newKCP.Spec.Version == "" {
			return warnings, nil
		}

		oldV, err := version.NewVersion(oldKCP.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse old version: %v", err)
		}
		newV, err := version.NewVersion(newKCP.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse new version: %v", err)
		}

		// According to the Kubernetes skew policy, we can't upgrade more than one minor version at a time.
		if newV.Core().Segments()[1]-oldV.Core().Segments()[1] > 1 {
			return warnings, fmt.Errorf("upgrading more than one minor version at a time is not allowed by the Kubernetes skew policy")
		}
	}

	_, err := v.validate(newKCP)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

func (v *K0smotronControlPlaneValidator) validate(kcp *v1beta1.K0smotronControlPlane) (admission.Warnings, error) {
	return v.cv.ValidateClusterSpec(&kcp.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type K0smotronControlPlane.
func (v *K0smotronControlPlaneValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// SetupK0smotronControlPlaneWebhookWithManager registers the webhook for K0smotronControlPlane in the manager.
func (v *K0smotronControlPlaneValidator) SetupK0smotronControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1beta1.K0smotronControlPlane{}).
		WithValidator(v).
		Complete()
}
