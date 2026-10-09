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

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
	"github.com/k0sproject/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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

var _ admission.Defaulter[*K0sControlPlane] = &K0sControlPlaneDefaulter{}
var _ admission.Validator[*K0sControlPlane] = &K0sControlPlaneValidator{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type K0sControlPlane.
func (d *K0sControlPlaneDefaulter) Default(_ context.Context, kcp *K0sControlPlane) error {
	if kcp == nil {
		return fmt.Errorf("expected a K0sControlPlane object but got nil")
	}

	migrateDeprecatedInfrastructureRef(kcp)

	return nil
}

// migrateDeprecatedInfrastructureRef fills the nested field from the deprecated one only where the
// nested is unset, so a value written through the new API is never overwritten by a leftover.
func migrateDeprecatedInfrastructureRef(kcp *K0sControlPlane) {
	mt := kcp.Spec.MachineTemplate
	if mt == nil || mt.InfrastructureRef == (corev1.ObjectReference{}) {
		return
	}

	if mt.Spec.InfrastructureRef != (clusterv1.ContractVersionedObjectReference{}) {
		return
	}

	// Copied rather than moved, since deleting a field the user wrote makes their next apply put
	// it back and the resource diff forever.
	mt.Spec.InfrastructureRef = ContractRefFromObjectReference(mt.InfrastructureRef)
}

// validateInfrastructureRef reads the deprecated field itself rather than an empty nested one,
// because defaulting runs first and would otherwise have swallowed the warning.
func (v *K0sControlPlaneValidator) validateInfrastructureRef(kcp *K0sControlPlane) admission.Warnings {
	if kcp.Spec.MachineTemplate == nil || kcp.Spec.MachineTemplate.InfrastructureRef == (corev1.ObjectReference{}) {
		return nil
	}

	return admission.Warnings{
		"spec.machineTemplate.infrastructureRef is deprecated, use spec.machineTemplate.spec.infrastructureRef instead.",
	}
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
func (v *K0sControlPlaneValidator) ValidateCreate(_ context.Context, kcp *K0sControlPlane) (admission.Warnings, error) {
	if kcp == nil {
		return nil, fmt.Errorf("expected a K0sControlPlane object but got nil")
	}

	warnings := v.validateVersionSuffix(kcp.Spec.Version)
	warnings = append(warnings, v.validateInfrastructureRef(kcp)...)

	return warnings, validateK0sControlPlane(kcp)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type K0sControlPlane.
func (v *K0sControlPlaneValidator) ValidateUpdate(_ context.Context, oldKcp, newKcp *K0sControlPlane) (admission.Warnings, error) {
	warnings := v.validateVersionSuffix(newKcp.Spec.Version)
	warnings = append(warnings, v.validateInfrastructureRef(newKcp)...)

	// The field is optional, and a skew only exists between two versions that are set.
	// Requiring both also lets the controller persist the version it defaults.
	if oldKcp.Spec.Version != "" && newKcp.Spec.Version != "" && oldKcp.Spec.Version != newKcp.Spec.Version {
		oldV, err := version.NewVersion(oldKcp.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse old version: %v", err)
		}
		newV, err := version.NewVersion(newKcp.Spec.Version)
		if err != nil {
			return warnings, fmt.Errorf("failed to parse new version: %v", err)
		}

		// According to the Kubernetes skew policy, we can't upgrade more than one minor version at a time.
		// Comparing minors alone reads a major bump as a large downgrade, so the major has to be
		// part of the comparison the way upstream's ceiling is.
		oldCore, newCore := oldV.Core().Segments(), newV.Core().Segments()
		if newCore[0] > oldCore[0] || (newCore[0] == oldCore[0] && newCore[1]-oldCore[1] > 1) {
			return warnings, fmt.Errorf("upgrading more than one minor version at a time is not allowed by the Kubernetes skew policy")
		}
	}

	return warnings, validateK0sControlPlane(newKcp)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type K0sControlPlane.
func (v *K0sControlPlaneValidator) ValidateDelete(_ context.Context, _ *K0sControlPlane) (admission.Warnings, error) {
	return nil, nil
}

func validateK0sControlPlane(kcp *K0sControlPlane) error {
	prefix := field.NewPath("spec")

	// Collected rather than returned one at a time, so a spec with several problems
	// reports all of them instead of revealing the next one on every apply.
	var allErrs field.ErrorList

	for _, err := range []*field.Error{
		denyMissingInfrastructureRef(kcp, prefix),
		denyIncompatibleK0sVersions(kcp, prefix),
		denyIncompatibleProvisioners(kcp, prefix),
		denyRecreateOnSingleClusters(kcp, prefix),
	} {
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	// K0sControllerConfig has no webhook of its own, so validate the files here
	// where they are still part of the control plane spec.
	allErrs = append(allErrs, bootstrapv1.ValidateFiles(
		kcp.Spec.K0sConfigSpec.Files,
		kcp.Spec.K0sConfigSpec.Provisioner,
		prefix.Child("k0sConfigSpec"),
	)...)

	return allErrs.ToAggregate()
}

// denyMissingInfrastructureRef takes over what the schema used to enforce, since neither field is
// required on its own any more. A disagreeing pair is not refused, defaulting has resolved it.
func denyMissingInfrastructureRef(kcp *K0sControlPlane, prefix *field.Path) *field.Error {
	mt := kcp.Spec.MachineTemplate
	if mt == nil {
		return nil
	}

	if mt.InfrastructureRef == (corev1.ObjectReference{}) && mt.Spec.InfrastructureRef == (clusterv1.ContractVersionedObjectReference{}) {
		return field.Required(prefix.Child("machineTemplate", "spec", "infrastructureRef"),
			"an infrastructure template reference is required")
	}

	return nil
}

func denyIncompatibleProvisioners(kcp *K0sControlPlane, prefix *field.Path) *field.Error {
	if kcp.Spec.K0sConfigSpec.Provisioner.Platform == bootstrapv1.PlatformWindows ||
		kcp.Spec.K0sConfigSpec.Provisioner.Type == provisioner.PowershellXMLProvisioningFormat ||
		kcp.Spec.K0sConfigSpec.Provisioner.Type == provisioner.PowershellProvisioningFormat {
		return field.Invalid(
			prefix.Child("k0sConfigSpec", "provisioner"),
			kcp.Spec.K0sConfigSpec.Provisioner.Type,
			"K0sControlPlane does not support powershell and powershell-xml provisioning formats",
		)
	}

	return nil
}

func denyIncompatibleK0sVersions(kcp *K0sControlPlane, prefix *field.Path) *field.Error {
	var incompatibleVersions = map[string]string{
		"1.31.1": "v1.31.2+",
	}

	// The field is optional and the controller picks a version when it is empty, so
	// there is nothing to hold against the incompatible list yet.
	if kcp.Spec.Version == "" {
		return nil
	}

	versionPath := prefix.Child("version")

	v, err := version.NewVersion(kcp.Spec.Version)
	if err != nil {
		return field.Invalid(versionPath, kcp.Spec.Version, fmt.Sprintf("failed to parse version: %v", err))
	}

	if vv, ok := incompatibleVersions[v.Core().String()]; ok {
		return field.Invalid(versionPath, kcp.Spec.Version,
			fmt.Sprintf("version %s is not compatible with K0sControlPlane, use %s", kcp.Spec.Version, vv))
	}

	return nil
}

func denyRecreateOnSingleClusters(kcp *K0sControlPlane, prefix *field.Path) *field.Error {
	// Read through the helper rather than matching the bare flag, which missed --single=true and
	// every other spelling pflag accepts.
	if kcp.Spec.UpdateStrategy == UpdateRecreate && kcp.Spec.K0sConfigSpec.SingleNodeEnabled() {
		return field.Invalid(
			prefix.Child("updateStrategy"),
			kcp.Spec.UpdateStrategy,
			"UpdateStrategy Recreate strategy is not allowed when the cluster is running in single mode",
		)
	}

	return nil
}

// SetupK0sControlPlaneWebhookWithManager registers the webhook for K0sControlPlane in the manager.
func SetupK0sControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &K0sControlPlane{}).
		WithValidator(&K0sControlPlaneValidator{}).
		WithDefaulter(&K0sControlPlaneDefaulter{}).
		Complete()
}
