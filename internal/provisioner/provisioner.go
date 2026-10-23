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
	"encoding/base64"
	"fmt"
	"strings"
)

// ProvisioningFormat represents the format used for provisioning.
type ProvisioningFormat string

const (
	// CloudInitProvisioningFormat represents the cloud-init format.
	CloudInitProvisioningFormat ProvisioningFormat = "cloud-config"
	// IgnitionProvisioningFormat represents the ignition format.
	IgnitionProvisioningFormat ProvisioningFormat = "ignition"
	// PowershellProvisioningFormat represents the format of powershell script.
	PowershellProvisioningFormat ProvisioningFormat = "powershell"
	// PowershellXMLProvisioningFormat represents the format of powershell script wrapped in XML tags. Suitable for AWS Windows user data.
	PowershellXMLProvisioningFormat ProvisioningFormat = "powershell-xml"
)

// InputProvisionData holds the data needed for provisioning a machine.
type InputProvisionData struct {
	Files          []File             `yaml:"write_files"`
	Commands       []string           `yaml:"runcmd"`
	CustomUserData string             `yaml:"-"`
	Vars           map[VarName]string `yaml:"-"`
}

// Encoding specifies the encoding of a file's content.
// +kubebuilder:validation:Enum=base64
type Encoding string

const (
	// Base64 implies the contents of the file are base64 encoded.
	Base64 Encoding = "base64"
)

// File represents a file to be created on the target system.
type File struct {
	Path        string `yaml:"path" json:"path,omitempty"`
	Content     string `yaml:"content" json:"content,omitempty"`
	Permissions string `yaml:"permissions" json:"permissions,omitempty"`
	// Owner sets file ownership as a user name and an optional group name
	// separated by a colon. Empty means the file is owned by root.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=256
	Owner string `yaml:"owner,omitempty" json:"owner,omitempty"`
	// Encoding specifies how Content is encoded. When empty Content is used
	// verbatim. Use it to carry bytes that a plain string cannot hold.
	// +kubebuilder:validation:Optional
	Encoding Encoding `yaml:"encoding,omitempty" json:"encoding,omitempty"`
	// Append specifies whether Content is appended to an existing file rather
	// than replacing it. If the file does not exist it is created either way.
	// +kubebuilder:validation:Optional
	Append bool `yaml:"append,omitempty" json:"append,omitempty"`
}

// OwnerUserAndGroup splits Owner into user and group parts. Group is empty
// when Owner has no colon separator.
func (f File) OwnerUserAndGroup() (user, group string) {
	user, group, _ = strings.Cut(f.Owner, ":")
	return user, group
}

func decodeBase64(content, path string) ([]byte, error) {
	// Tolerate whitespace that survives a round trip through YAML block
	// scalars and Secret data.
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(content), ""))
	if err != nil {
		return nil, fmt.Errorf("failed to base64 decode content of file %s: %w", path, err)
	}
	return b, nil
}

// DecodedContent returns Content with Encoding reversed. Provisioners whose
// format has no encoding concept must use this instead of reading Content.
func (f File) DecodedContent() ([]byte, error) {
	switch f.Encoding {
	case "":
		return []byte(f.Content), nil
	case Base64:
		return decodeBase64(f.Content, f.Path)
	default:
		return nil, fmt.Errorf("unsupported encoding %q for file %s", f.Encoding, f.Path)
	}
}

// Provisioner is the interface that wraps the method for converting input data
// to provisioner-specific data.
type Provisioner interface {
	// ToProvisionData converts the input provision data to a provisioner-specific format.
	ToProvisionData(*InputProvisionData) ([]byte, error)
	// GetFormat returns the format string of the provisioner.
	GetFormat() ProvisioningFormat
}
