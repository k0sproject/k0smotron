package controlplane

import (
	"context"
	"fmt"
	"github.com/imdario/mergo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	k0smoutil "github.com/k0sproject/k0smotron/internal/controller/util"
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

func (c *K0sController) generateKubeconfig(ctx context.Context, cluster *clusterv1.Cluster, endpoint string) (*api.Config, error) {
	clusterName := util.ObjectKey(cluster)
	clusterCA, err := secret.GetFromNamespacedName(ctx, c.Client, clusterName, secret.ClusterCA)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, kubeconfig.ErrDependentCertificateNotFound
		}
		return nil, err
	}

	cert, err := certs.DecodeCertPEM(clusterCA.Data[secret.TLSCrtDataName])
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA Cert: %w", err)
	} else if cert == nil {
		return nil, fmt.Errorf("certificate not found in config: %w", err)
	}

	key, err := certs.DecodePrivateKeyPEM(clusterCA.Data[secret.TLSKeyDataName])
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	} else if key == nil {
		return nil, fmt.Errorf("CA private key not found: %w", err)
	}

	cfg, err := kubeconfig.New(clusterName.Name, endpoint, cert, key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate a kubeconfig: %w", err)
	}

	return cfg, nil

}

func (c *K0sController) createKubeconfigSecret(ctx context.Context, cfg *api.Config, cluster *clusterv1.Cluster, secretName string) error {
	cfgBytes, err := clientcmd.Write(*cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config to yaml: %w", err)
	}

	clusterName := util.ObjectKey(cluster)
	owner := metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}

	kcSecret := kubeconfig.GenerateSecretWithOwner(clusterName, cfgBytes, owner)
	kcSecret.Name = secretName

	return c.Create(ctx, kcSecret)
}

func (c *K0sController) getKubeClient(ctx context.Context, cluster *clusterv1.Cluster) (*kubernetes.Clientset, error) {
	return k0smoutil.GetKubeClient(ctx, c.Client, cluster)
}

func enrichK0sConfigWithClusterData(cluster *clusterv1.Cluster, k0sConfig *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if cluster.Spec.ClusterNetwork == nil {
		return k0sConfig, nil
	}

	clusterNetworkValues := make(map[string]interface{})
	if cluster.Spec.ClusterNetwork.Pods != nil {
		clusterNetworkValues["podCIDR"] = cluster.Spec.ClusterNetwork.Pods.String()
	}
	if cluster.Spec.ClusterNetwork.Services != nil {
		clusterNetworkValues["serviceCIDR"] = cluster.Spec.ClusterNetwork.Services.String()
	}
	if cluster.Spec.ClusterNetwork.ServiceDomain != "" {
		clusterNetworkValues["clusterDomain"] = cluster.Spec.ClusterNetwork.ServiceDomain
	}

	if k0sConfig == nil {
		k0sConfig = &unstructured.Unstructured{}
	}

	clusterValues := map[string]interface{}{
		"apiVersion": "k0s.k0sproject.io/v1beta1",
		"kind":       "ClusterConfig",
		"spec": map[string]interface{}{
			"network": clusterNetworkValues,
		},
	}

	err := mergo.Merge(&k0sConfig.Object, clusterValues)
	return k0sConfig, err
}
