package k0smotronio

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
)

func (scope *kmcScope) reconcileIngress(ctx context.Context, kmc *km.Cluster) error {
	if kmc.Spec.Ingress == nil {
		return nil
	}

	configMap, err := scope.generateIngressManifestsConfigMap(kmc)
	if err != nil {
		return fmt.Errorf("failed to generate ingress manifests configmap: %w", err)
	}
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &configMap, scope.client.Scheme(), scope.externalOwner)
	err = scope.client.Patch(ctx, &configMap, client.Apply, patchOpts...)
	if err != nil {
		return fmt.Errorf("failed to patch haproxy configmap for ingress: %w", err)
	}

	configMap, err = scope.generateKonnectivityIngressConfigMap(kmc)
	if err != nil {
		return fmt.Errorf("failed to generate ingress manifests configmap: %w", err)
	}
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &configMap, scope.client.Scheme(), scope.externalOwner)

	err = scope.client.Patch(ctx, &configMap, client.Apply, patchOpts...)

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
		}, corev1.Volume{
			Name: "konnectivity",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: kmc.GetIngressManifestsConfigMapName() + "-konnectivity",
					},
				},
			},
		})
	}

	if *kmc.Spec.Ingress.Deploy {
		ingress := scope.generateIngress(kmc)
		_ = kcontrollerutil.SetExternalOwnerReference(kmc, &ingress, scope.client.Scheme(), scope.externalOwner)
		return scope.client.Patch(ctx, &ingress, client.Apply, patchOpts...)
	}

	return nil
}

func (scope *kmcScope) generateIngressManifestsConfigMap(kmc *km.Cluster) (corev1.ConfigMap, error) {
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
			"1_haproxy-configmap.yaml": fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: k0smotron-haproxy-config
  namespace: default
data:
  haproxy.cfg: |
    frontend kubeapi_front
        bind [::]:7443 v4v6 ssl crt /etc/haproxy/certs/server.pem
        mode tcp
        default_backend kubeapi_back

    backend kubeapi_back
        mode tcp
        server kube_api %s:%d ssl verify required ca-file /etc/haproxy/certs/ca.crt sni str(%s)
`, kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port, kmc.Spec.Ingress.APIHost),
			"2_haproxy-ds.yaml": `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: k0smotron-haproxy
  namespace: default
  labels:
    app: k0smotron-haproxy
spec:
  selector:
    matchLabels:
      app: k0smotron-haproxy
  template:
    metadata:
      labels:
        app: k0smotron-haproxy
    spec:
      hostNetwork: true
      containers:
        - name: haproxy
          image: haproxy:2.8
          args:
            - -f
            - /usr/local/etc/haproxy/haproxy.cfg
          ports:
            - containerPort: 7443
              name: https
          volumeMounts:
            - name: haproxy-config
              mountPath: /usr/local/etc/haproxy/haproxy.cfg
              subPath: haproxy.cfg
            - name: haproxy-certs
              mountPath: /etc/haproxy/certs
              readOnly: true
      volumes:
        - name: haproxy-config
          configMap:
            name: k0smotron-haproxy-config
        - name: haproxy-certs
          hostPath:
            path: /etc/haproxy/certs
            type: DirectoryOrCreate
`,
			"3_kube-service.yaml": fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  labels:
    component: apiserver
    provider: kubernetes
  name: kubernetes
  namespace: default
spec:
  clusterIP: %s
  clusterIPs:
  - %s
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
    app: k0smotron-haproxy
  sessionAffinity: None
  type: ClusterIP`, scope.clusterSettings.kubernetesServiceIP, scope.clusterSettings.kubernetesServiceIP),
		},
	}

	return configMap, nil
}

func (scope *kmcScope) generateKonnectivityIngressConfigMap(kmc *km.Cluster) (corev1.ConfigMap, error) {
	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressManifestsConfigMapName() + "-konnectivity",
			Namespace:   kmc.Namespace,
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Data: map[string]string{
			"konnectivity-agent.yaml": fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:konnectivity-server
  labels:
    kubernetes.io/cluster-service: "true"
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:konnectivity-server
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: konnectivity-agent
  namespace: kube-system
  labels:
    kubernetes.io/cluster-service: "true"
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  labels:
    k8s-app: konnectivity-agent
  namespace: kube-system
  name: konnectivity-agent
spec:
  selector:
    matchLabels:
      k8s-app: konnectivity-agent
  template:
    metadata:
      labels:
        k8s-app: konnectivity-agent
      annotations:
        prometheus.io/scrape: 'true'
        prometheus.io/port: '8093'
    spec:
      securityContext:
        runAsNonRoot: true
        supplementalGroups: [0]
      nodeSelector:
        kubernetes.io/os: linux
      priorityClassName: system-cluster-critical
      tolerations:
        - operator: Exists
      containers:
        - image: quay.io/k0sproject/apiserver-network-proxy-agent:v0.33.0
          imagePullPolicy: IfNotPresent
          name: konnectivity-agent
          command: ["/proxy-agent"]
          env:
              - name: NODE_IP
                valueFrom:
                  fieldRef:
                    fieldPath: status.hostIP
          args:
            - --logtostderr=true
            - --ca-cert=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
            - --proxy-server-host=%s
            - --proxy-server-port=%d
            - --service-account-token-path=/var/run/secrets/tokens/konnectivity-agent-token
            - --agent-identifiers=host=$(NODE_IP)
            - --agent-id=$(NODE_IP)
          volumeMounts:
            - mountPath: /var/run/secrets/tokens
              name: konnectivity-agent-token
          livenessProbe:
            httpGet:
              port: 8093
              path: /healthz
            initialDelaySeconds: 15
            timeoutSeconds: 15
          readinessProbe:
            httpGet:
              port: 8093
              path: /readyz
            initialDelaySeconds: 15
            timeoutSeconds: 15
      serviceAccountName: konnectivity-agent
      volumes:
        - name: konnectivity-agent-token
          projected:
            sources:
              - serviceAccountToken:
                  path: konnectivity-agent-token
                  audience: system:konnectivity-server`, kmc.Spec.Ingress.KonnectivityHost, kmc.Spec.Ingress.Port),
		},
	}

	return configMap, nil
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
			IngressClassName: kmc.Spec.Ingress.ClassName,
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

	return ingress
}
