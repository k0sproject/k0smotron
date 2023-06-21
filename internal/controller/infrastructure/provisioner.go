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
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/avast/retry-go"
	"github.com/go-logr/logr"
	k0sctl "github.com/k0sproject/k0sctl/pkg/apis/k0sctl.k0sproject.io/v1beta1/cluster"
	api "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"gopkg.in/yaml.v3"

	// anonymous import is needed to load the os configurers
	_ "github.com/k0sproject/k0sctl/configurer"
	// anonymous import is needed to load the os configurers
	_ "github.com/k0sproject/k0sctl/configurer/linux"
	// anonymous import is needed to load the os configurers
	_ "github.com/k0sproject/k0sctl/configurer/linux/enterpriselinux"
)

type Provisioner struct {
	bootstrapData []byte
	machine       *api.RemoteMachine
	sshKey        []byte
	log           logr.Logger
}

// Provision provisions a new machine
// The provisioning process is as follows:
// 1. Open SSH connection to the machine
// 2. Execute the bootstrap script
// 3. Check sentinel file at /run/cluster-api/bootstrap-success.complete
// 4. success
func (p *Provisioner) Provision(ctx context.Context) error {
	// Parse the bootstrap data
	cloudInit := &cloudinit.CloudInit{}
	err := yaml.Unmarshal(p.bootstrapData, cloudInit)
	if err != nil {
		return fmt.Errorf("failed to parse bootstrap data: %w", err)
	}

	// Dump key to a temporary file
	f, err := os.CreateTemp("", "k0smotron-ssh-key")
	if err != nil {
		return fmt.Errorf("failed to create temporary key file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(p.sshKey); err != nil {
		return fmt.Errorf("failed to write key to temporary file: %w", err)
	}
	f.Close()

	keyFile := f.Name()

	connection := rig.Connection{
		SSH: &rig.SSH{
			Address: p.machine.Spec.Address,
			Port:    p.machine.Spec.Port,
			User:    p.machine.Spec.User,
			KeyPath: &keyFile,
		},
	}

	// Wrap the connection in k0sctl host to get OS resolving etc.
	host := &k0sctl.Host{
		Connection: connection,
	}

	if err := p.connect(host); err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}

	defer host.Disconnect()

	// Write files first
	for _, file := range cloudInit.Files {
		if err := p.uploadFile(host, file); err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
	}

	// Execute the bootstrap script commands
	for _, cmd := range cloudInit.RunCmds {
		output, err := host.ExecOutput(cmd)
		if err != nil {
			p.log.Error(err, "failed to run command", "output", output)
			return fmt.Errorf("failed to run command: %w", err)
		}
	}

	// Check for sentinel file
	if !host.Configurer.FileExist(host, "/run/cluster-api/bootstrap-success.complete") {
		return errors.New("bootstrap sentinel file not found")
	}

	return nil
}

const retries = uint(60)

func (p *Provisioner) uploadFile(h *k0sctl.Host, file cloudinit.File) error {

	// Ensure base dir exists for target
	dir := filepath.Dir(file.Path)
	if err := h.Configurer.MkDir(h, dir, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := h.Configurer.WriteFile(h, file.Path, file.Content, file.Permissions); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (p *Provisioner) connect(h *k0sctl.Host) error {
	err := retry.Do(
		func() error {
			h.SSH.SetDefaults()
			return h.Connect()
		},
		retry.OnRetry(
			func(n uint, err error) {
				attNr := strconv.FormatUint(uint64(n+1), 10)
				p.log.Error(err, "failed to connect", "host", h.String(), "attempt", attNr)
			},
		),
		retry.RetryIf(
			func(err error) bool {
				return !errors.Is(err, rig.ErrCantConnect)
			},
		),
		retry.DelayType(retry.CombineDelay(retry.FixedDelay, retry.RandomDelay)),
		retry.MaxJitter(time.Second*2),
		retry.Delay(time.Second*3),
		retry.Attempts(retries),
		retry.LastErrorOnly(true),
	)

	if err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}

	// Resolve the OS type etc.
	if err := h.ResolveConfigurer(); err != nil {
		return fmt.Errorf("failed to resolve host OS: %w", err)
	}

	return nil
}
