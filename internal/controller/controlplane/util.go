package controlplane

import (
	"context"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *K0sController) getMachineTemplate(ctx context.Context, kcp *cpv1beta1.K0sControlPlane) (*unstructured.Unstructured, error) {
	infRef := kcp.Spec.MachineTemplate.InfrastructureRef

	machineTemplate := new(unstructured.Unstructured)
	machineTemplate.SetAPIVersion(infRef.APIVersion)
	machineTemplate.SetKind(infRef.Kind)
	machineTemplate.SetName(infRef.Name)

	key := client.ObjectKey{Name: infRef.Name, Namespace: infRef.Namespace}

	err := c.Get(ctx, key, machineTemplate)
	if err != nil {
		return nil, err
	}
	return machineTemplate, nil
}
