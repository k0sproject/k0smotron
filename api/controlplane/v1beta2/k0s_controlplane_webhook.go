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
	"strings"

	"github.com/k0sproject/version"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/k0sproject/k0smotron/internal/provisioner"
)

// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta2-k0scontrolplane,mutating=false,failurePolicy=fail,sideEffects=None,groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes,verbs=create;update,versions=v1beta2,name=validate-k0scontrolplane-v1beta2.k0smotron.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/mutate-controlplane-cluster-x-k8s-io-v1beta2-k0scontrolplane,mutating=true,failurePolicy=fail,sideEffects=None,groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes,verbs=create;update,versions=v1beta2,name=mutate-k0scontrolplane-v1beta2.k0smotron.io,admissionReviewVersions=v1

// K0sControlPlaneValidator struct is responsible for validating the K0sControlPlane resource when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type K0sControlPlaneValidator struct{}

// K0sControlPlaneDefaulter struct is responsible for setting default values for the K0sControlPlane resource when it is created or updated.
type K0sControlPlaneDefaulter struct{}

var _ webhook.CustomValidator = &K0sControlPlaneValidator{}
var _ webhook.CustomDefaulter = &K0sControlPlaneDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type K0sControlPlane.
func (d *K0sControlPlaneDefaulter) Default(_ context.Context, obj runtime.Object) error {
	_, ok := obj.(*K0sControlPlane)
	if !ok {
		return fmt.Errorf("expected a K0sControlPlane object but got %T", obj)
	}

	return nil
}

// validateVersionSuffix checks if the version has a k0s suffix and returns a warning if it doesn't
func (v *K0sControlPlaneValidator) validateVersionSuffix(version string) admission.Warnings {
	warnings := admission.Warnings{}
	if version != "" && !strings.Contains(version, "+k0s.") {
		warnings = append(warnings, fmt.Sprintf("The specified version '%s' requires a k0s suffix (+k0s.<number>). Using '%s+k0s.0' instead.", version, version))
	}
	return warnings
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type K0sControlPlane.
func (v *K0sControlPlaneValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	kcp, ok := obj.(*K0sControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a K0sControlPlane object but got %T", obj)
	}

	warnings := v.validateVersionSuffix(kcp.Spec.Version)
	return warnings, validateK0sControlPlane(kcp)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type K0sControlPlane.
func (v *K0sControlPlaneValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newKCP, ok := newObj.(*K0sControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a new K0sControlPlane object but got %T", newObj)
	}
	oldKCP, ok := oldObj.(*K0sControlPlane)
	if !ok {
		return nil, fmt.Errorf("expected a old K0sControlPlane object but got %T", oldObj)
	}

	warnings := v.validateVersionSuffix(newKCP.Spec.Version)
	if oldKCP.Spec.Version != newKCP.Spec.Version {
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

	return warnings, validateK0sControlPlane(newKCP)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type K0sControlPlane.
func (v *K0sControlPlaneValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateK0sControlPlane(kcp *K0sControlPlane) error {
	if err := denyIncompatibleK0sVersions(kcp); err != nil {
		return err
	}

	if err := denyIncompatibleProvisioners(kcp); err != nil {
		return err
	}

	// nolint:revive
	if err := denyRecreateOnSingleClusters(kcp); err != nil {
		return err
	}

	return nil
}

func denyIncompatibleProvisioners(kcp *K0sControlPlane) error {
	if kcp.Spec.K0sConfigSpec.Provisioner.Type == provisioner.PowershellXMLProvisioningFormat ||
		kcp.Spec.K0sConfigSpec.Provisioner.Type == provisioner.PowershellProvisioningFormat {
		return fmt.Errorf("K0sControlPlane does not support powershell and powershell-xml provisioning formats")
	}

	return nil
}

func denyIncompatibleK0sVersions(kcp *K0sControlPlane) error {
	var incompatibleVersions = map[string]string{
		"1.31.1": "v1.31.2+",
	}
	v, err := version.NewVersion(kcp.Spec.Version)
	if err != nil {
		return fmt.Errorf("failed to parse version: %v", err)
	}

	if vv, ok := incompatibleVersions[v.Core().String()]; ok {
		return fmt.Errorf("version %s is not compatible with K0sControlPlane, use %s", kcp.Spec.Version, vv)
	}

	return nil
}

func denyRecreateOnSingleClusters(kcp *K0sControlPlane) error {
	if kcp.Spec.UpdateStrategy == UpdateRecreate {

		// If the cluster is running in single mode, we can't use the Recreate strategy
		if kcp.Spec.K0sConfigSpec.Args != nil {
			for _, arg := range kcp.Spec.K0sConfigSpec.Args {
				if arg == "--single" {
					return fmt.Errorf("UpdateStrategy Recreate strategy is not allowed when the cluster is running in single mode")
				}
			}
		}
	}

	return nil
}

// SetupK0sControlPlaneWebhookWithManager registers the webhook for K0sControlPlane in the manager.
func SetupK0sControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&K0sControlPlane{}).
		WithValidator(&K0sControlPlaneValidator{}).
		WithDefaulter(&K0sControlPlaneDefaulter{}).
		Complete()
}
