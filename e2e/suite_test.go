/*
Copyright 2024.

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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	controlplanev1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/bootstrap"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	ctx = ctrl.SetupSignalHandler()

	// watchesCtx is used in log streaming to be able to get canceld via cancelWatches after ending the test suite.
	watchesCtx, cancelWatches = context.WithCancel(ctx)

	// configPath is the path to the e2e config file.
	configPath string

	// e2eConfig to be used for this test, read from configPath.
	e2eConfig *clusterctl.E2EConfig

	// k0smotronTarPath is the path to the k0smotron tar archive.
	k0smotronTarPath string

	// clusterctlConfig is the file which tests will use as a clusterctl config.
	clusterctlConfig string

	// clusterctlConfigPath to be used for this test, created by generating a clusterctl local repository
	// with the providers specified in the configPath.
	clusterctlConfigPath string

	// artifactFolder is the folder to store e2e test artifacts.
	artifactFolder string

	// skipCleanup prevents cleanup of test resources e.g. for debug purposes.
	skipCleanup bool

	// managementClusterProvider manages provisioning of the bootstrap cluster to be used for the e2e tests.
	// Please note that provisioning will be skipped if e2e.use-existing-cluster is provided.
	managementClusterProvider bootstrap.ClusterProvider

	// managementClusterProxy allows to interact with the management cluster to be used for the e2e tests.
	managementClusterProxy capiframework.ClusterProxy
)

func init() {
	flag.StringVar(&configPath, "e2e.config", "", "path to the e2e config file")
	flag.StringVar(&k0smotronTarPath, "e2e.k0smotron-tar", "", "path to the k0smotron tarball.")
	flag.StringVar(&clusterctlConfig, "e2e.clusterctl-config", "", "file which tests will use as a clusterctl config.")
	flag.BoolVar(&skipCleanup, "e2e.skip-resource-cleanup", false, "if true, the resource cleanup after tests will be skipped")
	flag.StringVar(&artifactFolder, "e2e.artifacts-folder", "", "folder where e2e test artifact should be stored")
}

type synchronizedBeforeTestSuiteConfig struct {
	KubeconfigPath string `json:"kubeconfigPath,omitempty"`
}

func TestE2E(t *testing.T) {
	ctrl.SetLogger(klog.Background())

	RegisterFailHandler(Fail)

	// ensure the artifacts folder exists
	Expect(os.MkdirAll(artifactFolder, 0755)).To(Succeed(), "Invalid test suite argument. Can't create e2e.artifacts-folder %q", artifactFolder) //nolint:gosec

	RunSpecs(t, "K0smotron e2e specs")
}

var _ = SynchronizedBeforeSuite(
	runSingletonSetup,
	runBeforeEachParallelNode,
)

var _ = SynchronizedAfterSuite(func() {
	// After each ParallelNode.
}, func() {
	// After all ParallelNodes.

	By("Dumping logs from the bootstrap cluster")
	dumpBootstrapClusterLogs(managementClusterProxy)

	By("Tearing down the management cluster")
	if !skipCleanup {
		tearDown(managementClusterProvider, managementClusterProxy)
	}
})

func runSingletonSetup() []byte {

	Expect(configPath).To(BeAnExistingFile(), "clusterctl config file needs to be created before run e2e test suite.")

	By("Loading the e2e test configuration")
	e2eConfig = loadE2EConfig(ctx, configPath)

	if clusterctlConfig == "" {
		By("Creating a clusterctl local repository")
		clusterctlConfigPath = clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
			E2EConfig:        e2eConfig,
			RepositoryFolder: filepath.Join(artifactFolder, "repository"),
		})
		Expect(clusterctlConfigPath).To(BeAnExistingFile(), "The clusterctl config file does not exists in the local repository %s", artifactFolder)
	} else {
		By("Using existing clusterctl config")
		clusterctlConfigPath = clusterctlConfig
	}

	By("Creating a clusterctl local repository")
	clusterctlConfig := clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
		E2EConfig:        e2eConfig,
		RepositoryFolder: filepath.Join(artifactFolder, "repository"),
	})
	Expect(clusterctlConfig).To(BeAnExistingFile(), "The clusterctl config file does not exists in the local repository %s", artifactFolder)

	By("Creating a Kind management cluster")
	managementClusterProvider = bootstrap.CreateKindBootstrapClusterAndLoadImages(ctx, bootstrap.CreateKindBootstrapClusterAndLoadImagesInput{
		Name:               e2eConfig.ManagementClusterName,
		Images:             e2eConfig.Images,
		KubernetesVersion:  e2eConfig.GetVariable(KubernetesVersionManagement),
		RequiresDockerSock: e2eConfig.HasDockerProvider(),
		IPFamily:           e2eConfig.GetVariable(IPFamily),
		LogFolder:          filepath.Join(artifactFolder, "kind"),
	})
	Expect(managementClusterProvider).ToNot(BeNil(), "Failed to create a management cluster")

	kubeconfigPath := managementClusterProvider.GetKubeconfigPath()
	Expect(kubeconfigPath).To(BeAnExistingFile(), "Failed to get the kubeconfig file for the management cluster")

	scheme := initScheme()

	managementClusterProxy = capiframework.NewClusterProxy("bootstrap", kubeconfigPath, scheme)
	Expect(managementClusterProxy).ToNot(BeNil(), "Failed to get a management cluster proxy")

	By("Installing Cluster API core components")
	clusterctl.InitManagementClusterAndWatchControllerLogs(watchesCtx, clusterctl.InitManagementClusterAndWatchControllerLogsInput{
		ClusterProxy:            managementClusterProxy,
		ClusterctlConfigPath:    clusterctlConfigPath,
		InfrastructureProviders: e2eConfig.InfrastructureProviders(),
		BootstrapProviders:      []string{"k0sproject-k0smotron"},
		ControlPlaneProviders:   []string{"k0sproject-k0smotron"},
		LogFolder:               filepath.Join(artifactFolder, "capi"),
	})

	setupConfig := synchronizedBeforeTestSuiteConfig{
		KubeconfigPath: managementClusterProvider.GetKubeconfigPath(),
	}

	data, err := yaml.Marshal(setupConfig)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func runBeforeEachParallelNode(data []byte) {
	// only one node runs ATM, not specific setup needed for "each" node.
}

func initScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	capiframework.TryAddDefaultSchemes(s)
	err := controlplanev1beta1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	return s
}

func loadE2EConfig(ctx context.Context, configPath string) *clusterctl.E2EConfig {
	configData, err := os.ReadFile(configPath)
	Expect(err).ToNot(HaveOccurred(), "Failed to read the e2e test config file")
	Expect(configData).ToNot(BeEmpty(), "The e2e test config file should not be empty")

	config := &clusterctl.E2EConfig{}
	Expect(yaml.Unmarshal(configData, config)).To(Succeed(), "Failed to convert the e2e test config file to yaml")

	Expect(config.ResolveReleases(ctx)).To(Succeed(), "Failed to resolve release markers in e2e test config file")
	config.Defaults()
	config.AbsPaths(filepath.Dir(configPath))

	return config
}

func dumpBootstrapClusterLogs(bootstrapClusterProxy capiframework.ClusterProxy) {
	if bootstrapClusterProxy == nil {
		return
	}

	clusterLogCollector := bootstrapClusterProxy.GetLogCollector()
	if clusterLogCollector == nil {
		return
	}

	nodes, err := bootstrapClusterProxy.GetClientSet().CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to get nodes for the bootstrap cluster: %v\n", err)
		return
	}

	for i := range nodes.Items {
		nodeName := nodes.Items[i].GetName()
		err = clusterLogCollector.CollectMachineLog(
			ctx,
			bootstrapClusterProxy.GetClient(),
			// The bootstrap cluster is not expected to be a CAPI cluster, so in order to re-use the logCollector,
			// we create a fake machine that wraps the node.
			// NOTE: This assumes a naming convention between machines and nodes, which e.g. applies to the bootstrap clusters generated with kind.
			//       This might not work if you are using an existing bootstrap cluster provided by other means.
			&clusterv1.Machine{
				Spec:       clusterv1.MachineSpec{ClusterName: nodeName},
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			},
			filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName(), "machines", nodeName),
		)
		if err != nil {
			fmt.Printf("Failed to get logs for the bootstrap cluster node %s: %v\n", nodeName, err)
		}
	}
}

func tearDown(bootstrapClusterProvider bootstrap.ClusterProvider, bootstrapClusterProxy capiframework.ClusterProxy) {
	cancelWatches()
	if bootstrapClusterProxy != nil {
		bootstrapClusterProxy.Dispose(ctx)
	}
	if bootstrapClusterProvider != nil {
		bootstrapClusterProvider.Dispose(ctx)
	}
}
