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

package v1beta1

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/contract"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &K0sControlPlane{}
var _ conversion.Convertible = &K0sControlPlaneTemplate{}

var apiVersionGetter = func(_ schema.GroupKind) (string, error) {
	return "", errors.New("apiVersionGetter not set")
}

// SetAPIVersionGetter sets the function used to retrieve apiVersion for a GroupKind for conversion webhooks.
func SetAPIVersionGetter(f func(gk schema.GroupKind) (string, error)) {
	apiVersionGetter = f
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kcpv1beta1 *K0sControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlane)
	dst.ObjectMeta = *kcpv1beta1.ObjectMeta.DeepCopy()

	spec, err := k0sControlPlaneSpecV1beta1ToV1beta2(kcpv1beta1.Spec)
	if err != nil {
		return err
	}
	dst.Spec = *spec

	dst.Status = v1beta2.K0sControlPlaneStatus{
		Initialization:              v1beta2.Initialization{ControlPlaneInitialized: &kcpv1beta1.Status.Initialized},
		ExternalManagedControlPlane: new(kcpv1beta1.Status.ExternalManagedControlPlane),
		Replicas:                    new(kcpv1beta1.Status.Replicas),
		Version:                     kcpv1beta1.Status.Version,
		Selector:                    kcpv1beta1.Status.Selector,
		ReadyReplicas:               new(kcpv1beta1.Status.ReadyReplicas),
		UpToDateReplicas:            new(kcpv1beta1.Status.UpdatedReplicas),
		AvailableReplicas:           new(kcpv1beta1.Status.Replicas - kcpv1beta1.Status.UnavailableReplicas),
		Conditions:                  kcpv1beta1.Status.Conditions,
	}
	return nil
}

func k0sControlPlaneSpecV1beta1ToV1beta2(spec K0sControlPlaneSpec) (*v1beta2.K0sControlPlaneSpec, error) {
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&spec.K0sConfigSpec)
	kcpSpec := &v1beta2.K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec.DeepCopy(),
		Replicas:                 spec.Replicas,
		UpdateStrategy:           spec.UpdateStrategy,
		Version:                  spec.Version,
		KubeconfigSecretMetadata: spec.KubeconfigSecretMetadata,
	}

	infraRef, err := convertToContractVersionedObjectReference(&spec.MachineTemplate.InfrastructureRef)
	if err != nil {
		return nil, err
	}
	kcpSpec.MachineTemplate.Spec.InfrastructureRef = *infraRef

	return kcpSpec, nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (kcpv1beta1 *K0sControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControlPlane)
	kcpv1beta1.ObjectMeta = *src.ObjectMeta.DeepCopy()

	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec.DeepCopy(),
		Replicas:                 src.Spec.Replicas,
		UpdateStrategy:           src.Spec.UpdateStrategy,
		Version:                  src.Spec.Version,
		KubeconfigSecretMetadata: src.Spec.KubeconfigSecretMetadata,
	}
	if src.Spec.MachineTemplate.Spec.InfrastructureRef.IsDefined() {
		infraRef, err := convertToObjectReference(&src.Spec.MachineTemplate.Spec.InfrastructureRef, src.Namespace)
		if err != nil {
			return err
		}
		if kcpv1beta1.Spec.MachineTemplate == nil {
			kcpv1beta1.Spec.MachineTemplate = &K0sControlPlaneMachineTemplate{}
		}
		kcpv1beta1.Spec.MachineTemplate.InfrastructureRef = *infraRef
	}

	kcpv1beta1.Status = K0sControlPlaneStatus{
		Ready:       ptr.Deref(src.Status.ReadyReplicas, 0) > 0,
		Initialized: ptr.Deref(src.Status.Initialization.ControlPlaneInitialized, false),
		Initialization: Initialization{
			ControlPlaneInitialized: ptr.Deref(src.Status.Initialization.ControlPlaneInitialized, false),
		},
		ExternalManagedControlPlane: ptr.Deref(src.Status.ExternalManagedControlPlane, false),
		Replicas:                    ptr.Deref(src.Status.Replicas, 0),
		Version:                     src.Status.Version,
		Selector:                    src.Status.Selector,
		UnavailableReplicas:         ptr.Deref(src.Status.Replicas, 0),
		ReadyReplicas:               ptr.Deref(src.Status.ReadyReplicas, 0),
		Conditions:                  src.Status.Conditions,
	}
	if src.Status.UpToDateReplicas != nil {
		kcpv1beta1.Status.UpdatedReplicas = *src.Status.UpToDateReplicas
	}

	if src.Status.AvailableReplicas != nil {
		kcpv1beta1.Status.UnavailableReplicas = ptr.Deref(src.Status.Replicas, 0) - *src.Status.AvailableReplicas
	}

	return nil
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0sControlPlaneTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlaneTemplate)
	dst.ObjectMeta = *kcpv1beta1.ObjectMeta.DeepCopy()

	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&kcpv1beta1.Spec.Template.Spec.K0sConfigSpec)
	dst.Spec = v1beta2.K0sControlPlaneTemplateSpec{
		Template: v1beta2.K0sControlPlaneTemplateResource{
			ObjectMeta: kcpv1beta1.Spec.Template.ObjectMeta,
			Spec: v1beta2.K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec.DeepCopy(),
				MachineTemplate: kcpv1beta1.Spec.Template.Spec.MachineTemplate.DeepCopy(),
				Version:         kcpv1beta1.Spec.Template.Spec.Version,
				UpdateStrategy:  kcpv1beta1.Spec.Template.Spec.UpdateStrategy,
			},
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2 - self) to this version.
func (kcpv1beta1 *K0sControlPlaneTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControlPlaneTemplate)
	kcpv1beta1.ObjectMeta = *src.ObjectMeta.DeepCopy()
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.Template.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneTemplateSpec{
		Template: K0sControlPlaneTemplateResource{
			ObjectMeta: src.Spec.Template.ObjectMeta,
			Spec: K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec.DeepCopy(),
				MachineTemplate: src.Spec.Template.Spec.MachineTemplate.DeepCopy(),
				Version:         src.Spec.Template.Spec.Version,
				UpdateStrategy:  src.Spec.Template.Spec.UpdateStrategy,
			},
		},
	}
	return nil
}

func convertToContractVersionedObjectReference(ref *corev1.ObjectReference) (*clusterv1.ContractVersionedObjectReference, error) {
	var apiGroup string
	if ref.APIVersion != "" {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to convert object: failed to parse apiVersion: %v", err)
		}
		apiGroup = gv.Group
	}
	return &clusterv1.ContractVersionedObjectReference{
		APIGroup: apiGroup,
		Kind:     ref.Kind,
		Name:     ref.Name,
	}, nil
}

func convertToObjectReference(ref *clusterv1.ContractVersionedObjectReference, namespace string) (*corev1.ObjectReference, error) {
	apiVersion, err := apiVersionGetter(schema.GroupKind{
		Group: ref.APIGroup,
		Kind:  ref.Kind,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to convert object reference: %w", err)
	}
	return &corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       ref.Kind,
		Namespace:  namespace,
		Name:       ref.Name,
	}, nil
}

// ResolveAPIVersion uses the provided client to look up the CRD for the given GroupKind and determine the appropriate apiVersion to use for conversion.
func ResolveAPIVersion(ctx context.Context, c client.Reader, gk schema.GroupKind) (string, error) {
	// Fetch CRD metadata (same approach as CAPI internal/contract)
	crdMeta := &metav1.PartialObjectMetadata{}
	crdMeta.SetName(contract.CalculateCRDName(gk.Group, gk.Kind))
	crdMeta.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	if err := c.Get(ctx, client.ObjectKeyFromObject(crdMeta), crdMeta); err != nil {
		return "", fmt.Errorf("failed to get CRD for %s: %w", gk, err)
	}

	// Look for contract version labels: cluster.x-k8s.io/v1beta2 (and v1beta1 for compat)
	contractVersions := []string{"v1beta2", "v1beta1"}
	labels := crdMeta.GetLabels()

	for _, contractVersion := range contractVersions {
		labelKey := fmt.Sprintf("%s/%s", clusterv1.GroupVersion.Group, contractVersion)
		supportedVersions, ok := labels[labelKey]
		if !ok || supportedVersions == "" {
			continue
		}
		// Pick latest version
		versions := strings.Split(supportedVersions, "_")
		sort.Slice(versions, func(i, j int) bool {
			return k8sversion.CompareKubeAwareVersionStrings(versions[i], versions[j]) < 0
		})
		latest := versions[len(versions)-1]
		return schema.GroupVersion{Group: gk.Group, Version: latest}.String(), nil
	}

	return "", fmt.Errorf("no contract version label found on CRD %s", crdMeta.GetName())
}
