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

package scalingetcd

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/util/conditions"

	"github.com/k0sproject/k0s/inttest/common"
	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/v2/inttest/util"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type ScalingSuite struct {
	common.FootlooseSuite
}

func (s *ScalingSuite) TestK0sGetsUp() {
	s.T().Log("starting k0s")
	s.Require().NoError(s.InitController(0, "--disable-components=metrics-server"))
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
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling", "default"))
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling-etcd", "default"))

	pod, err := kc.CoreV1().Pods("default").Get(s.Context(), "kmc-scaling-0", metav1.GetOptions{})
	s.Require().NoError(err)
	s.Require().Equal("100m", pod.Spec.Containers[0].Resources.Requests.Cpu().String())
	s.Require().Equal("100Mi", pod.Spec.Containers[0].Resources.Requests.Memory().String())

	s.checkClusterStatus(s.Context(), rc)

	s.T().Log("scaling up k0smotron cluster")
	s.scaleK0smotronCluster(s.Context(), rc, 2)

	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(s.Context(), 1*time.Second, func(_ context.Context) (bool, error) {
		cpSts, err := kc.AppsV1().StatefulSets("default").Get(s.Context(), "kmc-scaling", metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return cpSts.Status.Replicas == 2, nil
	})
	s.Require().NoError(err)
	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(s.Context(), 1*time.Second, func(_ context.Context) (bool, error) {
		etcdSts, err := kc.AppsV1().StatefulSets("default").Get(s.Context(), "kmc-scaling-etcd", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		return etcdSts.Status.Replicas == 3, nil
	})
	s.Require().NoError(err)

	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling", "default"))
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling-etcd", "default"))

	s.checkClusterStatus(s.Context(), rc)

	s.T().Log("scaling up k0smotron cluster")
	s.scaleK0smotronCluster(s.Context(), rc, 1)

	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling", "default"))
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-scaling-etcd", "default"))

	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(s.Context(), 1*time.Second, func(_ context.Context) (bool, error) {
		sts, err := kc.AppsV1().StatefulSets("default").Get(s.Context(), "kmc-scaling", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		return sts.Status.Replicas == 1, nil
	})
	s.Require().NoError(err)

	etcdSts, err := kc.AppsV1().StatefulSets("default").Get(s.Context(), "kmc-scaling-etcd", metav1.GetOptions{})
	s.Require().NoError(err)
	s.Require().Equal(3, int(etcdSts.Status.Replicas))

	s.checkClusterStatus(s.Context(), rc)

	s.T().Log("verifying the etcd member stays Ready under a NOSPACE alarm")
	s.assertReadyUnderNospace(s.Context(), kc, rc)
}

func TestScalingSuite(t *testing.T) {
	s := ScalingSuite{
		common.FootlooseSuite{
			ControllerCount:                 1,
			WorkerCount:                     1,
			K0smotronWorkerCount:            1,
			K0smotronImageBundleMountPoints: []string{"/dist/bundle.tar"},
		},
	}
	suite.Run(t, &s)
}

func (s *ScalingSuite) checkClusterStatus(ctx context.Context, rc *rest.Config) {

	crdConfig := *rc
	crdConfig.ContentConfig.GroupVersion = &km.GroupVersion
	crdConfig.APIPath = "/apis"
	crdConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	crdConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	crdRestClient, err := rest.UnversionedRESTClientFor(&crdConfig)
	s.Require().NoError(err)

	// nolint:staticcheck
	err = wait.PollImmediateUntilWithContext(ctx, 1*time.Second, func(ctx context.Context) (bool, error) {
		var kmc km.Cluster
		err = crdRestClient.
			Get().
			Resource("clusters").
			Name("scaling").
			Namespace("default").
			Do(ctx).
			Into(&kmc)

		return conditions.IsTrue(&kmc, km.ClusterAvailableCondition), nil
	})

	s.Require().NoError(err)
}

func (s *ScalingSuite) assertReadyUnderNospace(ctx context.Context, kc *kubernetes.Clientset, rc *rest.Config) {
	const pod = "kmc-scaling-etcd-0"

	fill := `for i in $(seq 1 24); do head -c 1048576 /dev/urandom | base64 | etcdctl put key-$i >/dev/null 2>&1 || true; done; etcdctl alarm list`
	alarms, err := s.etcdExec(ctx, kc, rc, pod, fill)
	s.Require().NoError(err)
	s.Require().Contains(alarms, "NOSPACE", "test setup failed to arm a NOSPACE alarm")

	s.Require().NoError(util.WaitForPod(ctx, kc, pod, "default"), "etcd member went unready under NOSPACE")

	stillArmed, err := s.etcdExec(ctx, kc, rc, pod, "etcdctl alarm list")
	s.Require().NoError(err)
	s.Require().Contains(stillArmed, "NOSPACE", "alarm cleared before the readiness assertion; test proved nothing")
}

func (s *ScalingSuite) etcdExec(ctx context.Context, kc *kubernetes.Clientset, rc *rest.Config, pod, command string) (string, error) {
	req := kc.CoreV1().RESTClient().Post().Resource("pods").Name(pod).Namespace("default").SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "etcd",
		Command:   []string{"/bin/sh", "-c", command},
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(rc, "POST", req.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return stdout.String(), fmt.Errorf("exec in etcd container failed: %w, stderr: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func (s *ScalingSuite) createK0smotronCluster(ctx context.Context, kc *kubernetes.Clientset) {
	res := kc.RESTClient().Post().AbsPath("/apis/k0smotron.io/v1beta2/namespaces/default/clusters").Body([]byte(clusterResource)).Do(ctx)
	s.Require().NoError(res.Error())
}

func (s *ScalingSuite) scaleK0smotronCluster(ctx context.Context, rc *rest.Config, replicas int) {
	//kmc := fmt.Sprintf(kmcTmpl, 2)
	crdConfig := *rc
	crdConfig.ContentConfig.GroupVersion = &km.GroupVersion
	crdConfig.APIPath = "/apis"
	crdConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	crdConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	crdRestClient, err := rest.UnversionedRESTClientFor(&crdConfig)
	s.Require().NoError(err)

	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec/replicas", "value": %d}]`, replicas)
	res := crdRestClient.
		Patch(types.JSONPatchType).
		Resource("clusters").
		Name("scaling").
		Namespace("default").
		Body([]byte(patch)).
		Do(ctx)
	s.Require().NoError(res.Error())
}

var clusterResource = `
	{
		"apiVersion": "k0smotron.io/v1beta2",
		"kind": "Cluster",
		"metadata": {
		  "name": "scaling",
		  "namespace": "default"
		},
		"spec": {
			"replicas": 1,
   			"version": "v1.31.5-k0s.0",
			"service":{
				"type": "NodePort"
			},
			"resources": {
				"requests": {
					"cpu": "100m",
					"memory": "100Mi"
				}
			},
			"k0sConfig": {
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind": "ClusterConfig",
				"spec": {
					"telemetry": {"enabled": false}
				}
			},
			"storage": {
				"type": "etcd",
				"etcd": {
					"args": ["--auto-compaction-retention=1h", "--snapshot-count=500000", "--quota-backend-bytes=16777216"]
				}
			}
		}
	  }
`
