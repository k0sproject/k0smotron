package docker

import (
	"context"
	"fmt"
	"io"
	"os"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/k0sproject/k0smotron/e2e/util/poolprovisioner"
)

var loadBalancerConfig = `
global
  log stdout format raw local0

defaults
  mode tcp
  log global
  timeout connect 5s
  timeout client  50s
  timeout server  50s
  retries 3
  option redispatch

frontend k8s
  mode tcp
  bind *:6443
  default_backend remote_nodes

backend remote_nodes
  mode tcp
  balance roundrobin
  option tcp-check
  server node0 %s:6443 check
  server node1 %s:6443 check
  server node2 %s:6443 check
  server node3 %s:6443 check
  server node4 %s:6443 check
`

var lbIPAddress string

func (d *Provisioner) createLoadBalancer(ctx context.Context, apiClient *client.Client, networkName string, remoteMachinesIPAddresses []string) error {
	fmt.Println("Creating load balancer for workload cluster controlplanes")

	f, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	haproxyCfgPath := f.Name()
	f.Close()

	loadBalancerConfig = fmt.Sprintf(loadBalancerConfig,
		remoteMachinesIPAddresses[0],
		remoteMachinesIPAddresses[1],
		remoteMachinesIPAddresses[2],
		remoteMachinesIPAddresses[3],
		remoteMachinesIPAddresses[4],
	)
	if err := os.WriteFile(f.Name(), []byte(loadBalancerConfig), 0644); err != nil {
		panic(err)
	}

	imageName := "haproxy:2.9"

	reader, err := apiClient.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	exposedPort := nat.Port("6443/tcp")

	containerConfig := &dockercontainer.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			exposedPort: {},
		},
	}

	hostConfig := &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode(networkName),
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   haproxyCfgPath,
				Target:   "/usr/local/etc/haproxy/haproxy.cfg",
				ReadOnly: true,
			},
		},
		PortBindings: nat.PortMap{
			exposedPort: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: "",
				},
			},
		},
	}

	resp, err := apiClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, GetLoadBalancerName())
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := apiClient.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	ip, err := waitForContainerIP(ctx, apiClient, resp.ID, networkName)
	if err != nil {
		return fmt.Errorf("failed to get container IP %q: %v", resp.ID, err)
	}

	lbIPAddress = ip
	d.lb = &poolprovisioner.VM{
		ContainerName: GetLoadBalancerName(),
		ContainerID:   resp.ID,
		IPAddress:     lbIPAddress,
	}

	fmt.Printf("Load balancer started at %s:6443\n", ip)
	return nil
}

// GetLoadBalancerName returns the name of the load balancer container.
func GetLoadBalancerName() string {
	return "haproxy-proxy"
}

// GetLoadBalancerIPAddress returns the IP address of the load balancer.
func GetLoadBalancerIPAddress() string {
	return lbIPAddress
}
