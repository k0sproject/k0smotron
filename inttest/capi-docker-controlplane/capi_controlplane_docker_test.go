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

package capidocker

import (
	"context"
	"fmt"
	k0stestutil "github.com/k0sproject/k0s/inttest/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"testing"

	"github.com/k0sproject/k0smotron/inttest/util"
	"github.com/stretchr/testify/suite"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type CAPIDockerSuite struct {
	suite.Suite
	client           *kubernetes.Clientset
	restConfig       *rest.Config
	clusterYamlsPath string
	ctx              context.Context
}

func TestCAPIDockerSuite(t *testing.T) {
	s := CAPIDockerSuite{}
	suite.Run(t, &s)
}

func (s *CAPIDockerSuite) SetupSuite() {
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
	s.clusterYamlsPath = tmpDir + "/cluster.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYamlsPath, []byte(dockerClusterYaml), 0644))

	s.ctx, _ = util.NewSuiteContext(s.T())
}

func (s *CAPIDockerSuite) TestCAPIDocker() {

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

	// Wait for the cluster to be ready
	// Wait to see the CP pods ready
	//s.Require().NoError(common.WaitForStatefulSet(s.ctx, s.client, "kmc-docker-test", "default"))
	//
	//s.T().Log("Starting portforward")
	//fw, err := util.GetPortForwarder(s.restConfig, "kmc-docker-test-0", "default", 30443)
	//s.Require().NoError(err)
	//
	//go fw.Start(s.Require().NoError)
	//defer fw.Close()
	//
	//<-fw.ReadyChan
	//
	//localPort, err := fw.LocalPort()
	//s.Require().NoError(err)
	s.T().Log("waiting to see admin kubeconfig secret")
	kmcKC, err := GetKMCClientSet(s.ctx, s.client, "docker-test", "default")
	s.Require().NoError(err)

	s.T().Log("waiting for node to be ready")
	s.Require().NoError(k0stestutil.WaitForNodeReadyStatus(s.ctx, kmcKC, "docker-test-0", corev1.ConditionTrue))
	//node, err := kmcKC.CoreV1().Nodes().Get(s.ctx, "docker-test-worker-0", metav1.GetOptions{})
	//s.Require().NoError(err)
	//s.Require().Equal("v1.27.1+k0s", node.Status.NodeInfo.KubeletVersion)
	//fooLabel, ok := node.Labels["k0sproject.io/foo"]
	//s.Require().True(ok)
	//s.Require().Equal("bar", fooLabel)

	s.T().Log("verifying cloud-init extras")
	preStartFile, err := getDockerNodeFile("docker-test-worker-0", "/tmp/pre-start")
	s.Require().NoError(err)
	s.Require().Equal("pre-start", preStartFile)
	postStartFile, err := getDockerNodeFile("docker-test-worker-0", "/tmp/post-start")
	s.Require().NoError(err)
	s.Require().Equal("post-start", postStartFile)
	extraFile, err := getDockerNodeFile("docker-test-worker-0", "/tmp/test-file")
	s.Require().NoError(err)
	s.Require().Equal("test-file", extraFile)
}

func (s *CAPIDockerSuite) applyClusterObjects() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", s.clusterYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}

func (s *CAPIDockerSuite) deleteCluster() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "delete", "-f", s.clusterYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to delete cluster objects: %s", string(out))
}

func getDockerNodeFile(nodeName string, path string) (string, error) {
	output, err := exec.Command("docker", "exec", nodeName, "cat", path).Output()
	if err != nil {
		return "", fmt.Errorf("failed to get file %s from node %s: %w", path, nodeName, err)
	}

	return string(string(output)), nil
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

var dockerClusterYaml = `
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: docker-test
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    serviceDomain: cluster.local
    services:
      cidrBlocks:
      - 10.128.0.0/12
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    name: docker-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerCluster
    name: docker-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: docker-test-cp-template
  namespace: default
spec:
  template:
    spec: {}
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: docker-test
spec:
  replicas: 3
  k0sConfigSpec: {}
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerMachineTemplate
      name: docker-test-cp-template
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerCluster
metadata:
  name: docker-test
  namespace: default
spec:
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name:  docker-test-0
  namespace: default
spec:
  version: v1.27.1
  clusterName: docker-test
  bootstrap:
    configRef:
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: docker-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerMachine
    name: docker-test-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: docker-test-0
  namespace: default
spec:
  # version is deliberately different to be able to verify we actually pick it up :)
  version: v1.27.1+k0s.0
  args:
    - --labels=k0sproject.io/foo=bar
  preStartCommands:
    - echo -n "pre-start" > /tmp/pre-start
  postStartCommands:
    - echo -n "post-start" > /tmp/post-start
  files:
    - path: /tmp/test-file
      content: test-file
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachine
metadata:
  name: docker-test-0
  namespace: default
spec:
`

//---
//apiVersion: cluster.x-k8s.io/v1beta1
//kind: Machine
//metadata:
//name:  docker-test-worker-0
//namespace: default
//spec:
//version: v1.27.1
//clusterName: docker-test
//bootstrap:
//configRef:
//apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
//kind: K0sWorkerConfig
//name: docker-test-worker-0
//infrastructureRef:
//apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
//kind: DockerMachine
//name: docker-test-worker-0
//---
//apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
//kind: K0sWorkerConfig
//metadata:
//name: docker-test-worker-0
//namespace: default
//spec:
//# version is deliberately different to be able to verify we actually pick it up :)
//version: v1.27.1+k0s.0
//args:
//- --labels=k0sproject.io/foo=bar
//preStartCommands:
//- echo -n "pre-start" > /tmp/pre-start
//postStartCommands:
//- echo -n "post-start" > /tmp/post-start
//files:
//- path: /tmp/test-file
//content: test-file
//---
//apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
//kind: DockerMachine
//metadata:
//name: docker-test-worker-0
//namespace: default
//spec:
