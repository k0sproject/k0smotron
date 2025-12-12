/*
Copyright 2025.

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
	"path/filepath"
	"strings"
)

// PowerShellAWSProvisioner implements the Provisioner interface for cloud-init.
type PowerShellAWSProvisioner struct{}

// ToProvisionData converts the input data to aws windows user data.
func (c *PowerShellAWSProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	var b bytes.Buffer

	// Write the "header" first
	_, err := b.WriteString("<powershell>\n")
	if err != nil {
		return nil, err
	}
	// ---- write_files ----
	for _, f := range input.Files {
		renderWriteFile(&b, f)
	}

	// ---- runcmd ----
	if len(input.Commands) > 0 {
		b.WriteString("\n# --- runcmd ---\n")
		for _, cmd := range input.Commands {
			b.WriteString(cmd)
			b.WriteString("\n")
		}
	}

	if input.CustomUserData != "" {
		_, err = b.WriteString(input.CustomUserData)
		if err != nil {
			return nil, err
		}
	}

	// Write the "footer"
	_, err = b.WriteString("</powershell>\n")
	if err != nil {
		return nil, err
	}

	content := strings.ReplaceAll(b.String(), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return []byte(content), nil
}

// GetFormat returns the format 'cloud-config' of the provisioner.
func (c *PowerShellAWSProvisioner) GetFormat() string {
	return powershellAWSProvisioningFormat
}

func renderWriteFile(buf *bytes.Buffer, f File) {
	dir := filepath.Dir(strings.Replace(f.Path, `\`, `/`, -1))

	buf.WriteString("\n# --- write_file ---\n")

	// Ensure directory exists
	buf.WriteString(fmt.Sprintf(
		"New-Item -ItemType Directory -Force -Path \"%s\" | Out-Null\n",
		escapePS(dir),
	))

	content := normalizeNewlines(f.Content)

	// Here-string write
	buf.WriteString("$file = @'\n")
	buf.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString("'@\n")
	buf.WriteString(fmt.Sprintf(`[System.IO.File]::WriteAllText(
  "%s",
  $file.Trim(),
  [System.Text.Encoding]::ASCII
)`+"\n", escapePS(f.Path)))
}

func escapePS(s string) string {
	// PowerShell double-quoted string escaping
	return strings.ReplaceAll(s, `"`, `""`)
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
