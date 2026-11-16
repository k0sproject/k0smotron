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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/k0sproject/rig/v2/remotefs"
	"sigs.k8s.io/controller-runtime/pkg/log"

	api "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

var regex = regexp.MustCompile(`--kubelet-root-dir[ =](/[/a-zA-Z0-9_-]+)+`)

func init() {
	// RemoteMachine manages infrastructure where VMs may be reprovisioned with
	// new host keys at the same address. Disable host key verification to avoid
	// persistent connection failures after a wipe-and-reprovision cycle. An
	// empty path is what switches verification off.
	hostkey.KnownHostsPathFromEnv = func() (string, bool) {
		return "", true
	}
}

// SSHProvisioner is responsible for provisioning a remote machine using SSH.
type SSHProvisioner struct {
	cloudInit *provisioner.InputProvisionData
	machine   *api.RemoteMachine
	sshKey    []byte
	log       logr.Logger
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

	connection, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer connection.Disconnect()
	// Closing the connection is what stops a hung command or filesystem call.
	defer context.AfterFunc(ctx, connection.Disconnect)()

	// If sudo is required, run commands with elevated permissions and use the
	// sudo-capable filesystem for uploads.
	runner := connection
	if p.machine.Spec.UseSudo {
		runner = connection.Sudo()
	}
	fsys := remotefs.WithContext(ctx, runner.FS())

	installScriptPath := filepath.Join(p.machine.Spec.WorkingDir, "bootstrap.sh")

	if p.machine.Spec.CommandsAsScript {
		// Write the bootstrap script
		bootstrapFile := provisioner.File{
			Path:        installScriptPath,
			Permissions: "0700",
			Content:     "#!/bin/sh\nset -e\n\n" + strings.Join(p.cloudInit.Commands, "\n") + "\n",
		}
		p.cloudInit.Files = append(p.cloudInit.Files, bootstrapFile)
	}

	// Write files first
	for _, file := range p.cloudInit.Files {
		p.log.Info("Uploading file", "path", file.Path, "permissions", file.Permissions)
		if err := p.uploadFile(ctx, runner, fsys, file); err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
		p.log.Info("Uploaded file", "path", file.Path, "permissions", file.Permissions)
	}

	if p.machine.Spec.CommandsAsScript {
		// Run the install script
		p.log.Info("running install script", "command", installScriptPath)
		output, err := execOutput(ctx, runner, installScriptPath)
		if err != nil {
			p.log.Error(err, "failed to run command", "command", installScriptPath, "output", output, "stderr", cmd.StderrOf(err))
			return fmt.Errorf("failed to run command: %w", err)
		}
		log.Info("executed install script", "command", installScriptPath, "output", output)
	} else {
		// Run commands
		for _, command := range p.cloudInit.Commands {
			p.log.Info("running command", "command", command)
			output, err := execOutput(ctx, runner, command)
			if err != nil {
				p.log.Error(err, "failed to run command", "command", command, "output", output, "stderr", cmd.StderrOf(err))
				return fmt.Errorf("failed to run command: %w", err)
			}
			log.Info("executed command", "command", command, "output", output)
		}
	}

	// Check for sentinel file
	if _, err := remotefs.WithContext(ctx, connection.Sudo().FS()).Stat("/run/cluster-api/bootstrap-success.complete"); err != nil {
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
	connection, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer connection.Disconnect()
	// Closing the connection is what stops a hung command or filesystem call.
	defer context.AfterFunc(ctx, connection.Disconnect)()

	// If sudo is required, run commands with elevated permissions.
	runner := connection
	if p.machine.Spec.UseSudo {
		runner = connection.Sudo()
	}

	if p.machine.Spec.CleanUpCommands != nil {
		p.log.Info("Cleaning up remote machine...")
		for _, command := range p.machine.Spec.CleanUpCommands {
			output, err := execOutput(ctx, runner, command)
			if err != nil {
				p.log.Error(err, "failed to run command", "command", command, "output", output, "stderr", cmd.StderrOf(err))
			} else {
				p.log.Info("executed command", "command", command, "output", output)
			}
		}

		return nil
	}

	if mode == ModeNonK0s {
		// If k0s is not the bootstrap provider, we have nothing to do.
		p.log.Info("k0smotron is not the bootstrap provider and no cleanup commands specified, skipping cleanup")
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
	for _, command := range cmds {
		output, err := execOutput(ctx, runner, command)
		if err != nil {
			// if k0s command is not installed, manually remove files added for k0s bootstrap.
			if isCommandNotFound(cmd.StderrOf(err)) {
				for _, file := range p.cloudInit.Files {
					p.log.Info("Removing file", "path", file.Path)
					err := remotefs.WithContext(ctx, connection.Sudo().FS()).Remove(file.Path)
					if err != nil {
						p.log.Error(err, "failed to remove file", "path", file.Path)
					} else {
						p.log.Info("Removed file", "path", file.Path)
					}
				}
			}
			p.log.Error(err, "failed to run command", "command", command, "output", output, "stderr", cmd.StderrOf(err))
		}
	}

	return nil
}

// commandRunner runs a command on the remote machine. *rig.Client satisfies
// it, and a test can substitute a recorder.
type commandRunner interface {
	ExecContext(ctx context.Context, command string, opts ...cmd.ExecOption) error
}

// execOutput runs command and returns its stdout. Unlike ExecOutput, the
// stdout collected so far is still returned when the command fails.
func execOutput(ctx context.Context, runner commandRunner, command string) (string, error) {
	var out bytes.Buffer
	err := runner.ExecContext(ctx, command, cmd.Stdout(&out))
	return out.String(), err
}

// isCommandNotFound reports whether a command's stderr says the shell could not
// find it. Commands run under /bin/sh, and dash reports "k0s: not found" where
// bash says "command not found", so match the part both share.
func isCommandNotFound(stderr string) bool {
	return strings.Contains(stderr, "not found")
}

// shellQuote wraps s in single quotes so it survives a POSIX shell as one
// literal argument, rather than being interpolated into a remote command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (p *SSHProvisioner) uploadFile(ctx context.Context, runner commandRunner, fsys remotefs.FS, file provisioner.File) error {
	// Ensure base dir exists for target. Change Windows-style slashes to Unix-style. Windows is ok with forward slashes, but filepath.Dir is not.
	dir := filepath.Dir(strings.Replace(file.Path, `\`, `/`, -1))
	perms, err := file.PermissionsAsInt()
	if err != nil {
		return fmt.Errorf("failed to parse permissions: %w", err)
	}

	// Provision uploads every file again whenever it is retried, so appending
	// here would duplicate the content on each attempt.
	if file.Append {
		return fmt.Errorf("file %s uses append, which is not supported when provisioning over ssh", file.Path)
	}

	// Nothing on the remote host understands a content encoding, so decode here.
	content, err := file.DecodedContent()
	if err != nil {
		return err
	}

	// The file mode is deliberately not reused for the directory. A mode such as
	// 0600 would leave the directory untraversable for the file owner.
	if _, err := fsys.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := fsys.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	destFile, err := fsys.OpenFile(file.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(perms))
	if err != nil {
		return fmt.Errorf("failed to open remote file for writing: %w", err)
	}
	if _, err := destFile.Write(content); err != nil {
		_ = destFile.Close()
		return fmt.Errorf("failed to write to remote file: %w", err)
	}
	// On Windows the close is what confirms the remote helper finished the write.
	if err := destFile.Close(); err != nil {
		return fmt.Errorf("failed to close remote file: %w", err)
	}

	// remotefs.FS cannot change ownership, so shell out for it. Done after the
	// write, which already created the file with its restrictive mode.
	if file.Owner != "" {
		chownCmd := fmt.Sprintf("chown -- %s %s", shellQuote(file.Owner), shellQuote(file.Path))
		if output, err := execOutput(ctx, runner, chownCmd); err != nil {
			return fmt.Errorf("failed to set owner %q on %s: %w (output: %s)", file.Owner, file.Path, err, output)
		}
	}

	p.log.Info("uploaded file", "path", file.Path, "permissions", perms, "owner", file.Owner)
	return nil
}

// connect opens an SSH connection to the machine.
func (p *SSHProvisioner) connect(ctx context.Context) (*rig.Client, error) {
	authM, err := ssh.ParseSSHPrivateKey(p.sshKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ssh key: %w", err)
	}

	conn, err := ssh.NewConnection(ssh.Config{
		Address:     p.machine.Spec.Address,
		Port:        p.machine.Spec.Port,
		User:        p.machine.Spec.User,
		AuthMethods: authM,
		// Keep the controller hermetic: an ssh_config in the image or a mounted
		// home directory must not change how machines are reached.
		IgnoreSSHConfig: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh connection: %w", err)
	}

	// rig logs nothing at info level on its own, only warnings such as a sudo
	// that needs a terminal, so routing it here adds no noise.
	client, err := rig.NewClient(rig.WithConnection(conn), rig.WithLogger(slog.New(logr.ToSlogHandler(p.log))))
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh client: %w", err)
	}
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to host: %w", err)
	}
	return client, nil
}
