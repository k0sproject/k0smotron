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

package capidockermachinedeployment

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/inttest/util"
	"github.com/stretchr/testify/suite"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

type CAPIDockerSuite struct {
	suite.Suite
	client     *kubernetes.Clientset
	restConfig *rest.Config
	ctx        context.Context
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
		s.Require().NoError(util.DeleteCluster("docker-md-test"))
	}()
	s.T().Log("cluster objects applied, waiting for cluster to be ready")

	// Wait for the cluster to be ready
	// Wait to see the CP pods ready
	s.Require().NoError(util.WaitForStatefulSet(s.ctx, s.client, "kmc-docker-md-test", "default"))

	s.T().Log("Starting portforward")
	fw, err := util.GetPortForwarder(s.restConfig, "kmc-docker-md-test-0", "default", 30443)
	s.Require().NoError(err)

	go fw.Start(s.Require().NoError)
	defer fw.Close()

	<-fw.ReadyChan

	localPort, err := fw.LocalPort()
	s.Require().NoError(err)
	s.T().Log("waiting to see admin kubeconfig secret")
	s.Require().NoError(util.WaitForSecret(s.ctx, s.client, "docker-md-test-kubeconfig", "default"))
	kmcKC, err := util.GetKMCClientSet(s.ctx, s.client, "docker-md-test", "default", localPort)
	s.Require().NoError(err)

	s.T().Log("waiting for 2 nodes to be ready")
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	err = util.Poll(ctx, func(ctx context.Context) (done bool, err error) {
		nodes, err := kmcKC.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		readyCount := 0
		for _, node := range nodes.Items {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					if cond.Status == corev1.ConditionTrue {
						readyCount++
					}
					break
				}
			}
		}
		if readyCount != 2 {
			return false, nil
		}
		return true, nil
	})
	s.Require().NoError(err)
	// Check that the MD gets to ready state
	s.T().Log("waiting for machinedeployment to be ready")
	// nolint:staticcheck
	s.Require().NoError(wait.PollImmediateUntilWithContext(ctx, 1*time.Second, func(ctx context.Context) (done bool, err error) {
		// Get the MachineDeployment
		md := &clusterv1.MachineDeployment{}
		err = s.client.RESTClient().
			Get().
			AbsPath("/apis/cluster.x-k8s.io/v1beta1").
			Resource("machinedeployments").
			Namespace("default").
			Name("docker-md-test").
			Do(ctx).Into(md)
		if err != nil {
			return false, err
		}
		if ptr.Deref(md.Status.ReadyReplicas, 0) != 2 {
			return false, nil
		}
		return true, nil
	}))
}

func (s *CAPIDockerSuite) applyClusterObjects() {
	// Exec via kubectl
	out, err := exec.Command("kubectl", "apply", "-f", "../../config/samples/capi/docker/cluster-with-machinedeployment.yaml").CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}
