package util

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FindNodeAddress returns a random node address preferring external address if one is found
func FindNodeAddress(ctx context.Context, client client.Client) (string, error) {
	var internalAddress string

	// Get a random node address as external address
	nodes := &v1.NodeList{}
	err := client.List(ctx, nodes)
	if err != nil {
		return "", err
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if internalAddress == "" && addr.Type == v1.NodeInternalIP {
				internalAddress = addr.Address
			}

			if addr.Type == v1.NodeExternalDNS || addr.Type == v1.NodeExternalIP {
				return addr.Address, nil
			}
		}
	}

	// Return internal address if no external address was found for any node
	return internalAddress, nil
}
