package envtest

import (
	"strings"

	"github.com/gobuffalo/flect"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/contract"
)

var (
	// infrastructureGroupVersion is group version used for infrastructure objects.
	infrastructureGroupVersion = schema.GroupVersion{Group: "infrastructure.cluster.x-k8s.io", Version: "v1beta1"}

	// genericInfrastructureMachineCRD is a generic infrastructure machine CRD.
	genericInfrastructureMachineCRD = generateCRD(infrastructureGroupVersion.WithKind("GenericInfrastructureMachine"))

	// genericInfrastructureMachineTemplateCRD is a generic infrastructure machine template CRD.
	genericInfrastructureMachineTemplateCRD = generateCRD(infrastructureGroupVersion.WithKind("GenericInfrastructureMachineTemplate"))
)

func generateCRD(gvk schema.GroupVersionKind) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: contract.CalculateCRDName(gvk.Group, gvk.Kind),
			Labels: map[string]string{
				clusterv1.GroupVersion.String(): "v1beta1",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:   gvk.Kind,
				Plural: flect.Pluralize(strings.ToLower(gvk.Kind)),
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    gvk.Version,
					Served:  true,
					Storage: true,
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type:                   "object",
									XPreserveUnknownFields: ptr.To(true),
								},
								"status": {
									Type:                   "object",
									XPreserveUnknownFields: ptr.To(true),
								},
							},
						},
					},
				},
			},
		},
	}
}
