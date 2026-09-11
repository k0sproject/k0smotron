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
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

// PowerShellProvisioner implements the Provisioner interface for cloud-init.
type PowerShellProvisioner struct{}

// ToProvisionData converts the input data to aws windows user data.
func (c *PowerShellProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	var b bytes.Buffer

	// ---- write_files ----
	for _, f := range input.Files {
		if err := renderWriteFile(&b, f); err != nil {
			return nil, err
		}
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
		_, err := b.WriteString(input.CustomUserData)
		if err != nil {
			return nil, err
		}
	}

	content := strings.ReplaceAll(b.String(), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return []byte(content), nil
}

// GetFormat returns the format 'powershell' of the provisioner.
func (c *PowerShellProvisioner) GetFormat() ProvisioningFormat {
	return PowershellProvisioningFormat
}

func renderWriteFile(buf *bytes.Buffer, f File) error {
	dir := filepath.Dir(strings.Replace(f.Path, `\`, `/`, -1))

	buf.WriteString("\n# --- write_file ---\n")

	// Ensure directory exists
	fmt.Fprintf(buf,
		"New-Item -ItemType Directory -Force -Path %s | Out-Null\n",
		quotePS(dir),
	)

	// PowerShell has no notion of a content encoding, so decode here.
	decoded, err := f.DecodedContent()
	if err != nil {
		return err
	}

	// Content goes out as base64 rather than as a here string. A here string is
	// script text, so a line holding its terminator escapes into the script.
	fmt.Fprintf(buf, "$bytes = [System.Convert]::FromBase64String(%s)\n",
		quotePS(base64.StdEncoding.EncodeToString(decoded)))

	if f.Append {
		fmt.Fprintf(buf, `$stream = [System.IO.File]::Open(
  %s,
  [System.IO.FileMode]::Append
)
$stream.Write($bytes, 0, $bytes.Length)
$stream.Close()`+"\n", quotePS(f.Path))

		return nil
	}

	fmt.Fprintf(buf, "[System.IO.File]::WriteAllBytes(%s, $bytes)\n", quotePS(f.Path))

	return nil
}

// quotePS renders s as a PowerShell single quoted string, which is literal, so a
// path cannot expand a variable or run a subexpression.
func quotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
