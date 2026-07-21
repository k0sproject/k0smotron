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
package k0smotronio

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kcontrollerutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
)

func (scope *kmcScope) reconcileIngress(ctx context.Context, kmc *km.Cluster) error {
	if kmc.Spec.Ingress == nil {
		return nil
	}

	if err := scope.ensureIngressProxyCerts(ctx, kmc); err != nil {
		return fmt.Errorf("error generating ingress certificates: %w", err)
	}

	// Best-effort cleanup of the old ConfigMap-based bundle: prior to
	// switching the bundle to a Secret, the manifests were delivered via a
	// ConfigMap of this name. Remove the stale object so upgraded clusters
	// don't leave it orphaned in the management cluster.
	staleConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetIngressManifestsConfigName(),
			Namespace: kmc.Namespace,
		},
	}
	if err := scope.client.Delete(ctx, &staleConfigMap); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete stale ingress manifests configmap: %w", err)
	}

	proxyCert, proxyKey, caCert, err := scope.loadIngressCertMaterial(ctx, kmc)
	if err != nil {
		return fmt.Errorf("failed to load ingress cert material: %w", err)
	}

	proxyBundle, err := scope.generateIngressManifestsSecret(kmc, proxyCert, proxyKey, caCert)
	if err != nil {
		return fmt.Errorf("failed to generate ingress manifests secret: %w", err)
	}
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &proxyBundle, scope.client.Scheme(), scope.externalOwner)
	if err = scope.reconcileResource(ctx, kmc, &proxyBundle); err != nil {
		return fmt.Errorf("failed to reconcile proxy manifest bundle: %w", err)
	}

	// On k0s versions that honor spec.konnectivity.externalAddress the agent is
	// deployed by k0s itself; on older versions the field is ignored, so we ship
	// our own konnectivity agent manifest pointed at the ingress endpoint.
	if !kmc.Spec.HasNativeIngressKonnectivity() {
		konnectivityCM, err := scope.generateKonnectivityIngressConfigMap(kmc)
		if err != nil {
			return fmt.Errorf("failed to generate konnectivity ingress configmap: %w", err)
		}
		_ = kcontrollerutil.SetExternalOwnerReference(kmc, &konnectivityCM, scope.client.Scheme(), scope.externalOwner)
		if err = scope.reconcileResource(ctx, kmc, &konnectivityCM); err != nil {
			return fmt.Errorf("failed to reconcile konnectivity ingress configmap: %w", err)
		}
	}

	upsertIngressManifestVolumes(kmc)

	if *kmc.Spec.Ingress.Deploy {
		ingress := scope.generateIngress(kmc)
		_ = kcontrollerutil.SetExternalOwnerReference(kmc, &ingress, scope.client.Scheme(), scope.externalOwner)
		return scope.reconcileResource(ctx, kmc, &ingress)
	}

	return nil
}

// upsertIngressManifestVolumes ensures kmc.Spec.Manifests contains the ingress
// bundle volume (Secret-sourced), inserting it if missing. If the volume
// already exists (e.g. from a cluster created before the bundle was switched
// from a ConfigMap to a Secret), it is overwritten in place so the stale
// ConfigMap source is migrated to the Secret source.
//
// The "konnectivity" volume (ConfigMap-sourced) is kept only on k0s versions
// that don't deploy the konnectivity agent natively; on native versions it is
// dropped so the control-plane pod doesn't fail mounting a ConfigMap we no
// longer ship.
func upsertIngressManifestVolumes(kmc *km.Cluster) {
	ingressVolume := corev1.Volume{
		Name: kmc.GetIngressManifestsConfigName(),
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: kmc.GetIngressManifestsConfigName(),
			},
		},
	}
	konnectivityVolume := corev1.Volume{
		Name: "konnectivity",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: kmc.GetIngressManifestsConfigName() + "-konnectivity",
				},
			},
		},
	}
	native := kmc.Spec.HasNativeIngressKonnectivity()

	var foundIngress, foundKonnectivity bool
	filtered := kmc.Spec.Manifests[:0]
	for _, m := range kmc.Spec.Manifests {
		switch m.Name {
		case ingressVolume.Name:
			filtered = append(filtered, ingressVolume)
			foundIngress = true
		case konnectivityVolume.Name:
			if native {
				// Drop: konnectivity is deployed by k0s directly.
				continue
			}
			filtered = append(filtered, konnectivityVolume)
			foundKonnectivity = true
		default:
			filtered = append(filtered, m)
		}
	}
	kmc.Spec.Manifests = filtered
	if !foundIngress {
		kmc.Spec.Manifests = append(kmc.Spec.Manifests, ingressVolume)
	}
	if !native && !foundKonnectivity {
		kmc.Spec.Manifests = append(kmc.Spec.Manifests, konnectivityVolume)
	}
}

const (
	konnectivityDefaultImage   = "quay.io/k0sproject/apiserver-network-proxy-agent"
	konnectivityDefaultVersion = "v0.33.0"
)

const traefikProxyImage = "quay.io/k0sproject/traefik:v3.7.4-k0s.0"

// ingressProxyCertPurpose is the secret.Certificate purpose (and the
// "<cluster>-ingress-proxy" secret suffix) for the node-local proxy's TLS
// server certificate. Named after the ingress feature, not the proxy
// implementation.
const ingressProxyCertPurpose = "ingress-proxy"

// hostSNIRuleAll is Traefik's TCP catch-all SNI matcher. It lives in its own
// const because the backtick-quoted `*` cannot appear inside a Go raw string
// literal, which lets the config bodies below stay as plain raw strings.
const hostSNIRuleAll = "HostSNI(`*`)"

// generateTraefikConfig returns the Traefik static (config.yaml) and dynamic
// (dynamic.yaml) file bodies for the node-local proxy. It replicates the old
// HAProxy behavior: terminate the pod TLS with the cluster-CA server cert, then
// re-encrypt to the external ingress host with SNI + CA verification.
//
// The client-facing TLS termination pins ALPN to http/1.1. This proxy is a
// TCP terminate-and-reencrypt byte pipe: the protocol negotiated on the client
// leg must match the one on the backend leg. The backend (TCP serversTransport)
// dials without ALPN, so the apiserver serves http/1.1; without this pin the
// client would negotiate h2 on the front leg and then receive an http/1.1
// response, failing with "http2: frame too large ... looked like an HTTP/1.1
// header". HAProxy avoided this by advertising no ALPN on either leg.
func generateTraefikConfig(apiHost string, port int64) (string, string) {
	static := `global:
  checkNewVersion: false
  sendAnonymousUsage: false
log:
  level: INFO
  noColor: true
entryPoints:
  kubeapi:
    address: ":7443"
providers:
  file:
    filename: /etc/traefik/dynamic.yaml
    watch: true
`
	dynamic := fmt.Sprintf(`tls:
  options:
    kubeapi:
      alpnProtocols:
        - http/1.1
  stores:
    default:
      defaultCertificate:
        certFile: /etc/traefik/certs/server.crt
        keyFile: /etc/traefik/certs/server.key
tcp:
  routers:
    kubeapi:
      entryPoints: ["kubeapi"]
      rule: "%s"
      tls:
        options: kubeapi
      service: kubeapi
  services:
    kubeapi:
      loadBalancer:
        serversTransport: kubeapi
        servers:
          - address: "%s:%d"
            tls: true
  serversTransports:
    kubeapi:
      tls:
        serverName: "%s"
        rootCAs:
          - /etc/traefik/certs/ca.crt
`, hostSNIRuleAll, apiHost, port, apiHost)

	return static, dynamic
}

// generateTraefikConfigWindows returns the Traefik dynamic file body for the Windows HostProcess node-local proxy.
func generateTraefikConfigWindows(apiHost string, port int64) string {
	return fmt.Sprintf(`tls:
  options:
    kubeapi:
      alpnProtocols:
        - http/1.1
  stores:
    default:
      defaultCertificate:
        certFile: C:\ProgramData\k0smotron\traefik\certs\server.crt
        keyFile: C:\ProgramData\k0smotron\traefik\certs\server.key
tcp:
  routers:
    kubeapi:
      entryPoints: ["kubeapi"]
      rule: "%s"
      tls:
        options: kubeapi
      service: kubeapi
  services:
    kubeapi:
      loadBalancer:
        serversTransport: kubeapi
        servers:
          - address: "%s:%d"
            tls: true
  serversTransports:
    kubeapi:
      tls:
        serverName: "%s"
        rootCAs:
          - C:\ProgramData\k0smotron\traefik\certs\ca.crt
`, hostSNIRuleAll, apiHost, port, apiHost)
}

// overrideImageRepository replicates k0s's overrideRepository logic from
// pkg/apis/k0s/v1beta1/images.go: replaces the registry host of originalImage
// with repository, or prepends repository/ if no host is present.
func overrideImageRepository(repository, originalImage string) string {
	if repository == "" {
		return originalImage
	}
	if strings.HasPrefix(originalImage, repository) {
		return originalImage
	}
	if host := imageRegistryHost(originalImage); host != "" {
		return strings.Replace(originalImage, host, repository, 1)
	}
	return fmt.Sprintf("%s/%s", repository, originalImage)
}

// imageRegistryHost replicates k0s's getHostName: returns the registry host
// portion of an image reference, or "" if the first path component has no
// dot/colon and is not "localhost" (i.e. it's not a registry host).
func imageRegistryHost(imageName string) string {
	i := strings.IndexRune(imageName, '/')
	if i == -1 {
		return ""
	}
	host := imageName[:i]
	if !strings.ContainsAny(host, ".:") && host != "localhost" {
		return ""
	}
	return host
}

func (scope *kmcScope) getKonnectivityAgentImage(kmc *km.Cluster) string {
	image := konnectivityDefaultImage
	version := konnectivityDefaultVersion
	customImage := false

	if kmc.Spec.K0sConfig != nil {
		if v, _, _ := unstructured.NestedString(kmc.Spec.K0sConfig.Object, "spec", "images", "konnectivity", "image"); v != "" {
			image = v
			customImage = true
			version = "" // don't apply default version to a custom image
		}
		if v, _, _ := unstructured.NestedString(kmc.Spec.K0sConfig.Object, "spec", "images", "konnectivity", "version"); v != "" {
			version = v
		}
		repo, _, _ := unstructured.NestedString(kmc.Spec.K0sConfig.Object, "spec", "images", "repository")
		image = overrideImageRepository(repo, image)
	}

	if customImage && version == "" {
		return image
	}
	return fmt.Sprintf("%s:%s", image, version)
}

func (scope *kmcScope) getKonnectivityAgentPullPolicy(kmc *km.Cluster) string {
	if kmc.Spec.K0sConfig != nil {
		if v, _, _ := unstructured.NestedString(kmc.Spec.K0sConfig.Object, "spec", "images", "default_pull_policy"); v != "" {
			return v
		}
	}
	return "IfNotPresent"
}

// loadIngressCertMaterial fetches the ingress proxy TLS keypair and the
// cluster CA certificate from their respective Secrets so they can be
// embedded into the Traefik proxy bundle.
func (scope *kmcScope) loadIngressCertMaterial(ctx context.Context, kmc *km.Cluster) (cert, key, ca []byte, err error) {
	var proxySecret corev1.Secret
	if err := scope.client.Get(ctx, client.ObjectKey{
		Namespace: kmc.Namespace,
		Name:      secret.Name(kmc.Name, ingressProxyCertPurpose),
	}, &proxySecret); err != nil {
		return nil, nil, nil, fmt.Errorf("getting ingress proxy cert secret: %w", err)
	}

	var caSecret corev1.Secret
	if err := scope.client.Get(ctx, client.ObjectKey{
		Namespace: kmc.Namespace,
		Name:      secret.Name(kmc.Name, secret.ClusterCA),
	}, &caSecret); err != nil {
		return nil, nil, nil, fmt.Errorf("getting cluster CA secret: %w", err)
	}

	return proxySecret.Data["tls.crt"], proxySecret.Data["tls.key"], caSecret.Data["tls.crt"], nil
}

// generateIngressManifestsSecret builds the node-local proxy manifest bundle
// delivered into the workload cluster. It is a Secret (not a ConfigMap) because
// the bundle embeds the proxy's server private key (2_proxy-certs.yaml); the
// bundle volume name is unchanged so the k0s stack applier keeps pruning the
// old resources.
func (scope *kmcScope) generateIngressManifestsSecret(kmc *km.Cluster, proxyCert, proxyKey, caCert []byte) (corev1.Secret, error) {
	staticCfg, dynamicCfg := generateTraefikConfig(kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port)
	dynamicWinCfg := generateTraefikConfigWindows(kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port)

	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressManifestsConfigName(),
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentIngress),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		StringData: map[string]string{
			// Dummy Endpoints so a worker profile can be created before the proxy
			// updates the real kubernetes Endpoints. (unchanged)
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

			"1_proxy-config.yaml": fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: k0smotron-proxy-config
  namespace: default
data:
  config.yaml: |
%s
  dynamic.yaml: |
%s
  dynamic-win.yaml: |
%s`, indentBlock(staticCfg, "    "), indentBlock(dynamicCfg, "    "), indentBlock(dynamicWinCfg, "    ")),

			"2_proxy-certs.yaml": fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: k0smotron-proxy-certs
  namespace: default
type: Opaque
data:
  server.crt: %s
  server.key: %s
  ca.crt: %s`,
				base64.StdEncoding.EncodeToString(proxyCert),
				base64.StdEncoding.EncodeToString(proxyKey),
				base64.StdEncoding.EncodeToString(caCert)),

			"2a_proxy-ds-linux.yaml": fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: k0smotron-proxy
  namespace: default
  labels:
    app: k0smotron-proxy
spec:
  selector:
    matchLabels:
      app: k0smotron-proxy
  template:
    metadata:
      labels:
        app: k0smotron-proxy
    spec:
      hostNetwork: true
      nodeSelector:
        kubernetes.io/os: linux
      tolerations:
        - key: "node.kubernetes.io/not-ready"
          operator: "Exists"
          effect: "NoSchedule"
        - key: "node.kubernetes.io/unreachable"
          operator: "Exists"
          effect: "NoSchedule"
        - key: "node.cloudprovider.kubernetes.io/uninitialized"
          operator: "Exists"
          effect: "NoSchedule"
      containers:
        - name: traefik
          image: %s
          args:
            - --configFile=/etc/traefik/config.yaml
          ports:
            - containerPort: 7443
              name: https
          volumeMounts:
            - name: config
              mountPath: /etc/traefik/config.yaml
              subPath: config.yaml
              readOnly: true
            - name: config
              mountPath: /etc/traefik/dynamic.yaml
              subPath: dynamic.yaml
              readOnly: true
            - name: certs
              mountPath: /etc/traefik/certs
              readOnly: true
      volumes:
        - name: config
          configMap:
            name: k0smotron-proxy-config
        - name: certs
          secret:
            secretName: k0smotron-proxy-certs
`, traefikProxyImage),

			"2b_proxy-ds-windows.yaml": fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: k0smotron-proxy-win
  namespace: default
  labels:
    app: k0smotron-proxy
spec:
  selector:
    matchLabels:
      app: k0smotron-proxy
  template:
    metadata:
      labels:
        app: k0smotron-proxy
    spec:
      hostNetwork: true
      nodeSelector:
        kubernetes.io/os: windows
      securityContext:
        windowsOptions:
          hostProcess: true
          runAsUserName: "NT AUTHORITY\\Local service"
      tolerations:
        - operator: "Exists"
      initContainers:
        - name: setup
          image: %s
          command: ["cmd.exe", "/c"]
          args:
            - robocopy %%CONTAINER_SANDBOX_MOUNT_POINT%%\etc\traefik C:\ProgramData\k0smotron\traefik dynamic-win.yaml & robocopy %%CONTAINER_SANDBOX_MOUNT_POINT%%\etc\certs C:\ProgramData\k0smotron\traefik\certs & if errorlevel 8 exit /b 1 & exit /b 0 
          volumeMounts:
            - name: config
              mountPath: \etc\traefik
              readOnly: true
            - name: certs
              mountPath: \etc\certs
              readOnly: true
      containers:
        - name: traefik
          image: %s
          args:
            - --entrypoints.kubeapi.address=:7443
            - --providers.file.filename=C:\ProgramData\k0smotron\traefik\dynamic-win.yaml
            - --log.level=INFO
            - --global.checknewversion=false
            - --global.sendanonymoususage=false
          ports:
            - containerPort: 7443
              name: https
      volumes:
        - name: config
          configMap:
            name: k0smotron-proxy-config
        - name: certs
          secret:
            secretName: k0smotron-proxy-certs
`, traefikProxyImage, traefikProxyImage),

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
    app: k0smotron-proxy
  sessionAffinity: None
  type: ClusterIP`, scope.clusterSettings.kubernetesServiceIP, scope.clusterSettings.kubernetesServiceIP),
		},
	}

	return secret, nil
}

// indentBlock prefixes every line of s with indent (for embedding a YAML
// document inside a `|` block scalar).
func indentBlock(s, indent string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

// generateKonnectivityIngressConfigMap builds the konnectivity agent manifest
// bundle for k0s versions that don't deploy it natively. It is delivered to the
// workload cluster via the "konnectivity" manifest volume and points the agent
// at the ingress konnectivity endpoint (KonnectivityHost:Ingress.Port).
func (scope *kmcScope) generateKonnectivityIngressConfigMap(kmc *km.Cluster) (corev1.ConfigMap, error) {
	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressManifestsConfigName() + "-konnectivity",
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentIngress),
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
        - image: %s
          imagePullPolicy: %s
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
                  audience: system:konnectivity-server`, scope.getKonnectivityAgentImage(kmc), scope.getKonnectivityAgentPullPolicy(kmc), kmc.Spec.Ingress.KonnectivityHost, kmc.Spec.Ingress.Port),
		},
	}

	return configMap, nil
}

func (scope *kmcScope) generateIngress(kmc *km.Cluster) v1.Ingress {
	annotations := kcontrollerutil.AnnotationsForK0smotronCluster(kmc)
	if annotations == nil {
		annotations = make(map[string]string)
	}
	maps.Copy(annotations, kmc.Spec.Ingress.Annotations)
	ingress := v1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetIngressName(),
			Namespace:   kmc.Namespace,
			Annotations: annotations,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentIngress),
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
								PathType: new(v1.PathTypePrefix),
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
								PathType: new(v1.PathTypePrefix),
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
