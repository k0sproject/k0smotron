//go:build e2e

/*
Copyright 2025.

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

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/k0sproject/k0smotron/e2e/mothership"
	"github.com/k0sproject/k0smotron/e2e/util"
	"github.com/k0sproject/k0smotron/e2e/util/poolprovisioner"
	dockerprovisioner "github.com/k0sproject/k0smotron/e2e/util/poolprovisioner/docker"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/bootstrap"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// Test suite constants for e2e config variables.
const (
	KubernetesVersion           = "KUBERNETES_VERSION"
	KubernetesVersionRmPool     = "KUBERNETES_VERSION_RMPOOL"
	KubernetesVersionManagement = "KUBERNETES_VERSION_MANAGEMENT"
	K0sVersion                  = "K0S_VERSION"
	K0sVersionFirstUpgradeTo    = "K0S_VERSION_FIRST_UPGRADE_TO"
	K0sVersionSecondUpgradeTo   = "K0S_VERSION_SECOND_UPGRADE_TO"
	ControlPlaneMachineCount    = "CONTROL_PLANE_MACHINE_COUNT"
	IPFamily                    = "IP_FAMILY"
	SSHPublicKey                = "SSH_PUBLIC_KEY"
	SSHKeyName                  = "SSH_KEY_NAME"
	PoolProvisioner             = "POOL_PROVISIONER"
)

var (
	ctx = ctrl.SetupSignalHandler()

	// watchesCtx is used in log streaming to be able to get canceled via cancelWatches after ending the test suite.
	watchesCtx, cancelWatches = context.WithCancel(ctx)

	// configPath is the path to the e2e config file.
	configPath string

	// e2eConfig to be used for this test, read from configPath.
	e2eConfig *clusterctl.E2EConfig

	// clusterctlConfig is the file which tests will use as a clusterctl config.
	clusterctlConfig string

	// useExistingCluster instructs the test to use the current cluster instead of creating a new one (default discovery rules apply).
	useExistingCluster bool

	// clusterctlConfigPath to be used for this test, created by generating a clusterctl local repository
	// with the providers specified in the configPath.
	clusterctlConfigPath string

	// artifactFolder is the folder to store e2e test artifacts.
	artifactFolder string

	// skipCleanup prevents cleanup of test resources e.g. for debug purposes.
	skipCleanup bool

	// managementClusterProvider manages provisioning of the bootstrap cluster to be used for the e2e tests.
	// Please note that provisioning will be skipped if e2e.use-existing-cluster is provided.
	bootstrapClusterProvider bootstrap.ClusterProvider

	// managementClusterProxy allows to interact with the management cluster to be used for the e2e tests.
	bootstrapClusterProxy capiframework.ClusterProxy

	// controlPlaneMachineCount is the number of control plane machines to create in the workload clusters.
	controlPlaneMachineCount int

	// workerMachineCount is the number of worker machines to create in the workload clusters.
	workerMachineCount int

	// infrastructureProvider is the infrastructure provider to use for the tests. Default is k0smotron.
	infrastructureProvider string
)

func init() {

	flag.StringVar(&configPath, "config", "", "path to the e2e config file")
	flag.BoolVar(&skipCleanup, "skip-resource-cleanup", false, "if true, the resource cleanup after tests will be skipped")
	flag.StringVar(&artifactFolder, "artifacts-folder", "", "folder where e2e test artifact should be stored")
	flag.BoolVar(&useExistingCluster, "use-existing-cluster", false, "if true, the test uses the current cluster instead of creating a new one (default discovery rules apply)")
	flag.IntVar(&controlPlaneMachineCount, "control-plane-machine-count", 3, "number of control plane machines")
	flag.IntVar(&workerMachineCount, "worker-machine-count", 1, "number of worker machines")
	flag.StringVar(&infrastructureProvider, "infrastructure-provider", "k0sproject-k0smotron", "infrastructure provider to use for the tests")

	// On the k0smotron side we avoid using Gomega for assertions but since we want to use the
	// cluster-api framework as much as possible, the framework assertions require registering
	// a fail handler beforehand.
	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})
}

func setupAndRun(t *testing.T, test func(t *testing.T)) {
	ctrl.SetLogger(klog.Background())
	flag.Parse()

	defer func() {
		if !skipCleanup {
			tearDown(bootstrapClusterProvider, bootstrapClusterProxy)
		}
	}()
	err := setup()
	if err != nil {
		panic(err)
	}

	test(t)
}

func setupPoolMachinesConfigEnv(ctx context.Context, replicas int, nodeVersion string, e2eConfig *clusterctl.E2EConfig) error {
	switch os.Getenv(PoolProvisioner) {
	case "docker", "":
		poolprovisioner.PoolProvisioner = &dockerprovisioner.Provisioner{}
	// TODO: add AWS as provisioner
	default:
		return fmt.Errorf("unknown pool provisioner: %s", os.Getenv(PoolProvisioner))
	}

	// Create keypair to allow SSH for k0s provisioning by the infrastructure controller.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate private key: %v", err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}
	privPEM := pem.EncodeToMemory(privBlock)
	privB64 := base64.StdEncoding.EncodeToString(privPEM)
	// Format used in the cluster templates
	e2eConfig.Variables["SSH_PRIVATE_KEY_BASE64"] = privB64
	// Marshal public key for authorized_keys
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal public key: %v", err)
	}
	pubAuthorized := ssh.MarshalAuthorizedKey(pub)

	err = poolprovisioner.PoolProvisioner.Provision(ctx, replicas, nodeVersion, pubAuthorized)
	if err != nil {
		return fmt.Errorf("provision pool machines: %w", err)
	}

	// Load Balancer is created only for Docker provisioner at the moment. Check type of the provisioner
	if _, ok := poolprovisioner.PoolProvisioner.(*dockerprovisioner.Provisioner); ok {
		e2eConfig.Variables["LOAD_BALANCER_ADDRESS"] = dockerprovisioner.GetLoadBalancerIPAddress()
	}

	for i, address := range poolprovisioner.PoolProvisioner.GetRemoteMachinesAddresses() {
		// Format: ADDRESS_0, ADDRESS_1, ... is used in the cluster templates
		e2eConfig.Variables[fmt.Sprintf("ADDRESS_%d", i+1)] = address
	}

	return nil
}

func setup() error {
	var err error
	e2eConfig, err = loadE2EConfig(ctx, configPath)
	if err != nil {
		return fmt.Errorf("failed to load e2e config: %w", err)
	}

	// Since we share the declaration of the e2e test configuration in the same file with that of the infrastructure providers,
	// we remove those that are not necessary for this test and thus avoid creating local clusterctl repositories with unnecessary providers.
	filteredNonUsedProviders := []clusterctl.ProviderConfig{}
	for _, provider := range e2eConfig.Providers {
		if provider.Type == string(clusterctlv1.InfrastructureProviderType) && provider.Name != infrastructureProvider {
			continue
		}
		filteredNonUsedProviders = append(filteredNonUsedProviders, provider)
	}
	e2eConfig.Providers = filteredNonUsedProviders

	// If k0smotron provider is used, we need to create the virtual machines beforehand.
	if hasK0smotronProvider(e2eConfig) {
		// We add one extra machine for the load balancer because when a rolling upgrade happens,
		// we need to have an extra machine to avoid downtime.
		replicas := controlPlaneMachineCount + workerMachineCount + 1
		// We create the pool machines and set the ADDRESS_X environment variables corresponding to
		// each machine IP address.
		err := setupPoolMachinesConfigEnv(ctx, replicas, e2eConfig.MustGetVariable(KubernetesVersionRmPool), e2eConfig)
		if err != nil {
			if poolprovisioner.PoolProvisioner != nil {
				cleanupErr := poolprovisioner.PoolProvisioner.Clean(ctx)
				if cleanupErr != nil {
					klog.Errorf("failed to clean up pool machines after setup failure: %v", cleanupErr)
				}
			}
			panic(fmt.Errorf("failed to setup pool machines: %w", err))
		}
	}

	if clusterctlConfig == "" {
		clusterctlConfigPath = clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
			E2EConfig:        e2eConfig,
			RepositoryFolder: filepath.Join(artifactFolder, "repository"),
		})
	} else {
		clusterctlConfigPath = clusterctlConfig
	}

	scheme, err := initScheme()
	if err != nil {
		return err
	}

	kubeconfigPath := ""
	if !useExistingCluster {
		bootstrapClusterProvider = bootstrap.CreateKindBootstrapClusterAndLoadImages(ctx, bootstrap.CreateKindBootstrapClusterAndLoadImagesInput{
			Name:               e2eConfig.ManagementClusterName,
			Images:             e2eConfig.Images,
			KubernetesVersion:  e2eConfig.MustGetVariable(KubernetesVersionManagement),
			RequiresDockerSock: e2eConfig.HasDockerProvider(),
			IPFamily:           e2eConfig.MustGetVariable(IPFamily),
			LogFolder:          filepath.Join(artifactFolder, "kind"),
			ExtraPortMappings: []v1alpha4.PortMapping{
				{
					ContainerPort: 32143, // haproxy ingress port
					HostPort:      32143,
				},
				{
					ContainerPort: 30443, // HCP api nodeport
					HostPort:      30443,
				},
			},
		})
		if bootstrapClusterProvider == nil {
			return errors.New("failed to create a management cluster")
		}
		kubeconfigPath = bootstrapClusterProvider.GetKubeconfigPath()
	} else {
		fmt.Println("Using an existing bootstrap cluster")
	}

	var opts []capiframework.Option
	switch infrastructureProvider {
	case "docker":
		opts = append(opts, framework.WithMachineLogCollector(framework.DockerLogCollector{}))
	case "k0sproject-k0smotron":
		// At the momento only docker provisioner is supported for k0smotron so we can safely cast here.
		dockerProvisioner := poolprovisioner.PoolProvisioner.(*dockerprovisioner.Provisioner)
		opts = append(opts, framework.WithMachineLogCollector(dockerprovisioner.RemoteMachineLogCollector{
			Provisioner: dockerProvisioner,
		}))
	}

	bootstrapClusterProxy = capiframework.NewClusterProxy("bootstrap", kubeconfigPath, scheme, opts...)
	if bootstrapClusterProxy == nil {
		return errors.New("failed to get a management cluster proxy")
	}

	err = mothership.InitAndWatchControllerLogs(watchesCtx, clusterctl.InitManagementClusterAndWatchControllerLogsInput{
		ClusterProxy:             bootstrapClusterProxy,
		ClusterctlConfigPath:     clusterctlConfigPath,
		InfrastructureProviders:  e2eConfig.InfrastructureProviders(),
		DisableMetricsCollection: true,
		BootstrapProviders:       []string{"k0sproject-k0smotron"},
		ControlPlaneProviders:    []string{"k0sproject-k0smotron"},
		LogFolder:                filepath.Join(artifactFolder, "capi"),
	}, util.GetInterval(e2eConfig, "bootstrap", "wait-deployment-available"))
	if err != nil {
		return fmt.Errorf("failed to init management cluster: %w", err)
	}

	return nil
}

func hasK0smotronProvider(c *clusterctl.E2EConfig) bool {
	for _, i := range c.InfrastructureProviders() {
		if i == "k0sproject-k0smotron" {
			return true
		}
	}
	return false
}

func tearDown(bootstrapClusterProvider bootstrap.ClusterProvider, bootstrapClusterProxy framework.ClusterProxy) {
	cancelWatches()
	if bootstrapClusterProxy != nil {
		bootstrapClusterProxy.Dispose(ctx)
	}
	if bootstrapClusterProvider != nil {
		bootstrapClusterProvider.Dispose(ctx)
	}
}

func initScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	capiframework.TryAddDefaultSchemes(s)
	err := cpv1beta1.AddToScheme(s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func loadE2EConfig(ctx context.Context, configPath string) (*clusterctl.E2EConfig, error) {
	configData, err := os.ReadFile(configPath)

	if err != nil {
		return nil, fmt.Errorf("failed to read the e2e test config file: %w", err)
	}

	config := &clusterctl.E2EConfig{}
	err = yaml.Unmarshal(configData, config)
	if err != nil {
		return nil, fmt.Errorf("failed to convert the e2e test config file to yaml: %w", err)
	}

	err = config.ResolveReleases(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve release markers in e2e test config file: %w", err)
	}

	config.Defaults()
	config.AbsPaths(filepath.Dir(configPath))

	return config, nil
}
