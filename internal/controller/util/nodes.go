package util

import (
	v1 "k8s.io/api/core/v1"
	"math/rand"
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
