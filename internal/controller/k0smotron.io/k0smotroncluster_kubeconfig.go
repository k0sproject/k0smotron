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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	kubeconfig "k8s.io/kubernetes/cmd/kubeadm/app/util/kubeconfig"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/exec"
)

func (scope *kmcScope) reconcileKubeConfigSecret(ctx context.Context, managementClusterClient client.Client, kmc *km.Cluster) error {
	logger := log.FromContext(ctx)
	pod, err := findStatefulSetPod(ctx, kmc.GetStatefulSetName(), kmc.Namespace, scope.clienSet)

	if err != nil {
		return err
	}

	output, err := exec.PodExecCmdOutput(ctx, scope.clienSet, scope.restConfig, pod.Name, kmc.Namespace, "k0s kubeconfig create admin --groups system:masters")
	if err != nil {
		return err
	}

	// Post-process: build a kubeconfig with desired names based on current-context
	processedOutput, err := rewriteKubeconfigValues(output, kmc)
	if err != nil {
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
			Labels:      kcontrollerutil.LabelsForK0smotronCluster(kmc),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		StringData: map[string]string{"value": processedOutput},
		Type:       clusterv2.ClusterSecretType,
	}

	// workload cluster kubeconfig is always created in the management cluster so we set k0smotron cluster as owner
	_ = ctrl.SetControllerReference(kmc, &secret, scope.client.Scheme())

	return managementClusterClient.Patch(ctx, &secret, client.Apply, patchOpts...)
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
