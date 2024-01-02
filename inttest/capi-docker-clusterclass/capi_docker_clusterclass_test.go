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

package capidockerclusterclass

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/inttest/util"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type CAPIDockerClusterClassSuite struct {
	suite.Suite
	client                *kubernetes.Clientset
	restConfig            *rest.Config
	clusterYamlsPath      string
	clusterClassYamlsPath string
	ctx                   context.Context
}

func TestCAPIDockerClusterClassSuite(t *testing.T) {
	s := CAPIDockerClusterClassSuite{}
	suite.Run(t, &s)
}

func (s *CAPIDockerClusterClassSuite) SetupSuite() {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	s.Require().NotEmpty(kubeConfigPath, "KUBECONFIG env var must be set and point to kind cluster")
	// Get kube client from kubeconfig
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	s.Require().NoError(err)
	s.Require().NotNil(restCfg)
	s.restConfig = restCfg

	// Get kube client from kubeconfig
	kubeClient, err := kubernetes.NewForConfig(restCfg)
	s.Require().NoError(err)
	s.Require().NotNil(kubeClient)
	s.client = kubeClient

	tmpDir := s.T().TempDir()
	s.clusterClassYamlsPath = tmpDir + "/cluster-class.yaml"
	s.Require().NoError(os.WriteFile(s.clusterClassYamlsPath, []byte(clusterClassYaml), 0644))
	s.clusterYamlsPath = tmpDir + "/cluster.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYamlsPath, []byte(clusterYaml), 0644))

	s.ctx, _ = util.NewSuiteContext(s.T())
}

func (s *CAPIDockerClusterClassSuite) TestCAPIDockerClusterClass() {

	// Apply the child cluster objects
	s.applyClusterObjects()
	defer func() {
		keep := os.Getenv("KEEP_AFTER_TEST")
		if keep == "true" {
			return
		}
		if keep == "on-failure" && s.T().Failed() {
			return
		}
		s.T().Log("Deleting cluster objects")
		s.deleteCluster()
	}()
	s.T().Log("cluster objects applied, waiting for cluster to be ready")

	var localPort int
	// nolint:staticcheck
	err := wait.PollImmediateUntilWithContext(s.ctx, 1*time.Second, func(ctx context.Context) (bool, error) {
		localPort, _ = getLBPort("docker-test-cluster-lb")
		return localPort > 0, nil
	})
	s.Require().NoError(err)

	s.T().Log("waiting to see admin kubeconfig secret")
	kmcKC, err := util.GetKMCClientSet(s.ctx, s.client, "docker-test-cluster", "default", localPort)
	s.Require().NoError(err)

	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(s.ctx, 1*time.Second, func(ctx context.Context) (bool, error) {
		b, _ := s.client.RESTClient().
			Get().
			AbsPath("/healthz").
			DoRaw(context.Background())

		return string(b) == "ok", nil
	})
	s.Require().NoError(err)

	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(s.ctx, 1*time.Second, func(ctx context.Context) (bool, error) {
		nodes, _ := kmcKC.CoreV1().Nodes().List(s.ctx, metav1.ListOptions{})
		return len(nodes.Items) == 2, nil
	})
	s.Require().NoError(err)

}

func (s *CAPIDockerClusterClassSuite) applyClusterObjects() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", s.clusterClassYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster class objects: %s", string(out))
	out, err = exec.Command("kubectl", "apply", "-f", s.clusterYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}

func (s *CAPIDockerClusterClassSuite) deleteCluster() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "delete", "-f", s.clusterYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to delete cluster objects: %s", string(out))
	out, err = exec.Command("kubectl", "delete", "-f", s.clusterClassYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to delete cluster class objects: %s", string(out))
}

func GetKMCClientSet(ctx context.Context, kc *kubernetes.Clientset, name string, namespace string) (*kubernetes.Clientset, error) {
	secretName := fmt.Sprintf("%s-kubeconfig", name)
	// Wait first to see the secret exists
	if err := util.WaitForSecret(ctx, kc, secretName, namespace); err != nil {
		return nil, err
	}
	kubeConf, err := kc.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	kmcCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConf.Data["value"]))
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(kmcCfg)
}

func getLBPort(name string) (int, error) {
	b, err := exec.Command("docker", "inspect", name, "--format", "{{json .NetworkSettings.Ports}}").Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get inspect info from container %s: %w", name, err)
	}

	var ports map[string][]map[string]string
	err = json.Unmarshal(b, &ports)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal inspect info from container %s: %w", name, err)
	}

	return strconv.Atoi(ports["6443/tcp"][0]["HostPort"])
}

var clusterYaml = `
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: docker-test-cluster
  namespace: default
spec:
  topology:
    class: k0smotron-clusterclass
    version: v1.27.2
    workers:
      machineDeployments:
      - class: docker-test-default-worker
        name: md
        replicas: 1
`

var clusterClassYaml = `
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerClusterTemplate
metadata:
  name: k0smotron-docker-cluster-tmpl
spec:
  template:
    spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: docker-test-machine-template
  namespace: default
spec:
  template:
    spec: {}
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlaneTemplate
metadata:
  name: docker-test
  namespace: default
spec:
  template:
    spec:
      k0sVersion: v1.27.2+k0s.0
      k0sConfigSpec:
        k0s:
          apiVersion: k0s.k0sproject.io/v1beta1
          kind: ClusterConfig
          metadata:
            name: k0s
          spec:
            api:
              extraArgs:
                anonymous-auth: "true"
            telemetry:
              enabled: false
        args:
        - --enable-worker
        - --no-taints
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: docker-test-worker-template
  namespace: default
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
spec:
  controlPlane:
    ref:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta1
      kind: K0sControlPlaneTemplate
      name: docker-test
      namespace: default
    machineInfrastructure:
      ref:
        kind: DockerMachineTemplate
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        name: docker-test-machine-template
        namespace: default
  infrastructure:
    ref:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerClusterTemplate
      name: k0smotron-docker-cluster-tmpl
      namespace: default
  workers:
    machineDeployments:
    - class: docker-test-default-worker
      template:
        bootstrap:
          ref:
            apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
            kind: K0sWorkerConfigTemplate
            name: docker-test-worker-template
            namespace: default
        infrastructure:
          ref:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
            kind: DockerMachineTemplate
            name: docker-test-machine-template
            namespace: default
`
