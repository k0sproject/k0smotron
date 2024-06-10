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

package pvc

import (
	"context"
	"k8s.io/apimachinery/pkg/util/wait"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/inttest/util"
)

type PVCSuite struct {
	common.FootlooseSuite
}

func (s *PVCSuite) TestK0sGetsUp() {
	s.T().Log("starting k0s")
	s.Require().NoError(s.InitController(0, "--disable-components=konnectivity-server,metrics-server"))
	s.Require().NoError(s.RunWorkers())

	kc, err := s.KubeClient(s.ControllerNode(0))
	s.Require().NoError(err)
	rc, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err)

	err = s.WaitForNodeReady(s.WorkerNode(0), kc)
	s.NoError(err)

	// create folder for k0smotron persistent volume
	ssh, err := s.SSH(s.Context(), s.WorkerNode(0))
	s.Require().NoError(err)
	defer ssh.Disconnect()
	_, err = ssh.ExecWithOutput(s.Context(), "mkdir -p /tmp/kmc-test")
	s.Require().NoError(err)

	s.Require().NoError(s.ImportK0smotronImages(s.Context()))

	s.T().Log("deploying k0smotron operator")
	s.Require().NoError(util.InstallK0smotronOperator(s.Context(), kc, rc))
	s.Require().NoError(common.WaitForDeployment(s.Context(), kc, "k0smotron-controller-manager", "k0smotron"))

	s.T().Log("deploying k0smotron cluster")
	s.createK0smotronCluster(s.Context(), kc)
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-kmc-test", "kmc-test"))

	s.T().Log("updating k0smotron cluster")
	s.updateK0smotronCluster(s.Context(), rc)

	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-kmc-test", "kmc-test"))

	err = wait.PollUntilContextCancel(s.Context(), time.Second, true, func(_ context.Context) (done bool, err error) {
		sts, err := kc.AppsV1().StatefulSets("kmc-test").Get(s.Context(), "kmc-kmc-test", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		return "200Mi" == sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String(), nil
	})
	s.Require().NoError(err)
}

func TestPVCSuite(t *testing.T) {
	s := PVCSuite{
		common.FootlooseSuite{
			ControllerCount:                 1,
			WorkerCount:                     1,
			K0smotronWorkerCount:            1,
			K0smotronImageBundleMountPoints: []string{"/dist/bundle.tar"},
		},
	}
	suite.Run(t, &s)
}

func (s *PVCSuite) createK0smotronCluster(ctx context.Context, kc *kubernetes.Clientset) {
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
			"etcd":{
				"persistence": {"size": "200Mi"}
			},
			"persistence": {
				"type": "pvc",
				"persistentVolumeClaim": {
					"spec": {
						"accessModes": ["ReadWriteOnce"],
						"resources": {
							"requests": {
								"storage": "50Mi"
							}
						}
					}
				}
			},
			"k0sConfig": {
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind": "ClusterConfig",
				"spec": {
					"telemetry": {"enabled": false}
				}
			}
		}
	  }
`)

	res := kc.RESTClient().Post().AbsPath("/apis/k0smotron.io/v1beta1/namespaces/kmc-test/clusters").Body(kmc).Do(ctx)
	s.Require().NoError(res.Error())
}

func (s *PVCSuite) updateK0smotronCluster(ctx context.Context, rc *rest.Config) {
	crdConfig := *rc
	crdConfig.ContentConfig.GroupVersion = &km.GroupVersion
	crdConfig.APIPath = "/apis"
	crdConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	crdConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	crdRestClient, err := rest.UnversionedRESTClientFor(&crdConfig)
	s.Require().NoError(err)

	patch := `[{"op": "replace", "path": "/spec/persistence/persistentVolumeClaim/spec/resources/requests/storage", "value": "200Mi"}]`
	res := crdRestClient.
		Patch(types.JSONPatchType).
		Resource("clusters").
		Name("kmc-test").
		Namespace("kmc-test").
		Body([]byte(patch)).
		Do(ctx)
	s.Require().NoError(res.Error())

	patch = `[{"op": "replace", "path": "/spec/etcd/persistence/size", "value": "300Mi"}]`
	res = crdRestClient.
		Patch(types.JSONPatchType).
		Resource("clusters").
		Name("kmc-test").
		Namespace("kmc-test").
		Body([]byte(patch)).
		Do(ctx)
	s.Require().NoError(res.Error())
}
