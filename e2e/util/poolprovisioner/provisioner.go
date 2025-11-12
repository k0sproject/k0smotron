package poolprovisioner

import (
	"context"
)

// VM represents a virtual machine created by the provisioner.
type VM struct {
	ContainerName string
	ContainerID   string
	IPAddress     string
}

// PoolProvisioner is the global pool provisioner instance.
var PoolProvisioner Provisioner

// Provisioner is the interface that pool provisioners must implement.
type Provisioner interface {
	// Provision creates a number of virtual machines with the specified node version and returns their addresses.
	// It set addresses to the Pool variable.
	Provision(ctx context.Context, replicas int, nodeVersion string, publicKey []byte) error
	// Clean removes all the virtual machines created by the provisioner.
	Clean(ctx context.Context) error
	// GetRemoteMachinesAddresses returns the IP addresses of the provisioned virtual machines.
	GetRemoteMachinesAddresses() []string
}
