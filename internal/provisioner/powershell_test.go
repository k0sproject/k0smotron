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

var psBytesRe = regexp.MustCompile(`FromBase64String\("([^"]*)"\)`)

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

// TestPowerShellKeepsHereStringForPlainContent pins the existing output, so a
// config that worked before this change renders exactly as it did.
func TestPowerShellKeepsHereStringForPlainContent(t *testing.T) {
	s := render(t, File{Path: `C:\k\a.conf`, Content: "body", Permissions: "0644"})

	require.Contains(t, s, "$file = @'\nbody\n'@")
	require.Contains(t, s, "$file.Trim()")
	require.Contains(t, s, "[System.Text.Encoding]::ASCII")
	require.NotContains(t, s, "FromBase64String")
}

func TestPowerShellDecodesBeforeRendering(t *testing.T) {
	s := render(t, File{
		Path:     `C:\k\a.conf`,
		Content:  base64.StdEncoding.EncodeToString([]byte("decoded body")),
		Encoding: Base64,
	})

	require.Contains(t, s, "$file = @'\ndecoded body\n'@")
}

// TestPowerShellUsesBytesWhereAHereStringCannot covers the two cases a here
// string cannot represent, neither of which an existing config can reach.
func TestPowerShellUsesBytesWhereAHereStringCannot(t *testing.T) {
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
