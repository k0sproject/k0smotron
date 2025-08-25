package k0smotronio

import (
	"context"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

func (scope *kmcScope) reconcileIngress(ctx context.Context, kmc *km.Cluster) error {
	ingress := scope.generateIngress(kmc)
	_ = ctrl.SetControllerReference(kmc, &ingress, scope.client.Scheme())
	return scope.client.Patch(ctx, &ingress, client.Apply, patchOpts...)
}

func (scope *kmcScope) generateIngress(kmc *km.Cluster) v1.Ingress {
	annotations := kcontrollerutil.AnnotationsForK0smotronCluster(kmc)
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for k, v := range kmc.Spec.Ingress.Annotations {
		annotations[k] = v
	}
	ingress := v1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressName(),
			Namespace:   kmc.Namespace,
			Annotations: annotations,
			Labels:      kcontrollerutil.LabelsForK0smotronCluster(kmc),
		},
		Spec: v1.IngressSpec{
			Rules: []v1.IngressRule{
				{
					Host: kmc.Spec.Ingress.APIHost,
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{{
								Path:     "/",
								PathType: ptr.To(v1.PathTypePrefix),
								Backend: v1.IngressBackend{
									Service: &v1.IngressServiceBackend{
										Name: kmc.GetServiceName(),
										Port: v1.ServiceBackendPort{
											Number: int32(kmc.Spec.Service.APIPort),
										},
									},
								},
							}},
						},
					},
				},
				{
					Host: kmc.Spec.Ingress.KonnectivityHost,
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{{
								Path:     "/",
								PathType: ptr.To(v1.PathTypePrefix),
								Backend: v1.IngressBackend{
									Service: &v1.IngressServiceBackend{
										Name: kmc.GetServiceName(),
										Port: v1.ServiceBackendPort{
											Number: int32(kmc.Spec.Service.KonnectivityPort),
										},
									},
								},
							}},
						},
					},
				},
			},
		},
	}

	if kmc.Spec.Ingress.ClassName != "" {
		ingress.Spec.IngressClassName = &kmc.Spec.Ingress.ClassName
	}

	return ingress

}
