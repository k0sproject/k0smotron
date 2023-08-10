/*


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

package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	kutil "github.com/k0sproject/k0smotron/internal/util"
)

type ControlPlaneController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

const joinTokenFilePath = "/etc/k0s.token"

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete

func (c *ControlPlaneController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("K0sControllerConfig", req.NamespacedName)
	log.Info("Reconciling K0sControllerConfig")

	// Lookup the config object
	config := &bootstrapv1.K0sControllerConfig{}
	if err := c.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("K0sControllerConfig not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get config")
		return ctrl.Result{}, err
	}

	if config.Status.Ready {
		return ctrl.Result{}, nil
	}

	// Look up the owner of this config if there is one
	configOwner, err := bsutil.GetConfigOwner(ctx, c.Client, config)
	if apierrors.IsNotFound(errors.Cause(err)) {
		// Could not find the owner yet, this is not an error and will rereconcile when the owner gets set.
		log.Info("Owner not found yet, waiting until it is set")
		return ctrl.Result{}, nil
	}
	if err != nil {
		log.Error(err, "Failed to get owner")
		return ctrl.Result{}, err
	}
	if configOwner == nil {
		log.Info("Owner is nil, waiting until it is set")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	// Lookup the cluster the config owner is associated with
	cluster, err := capiutil.GetClusterByName(ctx, c.Client, configOwner.GetNamespace(), configOwner.ClusterName())
	if err != nil {
		if errors.Cause(err) == capiutil.ErrNoCluster {
			log.Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(errors.Cause(err)) {
			log.Info("Cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Could not get cluster with metadata")
		return ctrl.Result{}, err
	}

	if annotations.IsPaused(cluster, config) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	scope := &Scope{
		ConfigOwner: configOwner,
		Cluster:     cluster,
	}

	// TODO Check if the secret is already present etc. to bail out early

	log.Info("Creating bootstrap data")
	var (
		files      []cloudinit.File
		installCmd string
	)

	if config.Spec.K0s != nil {
		//config.Spec.K0s.SetUnstructuredContent(map["spec"]interface{}{})
		err = unstructured.SetNestedField(config.Spec.K0s.Object, scope.Cluster.Spec.ControlPlaneEndpoint.Host, "spec", "api", "externalAddress")
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error setting control plane endpoint: %v", err)
		}

		k0sConfigBytes, err := config.Spec.K0s.MarshalJSON()
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error marshalling k0s config: %v", err)
		}
		files = append(files, cloudinit.File{
			Path:        "/etc/k0s.yaml",
			Permissions: "0644",
			Content:     string(k0sConfigBytes),
		})
		config.Spec.Args = append(config.Spec.Args, "--config", "/etc/k0s.yaml")
	}

	if scope.Cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return ctrl.Result{}, fmt.Errorf("control plane endpoint is not set")
	}

	if strings.HasSuffix(config.Name, "-0") {
		files, err = c.genInitialControlPlaneFiles(ctx, scope, files)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error generating initial control plane files: %v", err)
		}
		installCmd = createCPInstallCmd(config)
	} else {
		files, err = c.genControlPlaneJoinFiles(ctx, scope, config, files)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error generating control plane join files: %v", err)
		}
		installCmd = createCPInstallCmdWithJoinToken(config, joinTokenFilePath)
	}
	files = append(files, config.Spec.Files...)

	downloadCommands := createCPDownloadCommands(config)

	commands := config.Spec.PreStartCommands
	commands = append(commands, downloadCommands...)
	commands = append(commands, installCmd, "k0s start")
	commands = append(commands, config.Spec.PostStartCommands...)
	// Create the sentinel file as the last step so we know all previous _stuff_ has completed
	// https://cluster-api.sigs.k8s.io/developer/providers/bootstrap.html#sentinel-file
	commands = append(commands, "mkdir -p /run/cluster-api && touch /run/cluster-api/bootstrap-success.complete")

	ci := &cloudinit.CloudInit{
		Files:   files,
		RunCmds: commands,
	}

	// Create the bootstrap data
	bootstrapData, err := ci.AsBytes()
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create the secret containing the bootstrap data
	bootstrapSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: scope.Cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: bootstrapv1.GroupVersion.String(),
					Kind:       "K0sControllerConfig",
					Name:       config.Name,
					UID:        config.UID,
					Controller: pointer.Bool(true),
				},
			},
		},
		Data: map[string][]byte{
			"value": bootstrapData,
		},
		Type: clusterv1.ClusterSecretType,
	}

	if err := c.Client.Patch(ctx, bootstrapSecret, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"}); err != nil {
		log.Error(err, "Failed to patch bootstrap secret")
		return ctrl.Result{}, err
	}

	log.Info("Bootstrap secret created", "secret", bootstrapSecret.Name)

	// Set the status to ready
	config.Status.Ready = true
	config.Status.DataSecretName = pointer.String(bootstrapSecret.Name)
	if err := c.Status().Update(ctx, config); err != nil {
		log.Error(err, "Failed to patch config status")
		return ctrl.Result{}, err
	}

	log.Info("Reconciled succesfully")

	return ctrl.Result{}, nil
}

func (c *ControlPlaneController) genInitialControlPlaneFiles(ctx context.Context, scope *Scope, files []cloudinit.File) ([]cloudinit.File, error) {
	log := log.FromContext(ctx).WithValues("K0sControllerConfig cluster", scope.Cluster.Name)

	certs, _, err := c.getCerts(ctx, scope)
	if err != nil {
		log.Error(err, "Failed to get certs")
		return nil, err
	}
	files = append(files, certs...)

	return files, nil
}

func (c *ControlPlaneController) genControlPlaneJoinFiles(ctx context.Context, scope *Scope, config *bootstrapv1.K0sControllerConfig, files []cloudinit.File) ([]cloudinit.File, error) {
	log := log.FromContext(ctx).WithValues("K0sControllerConfig cluster", scope.Cluster.Name)

	_, ca, err := c.getCerts(ctx, scope)
	if err != nil {
		log.Error(err, "Failed to create certs")
		return nil, err
	}

	// Create the token using the child cluster client
	tokenID := kutil.RandomString(6)
	tokenSecret := kutil.RandomString(16)
	token := fmt.Sprintf("%s.%s", tokenID, tokenSecret)
	tokenKubeSecret := createTokenSecret(tokenID, tokenSecret)

	chCS, err := remote.NewClusterClient(ctx, "k0smotron", c.Client, util.ObjectKey(scope.Cluster))
	if err != nil {
		log.Error(err, "Failed to getting child cluster client set")
		return nil, err
	}

	err = chCS.Create(ctx, tokenKubeSecret)
	if err != nil {
		log.Error(err, "Failed to create token secret in the child cluster")
		return nil, err
	}

	host, err := c.findFirstControllerIP(ctx, config)
	if err != nil {
		log.Error(err, "Failed to get controller IP")
		return nil, err
	}

	// TODO: fix hardcoded port
	port := "9443"
	joinToken, err := kutil.CreateK0sJoinToken(ca.KeyPair.Cert, token, fmt.Sprintf("https://%s:%s", host, port), "controller-bootstrap")

	files = append(files, cloudinit.File{
		Path:        joinTokenFilePath,
		Permissions: "0644",
		Content:     joinToken,
	})

	return files, err
}

func (c *ControlPlaneController) getCerts(ctx context.Context, scope *Scope) ([]cloudinit.File, *secret.Certificate, error) {
	var files []cloudinit.File
	certificates := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{
		CertificatesDir: "/var/lib/k0s/pki",
	})

	err := certificates.Lookup(ctx, c.Client, util.ObjectKey(scope.Cluster))
	if err != nil {
		return nil, nil, err
	}
	ca := certificates.GetByPurpose(secret.ClusterCA)
	for _, cert := range certificates.AsFiles() {
		files = append(files, cloudinit.File{
			Path:        cert.Path,
			Permissions: "0644",
			Content:     cert.Content,
		})
	}

	return files, ca, nil
}

func createTokenSecret(tokenID, tokenSecret string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", tokenID),
			Namespace: "kube-system",
		},
		Type: corev1.SecretTypeBootstrapToken,
		StringData: map[string]string{
			"token-id":     tokenID,
			"token-secret": tokenSecret,
			// TODO We need bit shorter time for the token
			"expiration":                     time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"description":                    "Controller bootstrap token generated by k0smotron",
			"usage-bootstrap-api-auth":       "true",
			"usage-bootstrap-authentication": "false",
			"usage-bootstrap-signing":        "false",
			"usage-controller-join":          "true",
		},
	}
}

func (c *ControlPlaneController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.K0sControllerConfig{}).
		Complete(c)
}

func createCPDownloadCommands(config *bootstrapv1.K0sControllerConfig) []string {
	if config.Spec.PreInstalledK0s {
		return nil
	}

	if config.Spec.DownloadURL != "" {
		return []string{
			fmt.Sprintf("curl -sSfL %s -o /usr/local/bin/k0s", config.Spec.DownloadURL),
			"chmod +x /usr/local/bin/k0s",
		}
	}

	// Figure out version to download if download URL is not set
	if config.Spec.Version != "" {
		return []string{fmt.Sprintf("curl -sSfL https://get.k0s.sh | K0S_VERSION=%s sh", config.Spec.Version)}
	}

	return []string{"curl -sSfL https://get.k0s.sh | sh"}
}

func createCPInstallCmd(config *bootstrapv1.K0sControllerConfig) string {
	installCmd := []string{
		"k0s install controller"}
	if config.Spec.Args != nil && len(config.Spec.Args) > 0 {
		installCmd = append(installCmd, config.Spec.Args...)
	}
	return strings.Join(installCmd, " ")
}

func createCPInstallCmdWithJoinToken(config *bootstrapv1.K0sControllerConfig, tokenPath string) string {
	installCmd := []string{
		"k0s install controller"}
	installCmd = append(installCmd, "--token-file", tokenPath)
	if config.Spec.Args != nil && len(config.Spec.Args) > 0 {
		installCmd = append(installCmd, config.Spec.Args...)
	}
	return strings.Join(installCmd, " ")
}

func (c *ControlPlaneController) findFirstControllerIP(ctx context.Context, config *bootstrapv1.K0sControllerConfig) (string, error) {
	// Dirty first controller name generation
	nameParts := strings.Split(config.Name, "-")
	nameParts[len(nameParts)-1] = "0"
	name := strings.Join(nameParts, "-")
	machineImpl, err := c.getMachineImplementation(ctx, name, config)
	if err != nil {
		return "", errors.Wrap(err, "error getting machine implementation")
	}
	addresses, found, err := unstructured.NestedSlice(machineImpl.UnstructuredContent(), "status", "addresses")
	if err != nil {
		return "", err
	}

	extAddr, intAddr := "", ""

	if found {
		for _, addr := range addresses {
			addrMap, _ := addr.(map[string]interface{})
			if addrMap["type"] == string(v1.NodeExternalIP) {
				extAddr = addrMap["address"].(string)
				break
			}
			if addrMap["type"] == string(v1.NodeInternalIP) {
				intAddr = addrMap["address"].(string)
				break
			}
		}
	}

	if extAddr != "" {
		return extAddr, nil
	}

	if intAddr != "" {
		return intAddr, nil
	}

	return "", fmt.Errorf("no address found for machine %s", name)
}

func (c *ControlPlaneController) getMachineImplementation(ctx context.Context, name string, config *bootstrapv1.K0sControllerConfig) (*unstructured.Unstructured, error) {
	var machine clusterv1.Machine
	err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: config.Namespace}, &machine)
	if err != nil {
		return nil, errors.Wrap(err, "error getting machine object")
	}

	infRef := machine.Spec.InfrastructureRef

	machineImpl := new(unstructured.Unstructured)
	machineImpl.SetAPIVersion(infRef.APIVersion)
	machineImpl.SetKind(infRef.Kind)
	machineImpl.SetName(infRef.Name)

	key := client.ObjectKey{Name: infRef.Name, Namespace: infRef.Namespace}

	err = c.Get(ctx, key, machineImpl)
	if err != nil {
		return nil, errors.Wrap(err, "error getting machine implementation object")
	}
	return machineImpl, nil
}
