package util

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

var ErrNodeNotReady = errors.New("node is not ready yet")

func SetNodeProviderId(ctx context.Context, client *kubernetes.Clientset, providerId string, opts metav1.ListOptions) error {
	nodes, err := client.CoreV1().Nodes().List(ctx, opts)
	if err != nil || len(nodes.Items) == 0 {
		return ErrNodeNotReady
	}

	node := nodes.Items[0]
	if node.Spec.ProviderID == "" {
		node.Spec.ProviderID = providerId
		err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
			return true
		}, func() error {
			_, upErr := client.CoreV1().Nodes().Update(context.Background(), &node, metav1.UpdateOptions{})
			return upErr
		})

		if err != nil {
			return fmt.Errorf("failed to update node '%s' with providerID: %w", node.Name, err)
		}
	}

	return nil
}

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
