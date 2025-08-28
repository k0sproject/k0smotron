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
	"strconv"

	"gopkg.in/yaml.v3"
)

// CloudInitProvisioner implements the Provisioner interface for cloud-init.
type CloudInitProvisioner struct{}

// ToProvisionData converts the input data to cloud-init user data.
func (c *CloudInitProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	var b bytes.Buffer

	// Write the "header" first
	_, err := b.WriteString("## template: jinja\n")
	if err != nil {
		return nil, err
	}
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

func (f File) PermissionsAsInt() (int64, error) {
	if f.Permissions == "" {
		f.Permissions = "0644"
	}
	return strconv.ParseInt(f.Permissions, 8, 32)
}
