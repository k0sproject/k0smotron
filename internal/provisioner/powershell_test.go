/*
Copyright 2026.

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
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var psBytesRe = regexp.MustCompile(`FromBase64String\('([^']*)'\)`)

// psBody decodes the payload of a script that took the byte path.
func psBody(t *testing.T, script string) []byte {
	t.Helper()

	m := psBytesRe.FindStringSubmatch(script)
	require.Len(t, m, 2, "no base64 payload in script")

	decoded, err := base64.StdEncoding.DecodeString(m[1])
	require.NoError(t, err)
	return decoded
}

func render(t *testing.T, f File) string {
	t.Helper()

	out, err := (&PowerShellProvisioner{}).ToProvisionData(&InputProvisionData{Files: []File{f}})
	require.NoError(t, err)
	return string(out)
}

// TestPowerShellSendsPlainContentAsBytes covers ordinary content taking the byte path
// too, so no file body is ever emitted as script text.
func TestPowerShellSendsPlainContentAsBytes(t *testing.T) {
	s := render(t, File{Path: `C:\k\a.conf`, Content: "body", Permissions: "0644"})

	require.Equal(t, "body", string(psBody(t, s)))
	require.Contains(t, s, "[System.IO.File]::WriteAllBytes(")
	require.NotContains(t, s, "@'")
	require.NotContains(t, s, "$file.Trim()")
}

func TestPowerShellDecodesBeforeRendering(t *testing.T) {
	s := render(t, File{
		Path:     `C:\k\a.conf`,
		Content:  base64.StdEncoding.EncodeToString([]byte("decoded body")),
		Encoding: Base64,
	})

	require.Equal(t, "decoded body", string(psBody(t, s)))
}

// TestPowerShellPreservesContentExactly covers bodies that the old here string
// mangled or could not represent at all.
func TestPowerShellPreservesContentExactly(t *testing.T) {
	t.Run("content outside UTF8", func(t *testing.T) {
		raw := []byte{0x00, 0xff, 0x1b}

		s := render(t, File{
			Path:     `C:\k\blob.bin`,
			Content:  base64.StdEncoding.EncodeToString(raw),
			Encoding: Base64,
		})

		require.Equal(t, raw, psBody(t, s))
		require.Contains(t, s, "[System.IO.File]::WriteAllBytes(")
		require.NotContains(t, s, "$file = @'")
	})

	t.Run("append keeps its bytes exactly", func(t *testing.T) {
		// A here string drops the newline before its terminator, which would
		// run consecutive appends together.
		s := render(t, File{Path: `C:\k\a.conf`, Content: "\ntail\n", Append: true})

		require.Equal(t, "\ntail\n", string(psBody(t, s)))
		require.Contains(t, s, "[System.IO.FileMode]::Append")
		require.NotContains(t, s, "$file.Trim()")
	})
}

func TestPowerShellRejectsUndecodableContent(t *testing.T) {
	_, err := (&PowerShellProvisioner{}).ToProvisionData(&InputProvisionData{
		Files: []File{{Path: `C:\k\x`, Content: "!!!", Encoding: Base64}},
	})
	require.ErrorContains(t, err, "failed to base64 decode")
	require.ErrorContains(t, err, `C:\k\x`)
}

// TestPowerShellQuotesPathsLiterally covers a path reaching the target as data rather
// than as script. Single quoted PowerShell strings expand nothing.
func TestPowerShellQuotesPathsLiterally(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "a subexpression stays inert",
			path: `C:\k\$(ni PWNED).conf`,
			want: `'C:\k\$(ni PWNED).conf'`,
		},
		{
			name: "a variable is not expanded",
			path: `C:\k\$env:TEMP.conf`,
			want: `'C:\k\$env:TEMP.conf'`,
		},
		{
			name: "an embedded quote is doubled",
			path: `C:\k\a'b.conf`,
			want: `'C:\k\a''b.conf'`,
		},
		{
			name: "a double quote needs no escaping now",
			path: `C:\k\a"b.conf`,
			want: `'C:\k\a"b.conf'`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := render(t, File{Path: tc.path, Content: "body", Permissions: "0644"})

			require.Contains(t, s, "WriteAllBytes("+tc.want+", $bytes)")
			// the old form put the path in a double quoted string, which expands
			require.NotContains(t, s, `WriteAllBytes("`)
		})
	}
}

// TestPowerShellQuotesPathsOnEveryWriteForm covers the three other places a path is
// emitted, since each one used to build its own double quoted string.
func TestPowerShellQuotesPathsOnEveryWriteForm(t *testing.T) {
	path := `C:\k\$(ni PWNED)\a.conf`

	// New-Item takes the directory, so it is quoted on every render
	require.Contains(t, render(t, File{Path: path, Content: "body"}),
		`-Path 'C:/k/$(ni PWNED)' | Out-Null`)

	// append routes through Open
	require.Contains(t, render(t, File{Path: path, Content: "body", Append: true}),
		"Open(\n  '"+path+"',")

	// the plain write form
	require.Contains(t, render(t, File{Path: path, Content: "body"}),
		`WriteAllBytes('`+path+`', $bytes)`)
}

// TestPowerShellContentCannotEscapeIntoScript covers a file body that used to close the
// here string that carried it, putting everything after the terminator into the script.
func TestPowerShellContentCannotEscapeIntoScript(t *testing.T) {
	body := "foo\n'@\nWrite-Host PWNED\n"

	s := render(t, File{Path: `C:\k\a.conf`, Content: body, Permissions: "0644"})

	// the body survives whole rather than being truncated at the terminator
	require.Equal(t, body, string(psBody(t, s)))
	// and none of it reaches the script as text
	require.NotContains(t, s, "Write-Host PWNED")
	require.NotContains(t, s, "@'")
}
