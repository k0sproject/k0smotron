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

package basic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type BasicSuite struct {
	common.FootlooseSuite
}

func (s *BasicSuite) TestK0sGetsUp() {
	s.logTS("starting k0s")
	s.Require().NoError(s.InitController(0, "--disable-components=konnectivity-server,metrics-server"))
	s.Require().NoError(s.RunWorkers())

	kc, err := s.KubeClient(s.ControllerNode(0))
	s.Require().NoError(err)

	err = s.WaitForNodeReady(s.WorkerNode(0), kc)
	s.NoError(err)

	s.Require().NoError(s.ImportK0smotronImages(s.Context()))

	s.logTS("deploying k0smotron operator")
	s.createFromYaml(s.Context(), os.Getenv("K0SMOTRON_INSTALL_YAML"), kc)
	s.Require().NoError(common.WaitForDeployment(s.Context(), kc, "k0smotron-controller-manager", "k0smotron"))

	s.logTS("deploying k0smotron cluster")
	s.createK0smotronCluster(s.Context(), kc)
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-kmc-test", "kmc-test"))

	s.logTS("Generating k0smotron join token")
	pod := s.getPod(s.Context(), kc)
	token := s.getJoinToken(kc, pod.Name)

	s.logTS("joining worker to k0smotron cluster")
	s.Require().NoError(s.RunWithToken(s.K0smotronNode(0), token))

	s.logTS("Starting portforward")
	cfg, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	fw := s.getPortForwarder(cfg, stopChan, readyChan, pod)

	go s.forwardPorts(fw)
	defer fw.Close()
	defer close(stopChan)

	s.logTS("waiting for portforward to be ready")
	<-readyChan

	s.logTS("waiting for node to be ready")
	kmcKC := s.getKMCClientSet(kc)
	s.Require().NoError(s.WaitForNodeReady(s.K0smotronNode(0), kmcKC))

}

func TestBasicSuite(t *testing.T) {
	s := BasicSuite{
		common.FootlooseSuite{
			ControllerCount:                 1,
			WorkerCount:                     1,
			K0smotronWorkerCount:            1,
			K0smotronImageBundleMountPoints: []string{"/dist/bundle.tar"},
		},
	}
	suite.Run(t, &s)
}

func (s *BasicSuite) createFromYaml(ctx context.Context, path string, kc *kubernetes.Clientset) {
	data, err := os.ReadFile(path)
	s.Require().NoError(err)

	resources, err := parseManifests(data)
	s.Require().NoError(err)

	s.createResources(ctx, resources, kc)
}

func (s *BasicSuite) createResources(ctx context.Context, resources []*unstructured.Unstructured, kc *kubernetes.Clientset) {
	client := s.getDynamicClient()
	mapper := s.getMapper(kc)
	for _, res := range resources {

		mapping, err := mapper.RESTMapping(
			res.GroupVersionKind().GroupKind(),
			res.GroupVersionKind().Version)
		s.Require().NoError(err)

		var drClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			drClient = client.Resource(mapping.Resource).Namespace(res.GetNamespace())
		} else {
			drClient = client.Resource(mapping.Resource)
		}

		_, err = drClient.Create(ctx, res, metav1.CreateOptions{})
		s.Require().NoError(err)
	}

}

func (s *BasicSuite) getMapper(kc *kubernetes.Clientset) *restmapper.DeferredDiscoveryRESTMapper {
	disc := memory.NewMemCacheClient(discovery.NewDiscoveryClient(kc.RESTClient()))
	return restmapper.NewDeferredDiscoveryRESTMapper(disc)
}

func (s *BasicSuite) getDynamicClient() *dynamic.DynamicClient {
	kc, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err)

	client, err := dynamic.NewForConfig(kc)
	s.Require().NoError(err)
	return client
}

func parseManifests(data []byte) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var resource map[string]interface{}
	for {
		err := decoder.Decode(&resource)
		if err == io.EOF {
			return resources, nil
		} else if err != nil {
			return nil, err
		}

		item := &unstructured.Unstructured{
			Object: resource,
		}
		if item.GetAPIVersion() != "" && item.GetKind() != "" {
			resources = append(resources, item)
			resource = nil
		}
	}
}

func (s *BasicSuite) createK0smotronCluster(ctx context.Context, kc *kubernetes.Clientset) {
	// create K0smotron namespace
	_, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kmc-test",
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	kmc := []byte(fmt.Sprintf(`
	{
		"apiVersion": "k0smotron.io/v1beta1",
		"kind": "Cluster",
		"metadata": {
		  "name": "kmc-test",
		  "namespace": "kmc-test"
		},
		"spec": {
			"externalAddress": "%s",
			"service":{
				"type": "NodePort"
			}
		}
	  }
`, s.getNodeAddress(ctx, kc, s.WorkerNode(0))))

	res := kc.RESTClient().Post().AbsPath("/apis/k0smotron.io/v1beta1/namespaces/kmc-test/clusters").Body(kmc).Do(ctx)
	s.Require().NoError(res.Error())
}

func (s *BasicSuite) getPod(ctx context.Context, kc *kubernetes.Clientset) corev1.Pod {
	pods, err := kc.CoreV1().Pods("kmc-test").List(
		ctx,
		metav1.ListOptions{FieldSelector: "status.phase=Running"})
	s.Require().NoError(err, "failed to list kmc-test pods")
	s.Require().Equal(1, len(pods.Items), "expected 1 kmc-test pod, got %d", len(pods.Items))

	return pods.Items[0]
}
func (s *BasicSuite) getJoinToken(kc *kubernetes.Clientset, podName string) string {
	rc, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err, "failed to acquire restConfig")
	output, err := common.PodExecCmdOutput(kc, rc, podName, "kmc-test", "k0s token create --role=worker")

	s.Require().NoError(err, "failed to get join token")
	return output
}

func (s *BasicSuite) getPortForwarder(cfg *rest.Config, stopChan <-chan struct{}, readyChan chan struct{}, pod corev1.Pod) *portforward.PortForwarder {
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	s.Require().NoError(err, "failed to create round tripper")

	url := &url.URL{
		Scheme: "https",
		Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", pod.Namespace, pod.Name),
		Host:   cfg.Host,
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

	fw, err := portforward.New(dialer, []string{"30443"}, stopChan, readyChan, io.Discard, os.Stderr)
	s.Require().NoError(err, "failed to create portforward")
	return fw
}

func (s *BasicSuite) getKMCClientSet(kc *kubernetes.Clientset) *kubernetes.Clientset {
	kubeConf, err := kc.CoreV1().Secrets("kmc-test").Get(s.Context(), "kmc-admin-kubeconfig-kmc-test", metav1.GetOptions{})
	s.Require().NoError(err)

	kmcCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConf.Data["kubeconfig"]))
	s.Require().NoError(err)

	kmcKC, err := kubernetes.NewForConfig(kmcCfg)
	s.Require().NoError(err)
	return kmcKC
}

func (s *BasicSuite) getNodeAddress(ctx context.Context, kc *kubernetes.Clientset, node string) string {
	n, err := kc.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	s.Require().NoError(err, "Unable to get node")
	for _, addr := range n.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	s.FailNow("Node doesn't have an Address of type InternalIP")
	return ""
}

func (s *BasicSuite) forwardPorts(fw *portforward.PortForwarder) {
	s.Require().NoError(fw.ForwardPorts())
}

func (s *BasicSuite) logTS(msg string) {
	s.T().Logf("[%s] %s", time.Now().Format(time.RFC3339), msg)
}
