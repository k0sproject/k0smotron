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

package capidockermachinetemplateupdaterecreatekine

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	k0stestutil "github.com/k0sproject/k0s/inttest/common"
	"github.com/k0sproject/k0smotron/inttest/util"
)

type CAPIDockerMachineTemplateUpdateRecreateKine struct {
	//type CAPIDockerMachineTemplateUpdateRecreateKine struct {
	suite.Suite
	client                 *kubernetes.Clientset
	restConfig             *rest.Config
	clusterYamlsPath       string
	clusterYamlsUpdatePath string
	ctx                    context.Context
}

func TestCAPIDockerMachineTemplateUpdateRecreateKine(t *testing.T) {
	s := CAPIDockerMachineTemplateUpdateRecreateKine{}
	suite.Run(t, &s)
}

func (s *CAPIDockerMachineTemplateUpdateRecreateKine) SetupSuite() {
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

	postgressAddress, err := getPostgresAddress()
	s.Require().NoError(err)

	tmpDir := s.T().TempDir()
	s.clusterYamlsPath = tmpDir + "/cluster.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYamlsPath, []byte(fmt.Sprintf(dockerClusterYaml, postgressAddress)), 0644))
	s.clusterYamlsUpdatePath = tmpDir + "/update.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYamlsUpdatePath, []byte(fmt.Sprintf(controlPlaneUpdate, postgressAddress)), 0644))

	s.ctx, _ = util.NewSuiteContext(s.T())
}

func (s *CAPIDockerMachineTemplateUpdateRecreateKine) TestCAPIControlPlaneDockerDownScaling() {

	s.installPostgres()

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
		s.Require().NoError(util.DeleteCluster("docker-test-cluster"))
	}()
	s.T().Log("cluster objects applied, waiting for cluster to be ready")

	var localPort int
	err := wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		localPort, _ = getLBPort("docker-test-cluster-lb")
		return localPort > 0, nil
	})
	s.Require().NoError(err)

	s.T().Log("waiting to see admin kubeconfig secret")
	kmcKC, err := util.GetKMCClientSet(s.ctx, s.client, "docker-test-cluster", "default", localPort)
	s.Require().NoError(err)

	err = wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		b, _ := s.client.RESTClient().
			Get().
			AbsPath("/healthz").
			DoRaw(context.Background())

		return string(b) == "ok", nil
	})
	s.Require().NoError(err)

	var nodeIDs []string
	err = wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		nodeIDs, err = util.GetControlPlaneNodesIDs("docker-test-")

		if err != nil {
			return false, nil
		}

		return len(nodeIDs) == 3, nil
	})
	s.Require().NoError(err)

	err = wait.PollUntilContextCancel(s.ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		machines, err := util.GetControlPlaneMachinesByKcpName(ctx, "docker-test", "default", s.client)
		if err != nil {
			return false, nil
		}

		if len(machines) != 3 {
			return false, nil
		}

		for _, m := range machines {
			output, err := exec.Command("docker", "exec", fmt.Sprintf("docker-test-cluster-%s", m.GetName()), "k0s", "status").Output()
			if err != nil {
				return false, nil
			}

			if !strings.Contains(string(output), "Version:") {
				return false, nil
			}

		}

		return true, nil
	})
	s.Require().NoError(err)

	s.T().Log("waiting for node to be ready")
	s.Require().NoError(k0stestutil.WaitForNodeReadyStatus(s.ctx, kmcKC, "docker-test-worker-0", corev1.ConditionTrue))

	s.T().Log("updating cluster objects")
	s.updateClusterObjects()

	err = wait.PollUntilContextCancel(s.ctx, 100*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		var obj unstructured.UnstructuredList
		err := s.client.RESTClient().
			Get().
			AbsPath("/apis/cluster.x-k8s.io/v1beta2/namespaces/default/machines").
			Do(s.ctx).
			Into(&obj)
		if err != nil {
			return false, nil
		}

		for _, item := range obj.Items {
			if strings.Contains(item.GetName(), "worker") {
				continue
			}

			v, _, err := unstructured.NestedString(item.Object, "spec", "version")
			if err != nil {
				return false, nil
			}
			if v != "v1.28.7+k0s.0" {
				return false, nil
			}
		}

		return true, nil
	})
	s.Require().NoError(err)
}

func (s *CAPIDockerMachineTemplateUpdateRecreateKine) installPostgres() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", "./postgresql.yaml").CombinedOutput()
	s.Require().NoError(err, "failed to apply postgres: %s", string(out))
}

func (s *CAPIDockerMachineTemplateUpdateRecreateKine) applyClusterObjects() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", s.clusterYamlsPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}

func (s *CAPIDockerMachineTemplateUpdateRecreateKine) updateClusterObjects() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", s.clusterYamlsUpdatePath).CombinedOutput()
	s.Require().NoError(err, "failed to update cluster objects: %s", string(out))
}

func getPostgresAddress() (string, error) {
	b, err := exec.Command("docker", "inspect", "kind-control-plane", "--format", "{{json .NetworkSettings.Networks.kind.IPAddress}}").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get inspect info from container kind-control-plane: %w", err)
	}

	re := regexp.MustCompile("[0-9.]+")
	address := re.FindString(string(b))

	return net.JoinHostPort(address, "32001"), nil
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

var dockerClusterYaml = `
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: docker-test-cluster
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
    apiGroup: controlplane.cluster.x-k8s.io
    kind: K0sControlPlane
    name: docker-test
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: DockerCluster
    name: docker-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: docker-test-cp-template
  namespace: default
spec:
  template:
    spec:
      customImage: kindest/node:v1.31.0
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: docker-test
spec:
  replicas: 3
  version: v1.27.1+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        storage:
          type: kine
          kine:
            datasource: postgres://postgres:postgres@%s/kine?sslmode=disable
        api:
          extraArgs:
            anonymous-auth: "true"
        telemetry:
          enabled: false
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DockerMachineTemplate
        name: docker-test-cp-template
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerCluster
metadata:
  name: docker-test
  namespace: default
spec:
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Machine
metadata:
  name:  docker-test-worker-0
  namespace: default
spec:
  version: v1.27.1
  clusterName: docker-test-cluster
  bootstrap:
    configRef:
      apiGroup: bootstrap.cluster.x-k8s.io
      kind: K0sWorkerConfig
      name: docker-test-worker-0
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: DockerMachine
    name: docker-test-worker-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfig
metadata:
  name: docker-test-worker-0
  namespace: default
spec:
  # version is deliberately different to be able to verify we actually pick it up :)
  version: v1.27.1+k0s.0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachine
metadata:
  name: docker-test-worker-0
  namespace: default
spec:
  customImage: kindest/node:v1.31.0
`

var controlPlaneUpdate = `
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: docker-test
spec:
  replicas: 3
  version: v1.28.7+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        storage:
          type: kine
          kine:
            datasource: postgres://postgres:postgres@%s/kine?sslmode=disable
        api:
          extraArgs:
            anonymous-auth: "true"
        telemetry:
          enabled: false
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DockerMachineTemplate
        name: docker-test-cp-template
`
