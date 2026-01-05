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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPowerShellAWS(t *testing.T) {
	c := &InputProvisionData{
		Files: []File{
			{
				Path:        "/etc/hosts",
				Content:     "foobar",
				Permissions: "0644",
			},
		},
		Commands: []string{
			"echo 'hello world'",
		},
	}

	p := &PowerShellXMLProvisioner{}

	b, err := p.ToProvisionData(c)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	assert.Equal(t, `<powershell>

# --- write_file ---
New-Item -ItemType Directory -Force -Path "/etc" | Out-Null
$file = @'
foobar
'@
[System.IO.File]::WriteAllText(
  "/etc/hosts",
  $file.Trim(),
  [System.Text.Encoding]::ASCII
)

# --- runcmd ---
echo 'hello world'
</powershell>
`, s)
}

func TestCustomPowerShellAWS(t *testing.T) {
	c := &InputProvisionData{
		Files: []File{
			{
				Path:        "/etc/hosts",
				Content:     "foobar",
				Permissions: "0644",
			},
		},
		Commands: []string{
			"echo 'hello world'",
		},
		CustomUserData: `New-Item C:\custom.file -ItemType File
`,
	}

	p := &PowerShellXMLProvisioner{}

	b, err := p.ToProvisionData(c)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	assert.Equal(t, `<powershell>

# --- write_file ---
New-Item -ItemType Directory -Force -Path "/etc" | Out-Null
$file = @'
foobar
'@
[System.IO.File]::WriteAllText(
  "/etc/hosts",
  $file.Trim(),
  [System.Text.Encoding]::ASCII
)

# --- runcmd ---
echo 'hello world'
New-Item C:\custom.file -ItemType File
</powershell>
`, s)
}
