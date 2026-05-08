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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	kubeconfig "k8s.io/kubernetes/cmd/kubeadm/app/util/kubeconfig"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/exec"
)

// kubeconfigRenewalThreshold is the safety margin before client cert expiry that triggers regeneration.
// Above this threshold the existing kubeconfig is reused; this avoids a hot reconcile loop where the
// admin cert (and therefore the secret content) would otherwise be regenerated on every reconcile.
const kubeconfigRenewalThreshold = 30 * 24 * time.Hour

func (scope *kmcScope) reconcileKubeConfigSecret(ctx context.Context, managementClusterClient client.Client, kmc *km.Cluster) error {
	logger := log.FromContext(ctx)

	var existing v1.Secret
	err := managementClusterClient.Get(ctx, client.ObjectKey{Name: kmc.GetAdminConfigSecretName(), Namespace: kmc.Namespace}, &existing)
	if err != nil && !apierrors.IsNotFound(err) {
		scope.currentReconcileState.controlplane.kubeconfig.message = err.Error()
		return err
	}
	if kubeconfigStillValid(&existing, kmc) {
		scope.currentReconcileState.controlplane.kubeconfig.data = existing.DeepCopy()
		return nil
	}

	pod, err := findStatefulSetPod(ctx, kmc.GetStatefulSetName(), kmc.Namespace, scope.clienSet)
	if err != nil {
		scope.currentReconcileState.controlplane.kubeconfig.message = err.Error()
		return err
	}

	output, err := exec.PodExecCmdOutput(ctx, scope.clienSet, scope.restConfig, pod.Name, kmc.Namespace, "k0s kubeconfig create admin --groups system:masters")
	if err != nil {
		scope.currentReconcileState.controlplane.kubeconfig.message = err.Error()
		return err
	}

	// Post-process: build a kubeconfig with desired names based on current-context
	processedOutput, err := rewriteKubeconfigValues(output, kmc)
	if err != nil {
		scope.currentReconcileState.controlplane.kubeconfig.message = err.Error()
		return err
	}

	logger.Info("Kubeconfig generated, creating the secret")

	secret := v1.Secret{
		// The dynamic r.Client needs TypeMeta to be set
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetAdminConfigSecretName(),
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentKubeconfig),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		StringData: map[string]string{"value": processedOutput},
		Type:       clusterv1.ClusterSecretType,
	}

	// workload cluster kubeconfig is always created in the management cluster so we set k0smotron cluster as owner
	_ = ctrl.SetControllerReference(kmc, &secret, scope.client.Scheme())

	err = managementClusterClient.Patch(ctx, &secret, client.Apply, patchOpts...)
	if err != nil {
		scope.currentReconcileState.controlplane.kubeconfig.message = err.Error()
		return err
	}

	scope.currentReconcileState.controlplane.kubeconfig.data = secret.DeepCopy()
	return nil
}

func rewriteKubeconfigValues(kubeconfigYAML string, kmc *km.Cluster) (string, error) {
	obj, err := k8sruntime.Decode(clientcmdlatest.Codec, []byte(kubeconfigYAML))
	if err != nil {
		return "", err
	}
	srcCfg, ok := obj.(*clientcmdapi.Config)
	if !ok {
		return "", fmt.Errorf("failed to decode kubeconfig")
	}

	if srcCfg.CurrentContext == "" {
		return "", fmt.Errorf("current-context is empty")
	}
	srcCtx, ok := srcCfg.Contexts[srcCfg.CurrentContext]
	if !ok {
		return "", fmt.Errorf("current-context %q not found", srcCfg.CurrentContext)
	}

	srcCluster, ok := srcCfg.Clusters[srcCtx.Cluster]
	if !ok {
		return "", fmt.Errorf("cluster %q not found", srcCtx.Cluster)
	}
	srcUser, ok := srcCfg.AuthInfos[srcCtx.AuthInfo]
	if !ok {
		return "", fmt.Errorf("user %q not found", srcCtx.AuthInfo)
	}

	if srcCluster.Server == "" {
		return "", fmt.Errorf("cluster server is empty")
	}
	if kmc.Spec.Ingress != nil {
		srcCluster.Server = fmt.Sprintf("https://%s:%d", kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port)
	}
	if len(srcUser.ClientCertificateData) == 0 || len(srcUser.ClientKeyData) == 0 {
		return "", fmt.Errorf("client certificate/key data not found in kubeconfig")
	}

	clusterKey := fmt.Sprintf("%s-k0s", kmc.Name)
	userKey := fmt.Sprintf("%s-admin", kmc.Name)

	newConfig := kubeconfig.CreateBasic(srcCluster.Server, clusterKey, userKey, srcCluster.CertificateAuthorityData)

	newConfig.AuthInfos[userKey] = &clientcmdapi.AuthInfo{
		ClientCertificateData: srcUser.ClientCertificateData,
		ClientKeyData:         srcUser.ClientKeyData,
	}

	configBytes, err := clientcmd.Write(*newConfig)
	if err != nil {
		return "", fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	return string(configBytes), nil
}

// kubeconfigStillValid reports whether the secret already holds a usable admin kubeconfig:
// the client certificate is well above the renewal threshold and the server URL matches
// the desired one when an Ingress is configured. Returning true lets the caller skip the
// pod exec and SSA write, which is what prevents the reconcile-loop storm.
func kubeconfigStillValid(secret *v1.Secret, kmc *km.Cluster) bool {
	raw, ok := secret.Data["value"]
	if !ok || len(raw) == 0 {
		return false
	}
	obj, err := k8sruntime.Decode(clientcmdlatest.Codec, raw)
	if err != nil {
		return false
	}
	cfg, ok := obj.(*clientcmdapi.Config)
	if !ok || cfg.CurrentContext == "" {
		return false
	}
	ctxEntry, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return false
	}
	cluster, ok := cfg.Clusters[ctxEntry.Cluster]
	if !ok {
		return false
	}
	if kmc.Spec.Ingress != nil {
		expectedServer := fmt.Sprintf("https://%s:%d", kmc.Spec.Ingress.APIHost, kmc.Spec.Ingress.Port)
		if cluster.Server != expectedServer {
			return false
		}
	}
	user, ok := cfg.AuthInfos[ctxEntry.AuthInfo]
	if !ok || len(user.ClientCertificateData) == 0 {
		return false
	}
	block, _ := pem.Decode(user.ClientCertificateData)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return time.Until(cert.NotAfter) > kubeconfigRenewalThreshold
}
