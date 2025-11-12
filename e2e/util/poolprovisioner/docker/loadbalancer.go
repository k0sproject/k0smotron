package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
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
  timeout connect 2s
  timeout server 5s
  retries 3
  option redispatch
  mode tcp
  balance roundrobin
  option tcp-check
  server node0 %s:6443 check
  server node1 %s:6443 check
  server node2 %s:6443 check
  server node3 %s:6443 check
  server node4 %s:6443 check


# ---------------------------
# HAProxy Stats Page
# ---------------------------
frontend stats
  mode http
  bind *:8404
  stats enable
  stats uri /stats
  stats refresh 1s
  stats admin if TRUE
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

	exposedWorkloadAPIServerPort := nat.Port("6443/tcp")
	exposedWorkloadStatsPort := nat.Port("8404/tcp")

	containerConfig := &dockercontainer.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			exposedWorkloadAPIServerPort: {},
			exposedWorkloadStatsPort:     {},
		},
	}

	hostConfig := &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode(networkName),
		PortBindings: nat.PortMap{
			exposedWorkloadAPIServerPort: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: "",
				},
			},
			exposedWorkloadStatsPort: []nat.PortBinding{
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

	tarBytes, err := tarFile(haproxyCfgPath, "haproxy.cfg")
	if err != nil {
		return fmt.Errorf("tar file: %w", err)
	}

	err = apiClient.CopyToContainer(
		ctx,
		resp.ID,
		"/usr/local/etc/haproxy/",
		bytes.NewReader(tarBytes),
		dockercontainer.CopyToContainerOptions{AllowOverwriteDirWithFile: true},
	)
	if err != nil {
		return fmt.Errorf("copy to container: %w", err)
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

func tarFile(srcPath, dstName string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, err
	}

	hdr := &tar.Header{
		Name: dstName,
		Mode: 0644,
		Size: info.Size(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
