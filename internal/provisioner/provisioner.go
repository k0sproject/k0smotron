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

// InputProvisionData holds the data needed for provisioning a machine.
type InputProvisionData struct {
	Files          []File   `yaml:"write_files" json:"files,omitempty"`
	Commands       []string `yaml:"runcmd" json:"cmds,omitempty"`
	CustomUserData string   `yaml:"-" json:"-,omitempty"`
}

// File represents a file to be created on the target system.
type File struct {
	Path        string `yaml:"path" json:"path,omitempty"`
	Content     string `yaml:"content" json:"content,omitempty"`
	Permissions string `yaml:"permissions" json:"permissions,omitempty"`
}

// Provisioner is the interface that wraps the method for converting input data
// to provisioner-specific data.
type Provisioner interface {
	// ToProvisionData converts the input provision data to a provisioner-specific format.
	ToProvisionData(*InputProvisionData) ([]byte, error)
}
