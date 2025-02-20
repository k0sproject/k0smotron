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

package monitoring

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/k0smotron/k0smotron/inttest/util"
)

type MonitoringSuite struct {
	common.FootlooseSuite
}

func (s *MonitoringSuite) TestK0sGetsUp() {
	s.T().Log("starting k0s")
	s.PutFile(s.ControllerNode(0), "/tmp/k0s.yaml", k0sConfig)
	s.Require().NoError(s.InitController(0, "--disable-components=metrics-server --config=/tmp/k0s.yaml"))
	s.Require().NoError(s.RunWorkers())

	kc, err := s.KubeClient(s.ControllerNode(0))
	s.Require().NoError(err)
	rc, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err)

	err = s.WaitForNodeReady(s.WorkerNode(0), kc)
	s.NoError(err)

	s.Require().NoError(s.ImportK0smotronImages(s.Context()))

	s.T().Log("deploying k0smotron operator")
	s.Require().NoError(util.InstallK0smotronOperator(s.Context(), kc, rc))
	s.Require().NoError(common.WaitForDeployment(s.Context(), kc, "k0smotron-controller-manager", "k0smotron"))

	s.T().Log("deploying k0smotron cluster")
	s.createK0smotronCluster(s.Context(), kc)
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-kmc-test", "kmc-test"))

	s.T().Log("Generating k0smotron join token")
	token, err := util.GetJoinToken(kc, rc, "kmc-kmc-test-0", "kmc-test")
	s.Require().NoError(err)

	s.T().Log("joining worker to k0smotron cluster")
	s.Require().NoError(s.RunWithToken(s.K0smotronNode(0), token))

	s.Require().NoError(common.WaitForDeployment(s.Context(), kc, "prometheus-server", "default"))

	s.T().Log("Starting portforward")
	fw, err := util.GetPortForwarder(rc, "kmc-kmc-test-0", "kmc-test", 30443)
	s.Require().NoError(err)

	go fw.Start(s.Require().NoError)
	defer fw.Close()

	<-fw.ReadyChan

	localPort, err := fw.LocalPort()
	s.Require().NoError(err)
	s.T().Log("waiting for node to be ready")

	kmcKC, err := util.GetKMCClientSet(s.Context(), kc, "kmc-test", "kmc-test", localPort)
	s.Require().NoError(err)

	s.Require().NoError(s.WaitForNodeReady(s.K0smotronNode(0), kmcKC))

	err = wait.PollUntilContextCancel(s.Context(), time.Second, true, func(_ context.Context) (done bool, err error) {
		b, err := kc.RESTClient().
			Get().
			AbsPath("/api/v1/namespaces/default/services/prometheus-server:http/proxy/api/v1/query").
			Param("query", "process_open_fds").
			DoRaw(s.Context())
		if err != nil {
			return true, err
		}

		out := string(b)

		return strings.Contains(out, `k0smotron_etcd_metrics`), nil
	})
	s.Require().NoError(err)
}

func TestMonitoringSuite(t *testing.T) {
	s := MonitoringSuite{
		common.FootlooseSuite{
			ControllerCount:                 1,
			WorkerCount:                     1,
			K0smotronWorkerCount:            1,
			K0smotronImageBundleMountPoints: []string{"/dist/bundle.tar"},
		},
	}
	suite.Run(t, &s)
}

func (s *MonitoringSuite) createK0smotronCluster(ctx context.Context, kc *kubernetes.Clientset) {
	// create K0smotron namespace
	_, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kmc-test",
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)

	kmc := []byte(`
	{
		"apiVersion": "k0smotron.io/v1beta1",
		"kind": "Cluster",
		"metadata": {
		  "name": "kmc-test",
		  "namespace": "kmc-test"
		},
		"spec": {
   			"version": "v1.31.5-k0s.0",
			"monitoring": {
				"enabled": true
			},
			"service":{
				"type": "NodePort"
			}
		}
	  }
`)

	res := kc.RESTClient().Post().AbsPath("/apis/k0smotron.io/v1beta1/namespaces/kmc-test/clusters").Body(kmc).Do(ctx)
	s.Require().NoError(res.Error())
}

const k0sConfig = `
spec:
    telemetry:
      enabled: false
    extensions:
        helm:
          repositories:
          - name: prometheus-community
            url: https://prometheus-community.github.io/helm-charts
          charts:
          - name: prometheus
            chartname: prometheus-community/prometheus
            version: "22.6.3"
            values: |
              server:
                persistentVolume:
                  enabled: false
              kube-state-metrics:
                enabled: false
              prometheus-node-exporter:
                enabled: false
              prometheus-pushgateway:
                enabled: false
              alertmanager:
                enabled: false
            namespace: default
`
