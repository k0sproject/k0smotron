/*
Copyright 2023.

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
package provisioner

import (
	"bytes"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/k0sproject/k0smotron/internal/featuregate"
)

// CloudInitProvisioner implements the Provisioner interface for cloud-init.
type CloudInitProvisioner struct{}

// VarName is the name of a variable that can be used in the cloud-init template
type VarName string

const (
	// VarPreStartCommand is the name of the variable that contains commands from spec.preStartCommands
	VarPreStartCommand VarName = "k0smotron_preStartCommands"
	// VarPostStartCommand is the name of the variable that contains commands from spec.postStartCommands
	VarPostStartCommand VarName = "k0smotron_postStartCommands"
	// VarK0sDownloadCommands is the name of the variable that contains commands to download k0s
	VarK0sDownloadCommands VarName = "k0smotron_k0sDownloadCommands"
	// VarK0sInstallCommand is the name of the variable that contains the command to install k0s
	VarK0sInstallCommand VarName = "k0smotron_k0sInstallCommand"
	// VarK0sStartCommand is the name of the variable that contains the command to start k0s
	VarK0sStartCommand VarName = "k0smotron_k0sStartCommand"
)

// ToProvisionData converts the input data to cloud-init user data.
func (c *CloudInitProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	var b bytes.Buffer

	// Write the "header" first
	_, err := b.WriteString("## template: jinja\n")
	if err != nil {
		return nil, err
	}

	// If CloudInitVars feature is enabled write all k0smotron commands and files as jinja variables
	if len(input.Vars) > 0 && featuregate.IsEnabled(featuregate.CloudInitVars) {
		for k, v := range input.Vars {
			_, _ = b.WriteString("{% set " + string(k) + " = \"" + v + "\" %}\n")
		}
		writeFilesVars(&b, input.Files)
	} else {
		_, err = b.WriteString("#cloud-config\n")
		if err != nil {
			return nil, err
		}
		// Marshal the data
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		defer enc.Close()

		err = enc.Encode(input)
		if err != nil {
			return nil, err
		}
	}

	if input.CustomUserData != "" {
		_, err = b.WriteString("\n#cloud-config\n")
		if err != nil {
			return nil, err
		}
		_, err = b.WriteString(input.CustomUserData)
		if err != nil {
			return nil, err
		}
	}

	return b.Bytes(), nil
}

// GetFormat returns the format 'cloud-config' of the provisioner.
func (c *CloudInitProvisioner) GetFormat() string {
	return cloudInitProvisioningFormat
}

func (f File) PermissionsAsInt() (int64, error) {
	if f.Permissions == "" {
		f.Permissions = "0644"
	}
	return strconv.ParseInt(f.Permissions, 8, 32)
}

func writeFilesVars(b *bytes.Buffer, files []File) {
	if len(files) == 0 {
		return
	}
	b.WriteString("{% set k0smotron_files = [\n")
	for i, f := range files {
		b.WriteString("  {\n")
		b.WriteString(fmt.Sprintf("    \"path\": \"%s\",\n", f.Path))
		b.WriteString(fmt.Sprintf("    \"content\": \"%s\",\n", escapeNewlines(f.Content)))
		b.WriteString(fmt.Sprintf("    \"permissions\": \"%s\"\n", f.Permissions))
		if i < len(files)-1 {
			b.WriteString("  },\n")
		} else {
			b.WriteString("  }\n")
		}
	}
	b.WriteString("] %}\n")
}

func escapeNewlines(s string) string {
	return fmt.Sprintf("%q", s)[1 : len(fmt.Sprintf("%q", s))-1]
}
