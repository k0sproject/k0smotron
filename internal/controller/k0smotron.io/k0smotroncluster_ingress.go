package k0smotronio

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
)

func (scope *kmcScope) reconcileIngress(ctx context.Context, kmc *km.Cluster) error {
	if kmc.Spec.Ingress == nil {
		return nil
	}
	configMap := scope.generateIngressManifestsConfigMap(kmc)
	_ = ctrl.SetControllerReference(kmc, &configMap, scope.client.Scheme())
	err := scope.client.Patch(ctx, &configMap, client.Apply, patchOpts...)
	if err != nil {
		return fmt.Errorf("failed to patch haproxy configmap for ingress: %w", err)
	}
	var foundManifest bool
	for _, manifest := range kmc.Spec.Manifests {
		if manifest.Name == kmc.GetIngressManifestsConfigMapName() {
			foundManifest = true
			break
		}
	}
	if !foundManifest {
		kmc.Spec.Manifests = append(kmc.Spec.Manifests, corev1.Volume{
			Name: kmc.GetIngressManifestsConfigMapName(),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: kmc.GetIngressManifestsConfigMapName(),
					},
				},
			},
		})
	}

	ingress := scope.generateIngress(kmc)
	_ = ctrl.SetControllerReference(kmc, &ingress, scope.client.Scheme())
	return scope.client.Patch(ctx, &ingress, client.Apply, patchOpts...)
}

func (scope *kmcScope) generateIngressManifestsConfigMap(kmc *km.Cluster) corev1.ConfigMap {
	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressManifestsConfigMapName(),
			Namespace:   kmc.Namespace,
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Data: map[string]string{
			// Due to k0s behavior, we need to create a dummy Endpoints object to create a worker profile
			// Once a local haproxy is up, it will update the Endpoints object to point with the actual proxy's IP
			"0_temp-kubernetes-ep.yaml": `apiVersion: v1
kind: Endpoints
metadata:
  name: kubernetes
  namespace: default
subsets:
- addresses:
  - ip: 1.2.3.4
  ports:
  - name: https
    port: 7443
    protocol: TCP`,
			"1_kube-service.yaml": `apiVersion: v1
kind: Service
metadata:
  labels:
    component: apiserver
    provider: kubernetes
  name: kubernetes
  namespace: default
spec:
  clusterIP: 10.96.0.1
  clusterIPs:
  - 10.96.0.1
  internalTrafficPolicy: Local
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  ports:
  - name: https
    port: 443
    protocol: TCP
    targetPort: 7443
  selector:
    app: k0smotron-ingress-haproxy
  sessionAffinity: None
  type: ClusterIP`,
		},
	}

	return configMap
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
