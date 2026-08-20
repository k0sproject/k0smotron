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

package capiremotemachine

import (
	"bytes"
	"encoding/base64"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"

	bootstrap "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/v2/inttest/util"
)

// TestClusterManifestDeclaresFile renders the suite manifest without needing a
// host. It is also the only cover for the json tags on the new fields.
func TestClusterManifestDeclaresFile(t *testing.T) {
	tpl, err := template.New("cluster").Parse(clusterYaml)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, tpl.Execute(&buf, struct {
		Address, SSHKey, K0SVersion, FilePath, FileContent, FileOwner, FilePermissions string
	}{
		Address:         "10.0.0.1",
		SSHKey:          "a2V5",
		K0SVersion:      "v1.30.2",
		FilePath:        testFilePath,
		FileContent:     base64.StdEncoding.EncodeToString([]byte(testFileContent)),
		FileOwner:       testFileOwner,
		FilePermissions: testFilePermissions,
	}))

	// Every document must parse, which catches indentation mistakes.
	resources, err := util.ParseManifests(buf.Bytes())
	require.NoError(t, err)
	require.NotEmpty(t, resources)

	// And the file must land on the bootstrap config with the fields intact.
	var found bool
	for _, r := range resources {
		if r.GetKind() != "K0sWorkerConfig" {
			continue
		}
		raw, err := r.MarshalJSON()
		require.NoError(t, err)

		var cfg bootstrap.K0sWorkerConfig
		require.NoError(t, yaml.Unmarshal(raw, &cfg))
		require.Len(t, cfg.Spec.Files, 1)

		f := cfg.Spec.Files[0]
		require.Equal(t, testFilePath, f.Path)
		require.Equal(t, testFileOwner, f.Owner)
		require.Equal(t, testFilePermissions, f.Permissions)
		require.EqualValues(t, "base64", f.Encoding)

		decoded, err := f.DecodedContent()
		require.NoError(t, err)
		require.Equal(t, testFileContent, string(decoded))
		found = true
	}
	require.True(t, found, "no K0sWorkerConfig in the rendered manifest")
}
