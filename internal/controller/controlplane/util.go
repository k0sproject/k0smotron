package controlplane

import (
	"context"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (c *Controller) GenerateBootstapSecret() {

}

func (c *Controller) GenerateMachine(ctx context.Context, name string, kcp *cpv1beta1.K0smotronControlPlane) (*unstructured.Unstructured, error) {
	unstructuredMachineTemplate, err := c.getMachineTemplate(ctx, kcp)
	if err != nil {
		return nil, err
	}

	template, found, err := unstructured.NestedMap(unstructuredMachineTemplate.UnstructuredContent(), "spec", "template")
	if !found {
		return nil, errors.Errorf("missing spec.template on %v %q", unstructuredMachineTemplate.GroupVersionKind(), unstructuredMachineTemplate.GetName())
	} else if err != nil {
		return nil, errors.Wrapf(err, "error getting spec.template map on %v %q", unstructuredMachineTemplate.GroupVersionKind(), unstructuredMachineTemplate.GetName())
	}

	machine := &unstructured.Unstructured{Object: template}
	//machine.SetName(names.SimpleNameGenerator.GenerateName(kcp.Spec.MachineTemplate.InfrastructureRef.Name + "-"))
	machine.SetName(name)
	machine.SetNamespace(kcp.Namespace)

	annotations := map[string]string{}
	for key, value := range kcp.Annotations {
		annotations[key] = value
	}
	annotations[clusterv1.TemplateClonedFromNameAnnotation] = kcp.Spec.MachineTemplate.InfrastructureRef.Name
	annotations[clusterv1.TemplateClonedFromGroupKindAnnotation] = kcp.Spec.MachineTemplate.InfrastructureRef.GroupVersionKind().GroupKind().String()
	machine.SetAnnotations(annotations)

	// Set labels.
	labels := map[string]string{}
	for key, value := range kcp.Labels {
		labels[key] = value
	}
	labels[clusterv1.ClusterNameLabel] = kcp.Name
	machine.SetLabels(labels)

	machine.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       kcp.Kind,
		Name:       kcp.Name,
		UID:        kcp.UID,
	}})

	machine.SetAPIVersion(unstructuredMachineTemplate.GetAPIVersion())
	machine.SetKind(strings.TrimSuffix(unstructuredMachineTemplate.GetKind(), clusterv1.TemplateSuffix))

	return machine, nil
}

func (c *Controller) getMachineTemplate(ctx context.Context, kcp *cpv1beta1.K0smotronControlPlane) (*unstructured.Unstructured, error) {
	infRef := kcp.Spec.MachineTemplate.InfrastructureRef

	var machineTemplate unstructured.Unstructured
	key := client.ObjectKey{Name: infRef.Name, Namespace: infRef.Namespace}

	err := c.Get(ctx, key, &machineTemplate)
	if err != nil {
		return nil, err
	}
	return &machineTemplate, nil
}
