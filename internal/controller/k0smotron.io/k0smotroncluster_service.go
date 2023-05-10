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

package k0smotronio

import (
	"context"
	"time"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ClusterReconciler) generateService(kmc *km.Cluster) v1.Service {
	var name string
	switch kmc.Spec.Service.Type {
	case v1.ServiceTypeNodePort:
		name = kmc.GetNodePortName()
	case v1.ServiceTypeLoadBalancer:
		name = kmc.GetLoadBalancerName()
	}

	svc := v1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		Spec: v1.ServiceSpec{
			Type:     kmc.Spec.Service.Type,
			Selector: map[string]string{"app": "k0smotron"},
			Ports: []v1.ServicePort{
				{
					Port:       int32(kmc.Spec.Service.APIPort),
					TargetPort: intstr.FromInt(kmc.Spec.Service.APIPort),
					Name:       "api",
					NodePort:   int32(kmc.Spec.Service.APIPort),
				},
				{
					Port:       int32(kmc.Spec.Service.KonnectivityPort),
					TargetPort: intstr.FromInt(kmc.Spec.Service.KonnectivityPort),
					Name:       "konnectivity",
					NodePort:   int32(kmc.Spec.Service.KonnectivityPort),
				},
			},
		},
	}

	_ = ctrl.SetControllerReference(kmc, &svc, r.Scheme)

	return svc
}

func (r *ClusterReconciler) reconcileServices(ctx context.Context, kmc km.Cluster) error {
	logger := log.FromContext(ctx)
	// Depending on ingress configuration create nodePort service.
	logger.Info("Reconciling services")
	svc := r.generateService(&kmc)
	if kmc.Spec.Service.Type == v1.ServiceTypeLoadBalancer && kmc.Spec.ExternalAddress == "" {
		err := r.watchLBServiceIP(ctx, kmc)
		if err != nil {
			return err
		}
	}
	return r.Client.Patch(ctx, &svc, client.Apply, patchOpts...)
}

func (r *ClusterReconciler) watchLBServiceIP(ctx context.Context, kmc km.Cluster) error {
	var handler cache.ResourceEventHandlerFuncs
	stopCh := make(chan struct{})
	handler.UpdateFunc = func(old, new interface{}) {
		newObj, ok := new.(*v1.Service)
		if !ok {
			return
		}

		var externalAddress string
		if len(newObj.Status.LoadBalancer.Ingress) > 0 {
			if newObj.Status.LoadBalancer.Ingress[0].Hostname != "" {
				externalAddress = newObj.Status.LoadBalancer.Ingress[0].Hostname
			}
			if newObj.Status.LoadBalancer.Ingress[0].IP != "" {
				externalAddress = newObj.Status.LoadBalancer.Ingress[0].IP
			}

			if externalAddress != "" {
				kmc.Spec.ExternalAddress = externalAddress
				err := r.Client.Update(ctx, &kmc)
				if err != nil {
					log.FromContext(ctx).Error(err, "failed to update K0smotronCluster")
				}
				stopCh <- struct{}{}
			}
		}
	}

	inf := informerv1.NewServiceInformer(r.ClientSet, kmc.Namespace, 100*time.Millisecond, nil)
	_, err := inf.AddEventHandler(handler)
	if err != nil {
		return err
	}

	go inf.Run(stopCh)

	return nil
}
