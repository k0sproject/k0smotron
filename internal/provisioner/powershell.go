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
	"unicode/utf8"
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
		"New-Item -ItemType Directory -Force -Path \"%s\" | Out-Null\n",
		escapePS(dir),
	)

	// PowerShell has no notion of a content encoding, so decode here.
	decoded, err := f.DecodedContent()
	if err != nil {
		return err
	}

	// A here string cannot hold bytes outside UTF8, and it drops the newline
	// before its terminator, so those two cases go through base64 instead.
	if f.Append || !utf8.Valid(decoded) {
		fmt.Fprintf(buf, "$bytes = [System.Convert]::FromBase64String(\"%s\")\n",
			base64.StdEncoding.EncodeToString(decoded))

		if f.Append {
			fmt.Fprintf(buf, `$stream = [System.IO.File]::Open(
  "%s",
  [System.IO.FileMode]::Append
)
$stream.Write($bytes, 0, $bytes.Length)
$stream.Close()`+"\n", escapePS(f.Path))

			return nil
		}

		fmt.Fprintf(buf, "[System.IO.File]::WriteAllBytes(\"%s\", $bytes)\n", escapePS(f.Path))

		return nil
	}

	content := normalizeNewlines(string(decoded))

	// Here-string write
	buf.WriteString("$file = @'\n")
	buf.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString("'@\n")
	fmt.Fprintf(buf, `[System.IO.File]::WriteAllText(
  "%s",
  $file.Trim(),
  [System.Text.Encoding]::ASCII
)`+"\n", escapePS(f.Path))

	return nil
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
