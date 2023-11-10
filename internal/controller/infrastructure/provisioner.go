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
	"path/filepath"

	"github.com/go-logr/logr"
	api "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/pkg/rigfs"
	"gopkg.in/yaml.v3"
)

type Provisioner struct {
	bootstrapData []byte
	machine       *api.RemoteMachine
	sshKey        []byte
	log           logr.Logger
}

const stopCommandTemplate = `(command -v systemctl > /dev/null 2>&1 && systemctl stop %s) || (command -v rc-service > /dev/null 2>&1 && rc-service %s stop) || (echo "Not a supported init system"; false)`

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
func (p *Provisioner) Provision(_ context.Context) error {
	// Parse the bootstrap data
	cloudInit := &cloudinit.CloudInit{}
	err := yaml.Unmarshal(p.bootstrapData, cloudInit)
	if err != nil {
		return fmt.Errorf("failed to parse bootstrap data: %w", err)
	}

	authM, err := rig.ParseSSHPrivateKey([]byte(p.sshKey), nil)
	if err != nil {
		return fmt.Errorf("failed to parse ssh key: %w", err)
	}

	connection := &rig.Connection{
		SSH: &rig.SSH{
			Address:     p.machine.Spec.Address,
			Port:        p.machine.Spec.Port,
			User:        p.machine.Spec.User,
			AuthMethods: authM,
		},
	}

	if err := connection.Connect(); err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}

	defer connection.Disconnect()

	// Write files first
	for _, file := range cloudInit.Files {
		if err := p.uploadFile(connection, file); err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
	}

	// Execute the bootstrap script commands
	for _, cmd := range cloudInit.RunCmds {
		output, err := connection.ExecOutput(cmd)
		if err != nil {
			p.log.Error(err, "failed to run command", "output", output)
			return fmt.Errorf("failed to run command: %w", err)
		}
	}

	// Check for sentinel file
	fsys := connection.SudoFsys()
	if _, err := fsys.Stat("/run/cluster-api/bootstrap-success.complete"); err != nil {
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
func (p *Provisioner) Cleanup(_ context.Context, mode RemoteMachineMode) error {
	if mode == ModeNonK0s {
		return nil
	}

	authM, err := rig.ParseSSHPrivateKey(p.sshKey, nil)
	if err != nil {
		return fmt.Errorf("failed to parse ssh key: %w", err)
	}

	connection := &rig.Connection{
		SSH: &rig.SSH{
			Address:     p.machine.Spec.Address,
			Port:        p.machine.Spec.Port,
			User:        p.machine.Spec.User,
			AuthMethods: authM,
		},
	}

	if err := connection.Connect(); err != nil {
		p.log.Error(err, "failed to connect to host")
	}

	defer connection.Disconnect()

	var cmds []string
	if mode == ModeController {
		cmds = append(cmds, "k0s etcd leave")
		cmds = append(cmds, fmt.Sprintf(stopCommandTemplate, ctrlService, ctrlService))
	} else {
		cmds = append(cmds, fmt.Sprintf(stopCommandTemplate, workerService, workerService))
	}
	cmds = append(cmds, "k0s reset")

	for _, cmd := range cmds {
		output, err := connection.ExecOutput(cmd)
		if err != nil {
			p.log.Error(err, "failed to run command", "output", output)
		}
	}

	return nil
}

func (p *Provisioner) uploadFile(conn *rig.Connection, file cloudinit.File) error {
	fsys := conn.SudoFsys()
	// Ensure base dir exists for target
	dir := filepath.Dir(file.Path)
	perms, err := file.PermissionsAsInt()
	if err != nil {
		return fmt.Errorf("failed to parse permissions: %w", err)
	}
	if err := fsys.MkDirAll(dir, rigfs.FileMode(perms)); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	destFile, err := fsys.OpenFile(file.Path, rigfs.ModeCreate, rigfs.ModeReadWrite)
	if err != nil {
		return fmt.Errorf("failed to open remote file for writing: %w", err)
	}
	defer destFile.Close()
	if _, err := io.WriteString(destFile, file.Content); err != nil {
		return fmt.Errorf("failed to write to remote file: %w", err)
	}

	return nil
}
