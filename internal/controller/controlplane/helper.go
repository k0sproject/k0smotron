package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

func (c *K0sController) createMachine(ctx context.Context, name string, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, infraRef corev1.ObjectReference) error {
	machine := c.generateMachine(ctx, name, kcp, infraRef)

	_ = ctrl.SetControllerReference(cluster, machine, c.Scheme)

	return c.Client.Patch(ctx, machine, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	})
}

//func (c *K0sController) deleteMachine(ctx context.Context, name string, kcp *cpv1beta1.K0sControlPlane) error {
//	machine := &clusterv1.Machine{
//
//		TypeMeta: metav1.TypeMeta{
//			APIVersion: clusterv1.GroupVersion.String(),
//			Kind:       "Machine",
//		},
//		ObjectMeta: metav1.ObjectMeta{
//			Name:      name,
//			Namespace: kcp.Namespace,
//		},
//	}
//
//	return c.Client.Delete(ctx, machine)
//}

func (c *K0sController) generateMachine(_ context.Context, name string, kcp *cpv1beta1.K0sControlPlane, infraRef corev1.ObjectReference) *clusterv1.Machine {
	ver := semver.MustParse(kcp.Spec.K0sVersion)
	v := fmt.Sprintf("%d.%d.%d", ver.Major(), ver.Minor(), ver.Patch())
	return &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kcp.Namespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/cluster-name":         kcp.Name,
				"cluster.x-k8s.io/control-plane":        "true",
				"cluster.x-k8s.io/generateMachine-role": "control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{
			Version:     &v,
			ClusterName: kcp.Name,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					APIVersion: "bootstrap.cluster.x-k8s.io/v1beta1",
					Kind:       "K0sControllerConfig",
					Name:       name,
				},
			},
			InfrastructureRef: infraRef,
		},
	}
}

func (c *K0sController) createMachineFromTemplate(ctx context.Context, name string, _ *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (*unstructured.Unstructured, error) {
	machineFromTemplate, err := c.generateMachineFromTemplate(ctx, name, kcp)
	if err != nil {
		return nil, err
	}

	if err = c.Client.Patch(ctx, machineFromTemplate, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	}); err != nil {
		return nil, err
	}

	return machineFromTemplate, nil
}

//func (c *K0sController) deleteMachineFromTemplate(ctx context.Context, name string, kcp *cpv1beta1.K0sControlPlane) error {
//	machineFromTemplate, err := c.generateMachineFromTemplate(ctx, name, kcp)
//	if err != nil {
//		return err
//	}
//
//	return c.Client.Delete(ctx, machineFromTemplate)
//}

func (c *K0sController) generateMachineFromTemplate(ctx context.Context, name string, kcp *cpv1beta1.K0sControlPlane) (*unstructured.Unstructured, error) {
	unstructuredMachineTemplate, err := c.getMachineTemplate(ctx, kcp)
	if err != nil {
		return nil, err
	}

	_ = ctrl.SetControllerReference(kcp, unstructuredMachineTemplate, c.Scheme)

	template, found, err := unstructured.NestedMap(unstructuredMachineTemplate.UnstructuredContent(), "spec", "template")
	if !found {
		return nil, errors.Errorf("missing spec.template on %v %q", unstructuredMachineTemplate.GroupVersionKind(), unstructuredMachineTemplate.GetName())
	} else if err != nil {
		return nil, errors.Wrapf(err, "error getting spec.template map on %v %q", unstructuredMachineTemplate.GroupVersionKind(), unstructuredMachineTemplate.GetName())
	}

	machine := &unstructured.Unstructured{Object: template}
	machine.SetName(name)
	machine.SetNamespace(kcp.Namespace)

	annotations := map[string]string{}
	for key, value := range kcp.Annotations {
		annotations[key] = value
	}
	annotations[clusterv1.TemplateClonedFromNameAnnotation] = kcp.Spec.MachineTemplate.InfrastructureRef.Name
	annotations[clusterv1.TemplateClonedFromGroupKindAnnotation] = kcp.Spec.MachineTemplate.InfrastructureRef.GroupVersionKind().GroupKind().String()
	machine.SetAnnotations(annotations)

	labels := map[string]string{}
	for key, value := range kcp.Labels {
		labels[key] = value
	}

	labels[clusterv1.ClusterNameLabel] = kcp.Name
	machine.SetLabels(labels)

	machine.SetAPIVersion(unstructuredMachineTemplate.GetAPIVersion())
	machine.SetKind(strings.TrimSuffix(unstructuredMachineTemplate.GetKind(), clusterv1.TemplateSuffix))

	return machine, nil
}
