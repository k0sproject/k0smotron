/*


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

package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	api "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	rig "github.com/k0sproject/rig/v2"
	rigssh "github.com/k0sproject/rig/v2/protocol/ssh"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var regex = regexp.MustCompile(`--kubelet-root-dir[ =](/[/a-zA-Z0-9_-]+)+`)

type SSHProvisioner struct {
	bootstrapData []byte
	cloudInit     *provisioner.InputProvisionData
	machine       *api.RemoteMachine
	sshKey        []byte
	log           logr.Logger
}

const stopCommandTemplate = `(command -v systemctl > /dev/null 2>&1 && systemctl stop %s) || ` + // systemd
	`(command -v rc-service > /dev/null 2>&1 && rc-service %s stop) || ` + // OpenRC
	`(command -v service > /dev/null 2>&1 && service %s stop) || ` + // SysV
	`(echo "Not a supported init system"; false)`

const (
	ctrlService   = "k0scontroller"
	workerService = "k0sworker"
)

// Provision provisions a new machine
// The provisioning process is as follows:
// 1. Open SSH connection to the machine
// 2. Execute the bootstrap script
// 3. Check sentinel file at /run/cluster-api/bootstrap-success.complete
// 4. success
func (p *SSHProvisioner) Provision(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("remotemachine", p.machine.Name)

	authM, err := rigssh.ParseSSHPrivateKey(p.sshKey, nil)
	if err != nil {
		return fmt.Errorf("failed to parse ssh key: %w", err)
	}

	config := rigssh.Config{
		Address:     p.machine.Spec.Address,
		Port:        p.machine.Spec.Port,
		User:        p.machine.Spec.User,
		AuthMethods: authM,
	}
	rigClient, err := rig.NewClient(rig.WithConnectionConfigurer(&config))
	if err != nil {
		return fmt.Errorf("failed to create SSH client: %w", err)
	}

	err = rigClient.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}
	defer rigClient.Disconnect()

	if p.machine.Spec.UseSudo {
		// If sudo is required, wrap the client with sudo capabilities
		rigClient = rigClient.Sudo()
	}

	// Write files first
	for _, file := range p.cloudInit.Files {
		if err := p.uploadFile(rigClient, file); err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
	}

	// Execute the bootstrap script commands
	for _, cmd := range p.cloudInit.Commands {
		output, err := rigClient.ExecOutput(cmd)
		if err != nil {
			p.log.Error(err, "failed to run command", "output", output)
			return fmt.Errorf("failed to run command: %w", err)
		}
		log.Info("executed command", "command", cmd, "output", output)
	}

	// Check for sentinel file
	if _, err := rigClient.Sudo().FS().Stat("/run/cluster-api/bootstrap-success.complete"); err != nil {
		return errors.New("bootstrap sentinel file not found")
	}

	return nil
}

// Cleanup cleans up a machine
// The provisioning process is as follows:
// 1. Open SSH connection to the machine
// 2. Stops k0s
// 3. Removes node from etcd
// 4. Runs k0s reset
func (p *SSHProvisioner) Cleanup(ctx context.Context, mode RemoteMachineMode) error {
	authM, err := rigssh.ParseSSHPrivateKey(p.sshKey, nil)
	if err != nil {
		return fmt.Errorf("failed to parse ssh key: %w", err)
	}

	config := rigssh.Config{
		Address:     p.machine.Spec.Address,
		Port:        p.machine.Spec.Port,
		User:        p.machine.Spec.User,
		AuthMethods: authM,
	}

	rigClient, err := rig.NewClient(rig.WithConnectionConfigurer(&config))
	if err != nil {
		return fmt.Errorf("failed to create SSH client: %w", err)
	}

	err = rigClient.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}
	defer rigClient.Disconnect()

	if p.machine.Spec.UseSudo {
		// If sudo is required, wrap the client with sudo capabilities
		rigClient = rigClient.Sudo()
	}

	// When k0s is not the bootstrap provider, the user can set custom commands for the clean up process.
	if mode == ModeNonK0s {
		if p.machine.Spec.CustomCleanUpCommands != nil {
			p.log.Info("Cleaning up remote machine...")
			for _, cmd := range p.machine.Spec.CustomCleanUpCommands {
				output, err := rigClient.ExecOutput(cmd)
				if err != nil {
					p.log.Error(err, "failed to run command", "command", cmd, "output", output)
				} else {
					p.log.Info("executed command", "command", cmd, "output", output)
				}
			}
		}

		return nil
	}

	// k0s bootstrap provider used.
	var cmds []string
	if mode == ModeController {
		cmds = append(cmds, "k0s etcd leave")
		cmds = append(cmds, fmt.Sprintf(stopCommandTemplate, ctrlService, ctrlService, ctrlService))
	} else {
		cmds = append(cmds, fmt.Sprintf(stopCommandTemplate, workerService, workerService, ctrlService))
	}

	var kubeletRootDir string
	for _, cmd := range p.cloudInit.Commands {
		if strings.Contains(cmd, "--kubelet-root-dir") {
			finds := regex.FindStringSubmatch(cmd)
			if len(finds) > 1 {
				kubeletRootDir = finds[1]
				break
			}
		}
	}
	if kubeletRootDir == "" {
		cmds = append(cmds, "k0s reset")
	} else {
		cmds = append(cmds, "k0s reset --kubelet-root-dir "+kubeletRootDir)
	}

	p.log.Info("Cleaning up remote machine...")
	for _, cmd := range cmds {
		output, err := rigClient.ExecOutput(cmd)
		if err != nil {
			p.log.Error(err, "failed to run command", "output", output)
		}
	}

	return nil
}

func (p *SSHProvisioner) uploadFile(client *rig.Client, file provisioner.File) error {
	fsys := client.Sudo().FS()
	// Ensure base dir exists for target
	dir := filepath.Dir(file.Path)
	perms, err := file.PermissionsAsInt()
	if err != nil {
		return fmt.Errorf("failed to parse permissions: %w", err)
	}

	if err := fsys.MkdirAll(dir, fs.FileMode(perms)); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	destFile, err := fsys.OpenFile(file.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(perms))
	if err != nil {
		return fmt.Errorf("failed to open remote file for writing: %w", err)
	}
	defer destFile.Close()
	if _, err := io.WriteString(destFile, file.Content); err != nil {
		return fmt.Errorf("failed to write to remote file: %w", err)
	}

	p.log.Info("uploaded file", "path", file.Path, "permissions", perms)
	return nil
}
