package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/external"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/infrastructure/container"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

// RemoteMachineLogCollector collects logs from remote machines.
type RemoteMachineLogCollector struct {
	Provisioner *Provisioner
}

// CollectMachineLog collects logs from the given machine and writes them to the outputPath.
func (k RemoteMachineLogCollector) CollectMachineLog(ctx context.Context, c client.Client, m *clusterv1.Machine, outputPath string) error {
	containerRuntime, err := container.NewDockerClient()
	if err != nil {
		return err
	}
	ctx = container.RuntimeInto(ctx, containerRuntime)

	containerIP, err := getContainerIP(ctx, c, m)
	if err != nil {
		return err
	}

	isControlPlaneNode := false
	for k := range m.Labels {
		if k == clusterv1.MachineControlPlaneLabel {
			isControlPlaneNode = true
			break
		}
	}

	for _, vm := range k.Provisioner.remoteMachines {
		if containerIP == vm.IPAddress {
			return k.collectLogsFromNode(ctx, outputPath, vm.ContainerName, isControlPlaneNode)
		}
	}
	return fmt.Errorf("no containers found for machine %s", m.Name)
}

// CollectMachinePoolLog is a no-op for docker provisioner as machine pools templates are not declared yet.
func (k RemoteMachineLogCollector) CollectMachinePoolLog(_ context.Context, _ client.Client, _ *expv1.MachinePool, _ string) error {
	return nil
}

// CollectInfrastructureLogs collects infrastructure logs and writes them to the outputPath.
func (k RemoteMachineLogCollector) CollectInfrastructureLogs(ctx context.Context, _ client.Client, _ *clusterv1.Cluster, outputPath string) error {
	containerRuntime, err := container.NewDockerClient()
	if err != nil {
		return err
	}
	ctx = container.RuntimeInto(ctx, containerRuntime)

	lbContainerName := k.Provisioner.lb.ContainerName

	f, err := fileOnHost(filepath.Join(outputPath, fmt.Sprintf("%s.log", lbContainerName)))
	if err != nil {
		return err
	}

	defer f.Close()

	return containerRuntime.ContainerDebugInfo(ctx, lbContainerName, f)
}

// fileOnHost is a helper to create a file at path
// even if the parent directory doesn't exist
// in which case it will be created with ModePerm.
func fileOnHost(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	return os.Create(path) //nolint:gosec // No security issue: path is safe.
}

func getContainerIP(ctx context.Context, c client.Client, m *clusterv1.Machine) (string, error) {
	if m == nil {
		return "", nil
	}
	remoteMachineRef := &corev1.ObjectReference{
		APIVersion: m.Spec.InfrastructureRef.APIVersion,
		Kind:       m.Spec.InfrastructureRef.Kind,
		Namespace:  m.Namespace,
		Name:       m.Spec.InfrastructureRef.Name,
	}
	uRemoteMachine, err := external.Get(ctx, c, remoteMachineRef)
	if err != nil {
		return "", err
	}
	ip, found, err := unstructured.NestedString(uRemoteMachine.Object, "spec", "address")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no address found in remote machine %s/%s", m.Namespace, m.Name)
	}
	return ip, nil
}

// From https://github.com/kubernetes-sigs/cluster-api/blob/main/test/framework/docker_logcollector.go#L106
// collectLogsFromNode collects logs from the specified container and writes them to outputPath.
func (k RemoteMachineLogCollector) collectLogsFromNode(ctx context.Context, outputPath string, containerName string, isControlPlaneNode bool) error {
	containerRuntime, err := container.RuntimeFrom(ctx)
	if err != nil {
		return errors.Wrap(err, "Failed to collect logs from node")
	}

	execToPathFn := func(outputFileName, command string, args ...string) func() error {
		return func() error {
			f, err := fileOnHost(filepath.Join(outputPath, outputFileName))
			if err != nil {
				return err
			}
			defer f.Close()
			execConfig := container.ExecContainerInput{
				OutputBuffer: f,
			}
			return containerRuntime.ExecContainer(ctx, containerName, &execConfig, command, args...)
		}
	}
	copyDirFn := func(containerDir, dirName string) func() error {
		return func() error {
			f, err := os.CreateTemp("", containerName)
			if err != nil {
				return err
			}

			tempfileName := f.Name()
			outputDir := filepath.Join(outputPath, dirName)

			defer os.Remove(tempfileName)

			var execErr string
			execConfig := container.ExecContainerInput{
				OutputBuffer: f,
				ErrorBuffer:  bytes.NewBufferString(execErr),
			}
			err = containerRuntime.ExecContainer(
				ctx,
				containerName,
				&execConfig,
				"tar", "--hard-dereference", "--dereference", "--directory", containerDir, "--create", "--file", "-", ".",
			)
			if err != nil {
				return errors.Wrap(err, execErr)
			}

			err = os.MkdirAll(outputDir, 0750)
			if err != nil {
				return err
			}

			return osExec.Command("tar", "--extract", "--file", tempfileName, "--directory", outputDir).Run() //nolint:gosec // We don't care about command injection here.
		}
	}

	serviceName := "k0scontroller.service"
	if !isControlPlaneNode {
		serviceName = "k0sworker.service"
	}

	collectFuncs := []func() error{
		execToPathFn(
			"journal.log",
			"journalctl", "--no-pager", "--output=short-precise",
		),
		execToPathFn(
			"kern.log",
			"journalctl", "--no-pager", "--output=short-precise", "-k",
		),
		execToPathFn(
			"kubelet-version.txt",
			"kubelet", "--version",
		),
		execToPathFn(
			"kubelet.log",
			"journalctl", "--no-pager", "--output=short-precise", "-u", "kubelet.service",
		),
		execToPathFn(
			"containerd-info.txt",
			"crictl", "info",
		),
		execToPathFn(
			"containerd.log",
			"journalctl", "--no-pager", "--output=short-precise", "-u", "containerd.service",
		),
		execToPathFn(
			"k0s.log",
			"journalctl", "--no-pager", "--output=short-precise", "-u", serviceName,
		),
		copyDirFn("/var/log/pods", "pods"),
	}

	return errors.AggregateConcurrent(collectFuncs)
}
