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

package controlplane

import (
	"context"
	"fmt"
	"time"

	etcdv1beta1 "github.com/k0sproject/k0s/pkg/apis/etcd/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func getEtcdMember(ctx context.Context, client *kubernetes.Clientset, name string) (*etcdv1beta1.EtcdMember, error) {
	var etcdMember etcdv1beta1.EtcdMember
	err := client.RESTClient().
		Get().
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Do(ctx).
		Into(&etcdMember)
	return &etcdMember, err
}

func deleteEtcdMember(ctx context.Context, client *kubernetes.Clientset, name string) error {
	return client.RESTClient().
		Delete().
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Do(ctx).
		Error()
}

func getEtcdMemberJoinedConditionStatus(etcdMember *etcdv1beta1.EtcdMember) (bool, error) {
	for _, condition := range etcdMember.Status.Conditions {
		if condition.Type == "Joined" {
			switch condition.Status {
			case "True":
				return true, nil
			case "False":
				return false, nil
			default:
				continue
			}
		}
	}
	return false, nil
}

func (c *K0sController) markNodeToLeave(ctx context.Context, client *kubernetes.Clientset, name string) error {
	if client == nil {
		return nil
	}

	logger := log.FromContext(ctx).WithValues("controlNode", name)

	err := client.RESTClient().
		Patch(types.MergePatchType).
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Body([]byte(`{"spec":{"leave":true}, "metadata": {"annotations": {"k0smotron.io/marked-to-leave-at": "` + time.Now().String() + `"}}}`)).
		Do(ctx).
		Error()
	if err != nil {
		logger.Error(err, "error marking etcd member to leave. Trying to mark control node to leave")
		err := client.RESTClient().
			Patch(types.MergePatchType).
			AbsPath("/apis/autopilot.k0sproject.io/v1beta2/controlnodes/" + name).
			Body([]byte(`{"metadata":{"annotations":{"k0smotron.io/leave":"true"}}}`)).
			Do(ctx).
			Error()
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("error marking control node to leave: %w", err)
		}
	}
	logger.Info("marked etcd to leave")

	return nil
}
