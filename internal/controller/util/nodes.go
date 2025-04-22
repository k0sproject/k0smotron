package util

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	etcdMemberConditionTypeJoined = "Joined"
)

// FindNodeAddress returns a random node address preferring external address if one is found
func FindNodeAddress(nodes *v1.NodeList) string {
	extAddr, intAddr := "", ""

	// Get random node from list
	node := nodes.Items[rand.Intn(len(nodes.Items))]

	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeExternalIP {
			extAddr = addr.Address
			break
		}
		if addr.Type == v1.NodeInternalIP {
			intAddr = addr.Address
		}
	}

	if extAddr != "" {
		return extAddr
	}
	return intAddr
}

func DeleteK0sNodeResources(ctx context.Context, logger logr.Logger, c *kubernetes.Clientset, machine *clusterv1.Machine) error {
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	err := wait.PollUntilContextCancel(waitCtx, 10*time.Second, true, func(fctx context.Context) (bool, error) {
		if err := markChildControlNodeToLeave(fctx, machine.Name, c); err != nil {
			return false, fmt.Errorf("error marking controlnode to leave: %w", err)
		}

		ok, err := checkMachineLeft(fctx, machine.Name, c)
		if err != nil {
			logger.Error(err, "Error checking machine left", "machine", machine.Name)
		}
		return ok, err
	})
	if err != nil {
		return fmt.Errorf("error checking machine left: %w", err)
	}

	return nil
}

func checkMachineLeft(ctx context.Context, name string, clientset *kubernetes.Clientset) (bool, error) {
	var etcdMember unstructured.Unstructured
	err := clientset.RESTClient().
		Get().
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Do(ctx).
		Into(&etcdMember)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("error getting etcd member: %w", err)
	}

	conditions, _, err := unstructured.NestedSlice(etcdMember.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("error getting etcd member conditions: %w", err)
	}

	for _, condition := range conditions {
		conditionMap := condition.(map[string]interface{})
		if conditionMap["type"] == etcdMemberConditionTypeJoined && conditionMap["status"] == "False" {
			err = clientset.RESTClient().
				Delete().
				AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
				Do(ctx).
				Into(&etcdMember)
			if err != nil && !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("error deleting etcd member %s: %w", name, err)
			}

			return true, nil
		}
	}
	return false, nil
}

func markChildControlNodeToLeave(ctx context.Context, name string, clientset *kubernetes.Clientset) error {
	if clientset == nil {
		return nil
	}

	logger := log.FromContext(ctx).WithValues("controlNode", name)

	err := clientset.RESTClient().
		Patch(types.MergePatchType).
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Body([]byte(`{"spec":{"leave":true}, "metadata": {"annotations": {"k0smotron.io/marked-to-leave-at": "` + time.Now().String() + `"}}}`)).
		Do(ctx).
		Error()
	if err != nil {
		logger.Error(err, "error marking etcd member to leave. Trying to mark control node to leave")
		err := clientset.RESTClient().
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
