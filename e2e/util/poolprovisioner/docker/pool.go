package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/k0sproject/k0smotron/e2e/util/poolprovisioner"
)

// Provisioner implements a pool provisioner using Docker containers as VMs.
type Provisioner struct {
	remoteMachines []poolprovisioner.VM
	lb             *poolprovisioner.VM
}

// Provision creates a number of Docker containers with the specified node version and sets theis addresses in the docker provisioner.
func (d *Provisioner) Provision(ctx context.Context, replicas int, nodeVersion string, publicKey []byte) error {
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create Docker client: %v", err)
	}
	defer apiClient.Close()

	info, err := apiClient.Info(ctx)
	if err != nil {
		return fmt.Errorf("unable to inspect Docker engine info: %v", err)
	}

	networks, err := apiClient.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("list Docker networks: %v", err)
	}

	// Network must be "kind" because management cluster where controllers run is within the "kind" network.
	networkName := "kind"
	existsKindNetwork := false
	for _, n := range networks {
		if n.Name == networkName {
			existsKindNetwork = true
			break
		}
	}

	if !existsKindNetwork {
		_, err = apiClient.NetworkCreate(ctx, networkName, network.CreateOptions{
			Driver: "bridge",
			Options: map[string]string{
				"com.docker.network.bridge.enable_ip_masquerade": "true",
			},
		})
		if err != nil {
			return fmt.Errorf("create %s network: %v", networkName, err)
		}
	}

	imageName := fmt.Sprintf("kindest/node:%s", nodeVersion)

	reader, err := apiClient.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	remoteMachinesIPAddresses := []string{}
	fmt.Println("Creating a pool of Docker containers as machines...")
	for i := 0; i < replicas; i++ {

		containerConfig := &dockercontainer.Config{
			Image: imageName,
			Tty:   true,
			Volumes: map[string]struct{}{
				"/var": {},
			},
		}

		hostConfig := &dockercontainer.HostConfig{
			Privileged:   true,
			SecurityOpt:  []string{"seccomp=unconfined", "apparmor=unconfined"},
			CgroupnsMode: "private",
			NetworkMode:  dockercontainer.NetworkMode(networkName),
			Tmpfs: map[string]string{
				"/tmp": "",
				"/run": "",
			},
			PortBindings: nat.PortMap{},
		}

		hostConfig.Binds = append(hostConfig.Binds,
			"/lib/modules:/lib/modules:ro",
		)

		if info.Driver == "btrfs" || info.Driver == "zfs" {
			hostConfig.Binds = append(hostConfig.Binds, "/dev/mapper:/dev/mapper:ro")
		}

		for _, sec := range info.SecurityOptions {
			if strings.Contains(sec, "rootless") {
				hostConfig.Devices = append(hostConfig.Devices, dockercontainer.DeviceMapping{PathOnHost: "/dev/fuse"})
				break
			}
		}

		networkConfig := &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: {
					Aliases: []string{fmt.Sprintf("remote-machine-%d", i)},
				},
			},
		}

		name := fmt.Sprintf("remote-machine-%d", i)
		resp, err := apiClient.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, name)
		if err != nil {
			return fmt.Errorf("create container %q: %v", name, err)
		}

		if err := apiClient.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
			return fmt.Errorf("start container %q: %v", name, err)
		}

		runSSH := "apt-get update && apt-get install -y openssh-server && mkdir -p /var/run/sshd && /usr/sbin/sshd"
		addKey := fmt.Sprintf(
			"mkdir -p /root/.ssh && echo '%s' >> /root/.ssh/authorized_keys && chmod 700 /root/.ssh && chmod 600 /root/.ssh/authorized_keys",
			strings.TrimSpace(string(publicKey)),
		)
		cmd := fmt.Sprintf("%s && %s", runSSH, addKey)
		execConfig := dockercontainer.ExecOptions{
			Cmd:          []string{"bash", "-c", cmd},
			AttachStdout: true,
			AttachStderr: true,
		}
		execResp, err := apiClient.ContainerExecCreate(ctx, resp.ID, execConfig)
		if err != nil {
			return fmt.Errorf("create exec in container %q: %v", name, err)
		}
		if err := apiClient.ContainerExecStart(ctx, execResp.ID, dockercontainer.ExecStartOptions{}); err != nil {
			return fmt.Errorf("exec start in container %q: %v", name, err)
		}

		ip, err := waitForContainerIP(ctx, apiClient, resp.ID, networkName)
		if err != nil {
			return fmt.Errorf("failed to get container IP %q: %v", name, err)
		}
		d.remoteMachines = append(d.remoteMachines, poolprovisioner.VM{
			ContainerName: name,
			ContainerID:   resp.ID,
			IPAddress:     ip,
		})
		remoteMachinesIPAddresses = append(remoteMachinesIPAddresses, ip)

		fmt.Printf("Created container %q with IP %s\n", name, ip)
	}
	fmt.Println("Created machines pool.")

	if len(d.remoteMachines) != replicas {
		return fmt.Errorf("expected %d addresses, got %d", replicas, len(d.remoteMachines))
	}

	err = d.createLoadBalancer(ctx, apiClient, networkName, remoteMachinesIPAddresses)
	if err != nil {
		return fmt.Errorf("create load balancer: %v", err)
	}

	return nil
}

// GetRemoteMachinesAddresses returns the IP addresses of the provisioned docker containers virtual machines.
func (d *Provisioner) GetRemoteMachinesAddresses() []string {
	var addresses []string
	for _, vm := range d.remoteMachines {
		addresses = append(addresses, vm.IPAddress)
	}
	return addresses
}

// Clean removes all the docker containers virtual machines created by the provisioner (including the load balancer).
func (d *Provisioner) Clean(ctx context.Context) error {
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create Docker client: %v", err)
	}
	defer apiClient.Close()

	fmt.Println("Cleaning up Remote machines...")
	for _, vm := range d.remoteMachines {
		timeout := 10
		err := apiClient.ContainerStop(ctx, vm.ContainerID, dockercontainer.StopOptions{Timeout: &timeout})
		if err != nil {
			return fmt.Errorf("stop container %q: %v", vm.ContainerName, err)
		}

		err = apiClient.ContainerRemove(ctx, vm.ContainerID, dockercontainer.RemoveOptions{
			Force: true,
		})
		if err != nil {
			return fmt.Errorf("remove container %q: %v", vm.ContainerName, err)
		}

		fmt.Printf("Removed container %q\n", vm.ContainerName)
	}

	if d.lb == nil {
		return nil
	}
	fmt.Println("Removing load balancer...")
	err = apiClient.ContainerRemove(ctx, d.lb.ContainerID, dockercontainer.RemoveOptions{
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("remove load balancer container %q: %v", GetLoadBalancerName(), err)
	}
	fmt.Println("Load balancer removed.")
	return nil
}

func waitForContainerIP(ctx context.Context, cli *client.Client, id string, network string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		inspect, err := cli.ContainerInspect(ctx, id)
		if err != nil {
			return "", err
		}
		if net, ok := inspect.NetworkSettings.Networks[network]; ok {
			if net.IPAddress != "" {
				return net.IPAddress, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for IP")
}
