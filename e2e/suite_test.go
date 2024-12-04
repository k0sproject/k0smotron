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
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"

	controlplanev1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"k8s.io/klog/v2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/bootstrap"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	ctrl "sigs.k8s.io/controller-runtime"
	kind "sigs.k8s.io/kind/pkg/cluster"
	kindnodeutils "sigs.k8s.io/kind/pkg/cluster/nodeutils"
)

var (
	ctx = ctrl.SetupSignalHandler()

	// k0smotronTarPath is the path to the k0smotron tar archive.
	k0smotronTarPath string

	// clusterctlConfig is the file which tests will use as a clusterctl config.
	clusterctlConfig string

	// managementClusterProxy allows to interact with the management cluster to be used for the e2e tests.
	managementClusterProxy capiframework.ClusterProxy
)

func init() {
	flag.StringVar(&k0smotronTarPath, "e2e.k0smotron-tar", "", "path to the k0smotron tarball.")
	flag.StringVar(&clusterctlConfig, "e2e.clusterctl-config", "", "file which tests will use as a clusterctl config.")
}

type synchronizedBeforeTestSuiteConfig struct {
	KubeconfigPath string `json:"kubeconfigPath,omitempty"`
}

func TestE2E(t *testing.T) {
	ctrl.SetLogger(klog.Background())

	RegisterFailHandler(Fail)

	RunSpecs(t, "K0smotron e2e specs")
}

var _ = SynchronizedBeforeSuite(
	runSingletonSetup,
	runBeforeEachParallelNode,
)

func runSingletonSetup() []byte {
	e2eDataFolder := os.TempDir()
	managementClusterName := "e2e-management-cluster"

	Expect(k0smotronTarPath).To(BeAnExistingFile(), "K0smotron development image needs to be created before run e2e test suite.")
	Expect(clusterctlConfig).To(BeAnExistingFile(), "clusterctl config file needs to be created before run e2e test suite.")

	By("Creating a Kind management cluster")
	managementCluster := bootstrap.CreateKindBootstrapClusterAndLoadImages(ctx, bootstrap.CreateKindBootstrapClusterAndLoadImagesInput{
		Name:               managementClusterName,
		KubernetesVersion:  "v1.31.0",
		RequiresDockerSock: true,
		IPFamily:           "dual",
		LogFolder:          filepath.Join(e2eDataFolder, "kind"),
	})
	Expect(managementCluster).ToNot(BeNil(), "Failed to create a management cluster")

	kubeconfigPath := managementCluster.GetKubeconfigPath()
	Expect(kubeconfigPath).To(BeAnExistingFile(), "Failed to get the kubeconfig file for the management cluster")

	scheme := initScheme()

	managementClusterProxy = capiframework.NewClusterProxy("bootstrap", kubeconfigPath, scheme)
	Expect(managementClusterProxy).ToNot(BeNil(), "Failed to get a management cluster proxy")

	By("Loading k0smotron development images into management cluster")
	loadK0smotronDevImageIntoManagementCluster(managementClusterName, k0smotronTarPath)

	By("Installing Cluster API core components")

	clusterctl.InitManagementClusterAndWatchControllerLogs(ctx, clusterctl.InitManagementClusterAndWatchControllerLogsInput{
		ClusterProxy:            managementClusterProxy,
		ClusterctlConfigPath:    clusterctlConfig,
		InfrastructureProviders: []string{"docker"},
		BootstrapProviders:      []string{"k0sproject-k0smotron"},
		ControlPlaneProviders:   []string{"k0sproject-k0smotron"},
		LogFolder:               filepath.Join(e2eDataFolder, "capi"),
	})

	setupConfig := synchronizedBeforeTestSuiteConfig{
		KubeconfigPath: managementCluster.GetKubeconfigPath(),
	}

	data, err := yaml.Marshal(setupConfig)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func runBeforeEachParallelNode(data []byte) {
	conf := &synchronizedBeforeTestSuiteConfig{}
	err := yaml.UnmarshalStrict(data, conf)
	Expect(err).NotTo(HaveOccurred())

	// one cluster proxy is instantiated for each node running in parallel
	managementClusterProxy = capiframework.NewClusterProxy("bootstrap", conf.KubeconfigPath, initScheme(), capiframework.WithMachineLogCollector(capiframework.DockerLogCollector{}))
}

func loadK0smotronDevImageIntoManagementCluster(clusterName string, devImage string) {
	provider := kind.NewProvider()
	nodeList, err := provider.ListInternalNodes(clusterName)
	Expect(err).NotTo(HaveOccurred())

	for _, node := range nodeList {
		f, err := os.Open(filepath.Clean(devImage))
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()

		err = kindnodeutils.LoadImageArchive(node, f)
		Expect(err).NotTo(HaveOccurred())
	}
}

func initScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	capiframework.TryAddDefaultSchemes(s)
	err := controlplanev1beta1.AddToScheme(s)
	Expect(err).NotTo(HaveOccurred())

	return s
}
