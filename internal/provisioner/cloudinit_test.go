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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/k0sproject/k0smotron/v2/internal/featuregate"
)

func TestCloudInit(t *testing.T) {
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

	p := &CloudInitProvisioner{}

	b, err := p.ToProvisionData(c)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	assert.Equal(t, `## template: jinja
#cloud-config
write_files:
  - path: /etc/hosts
    content: foobar
    permissions: "0644"
runcmd:
  - echo 'hello world'
`, s)
}

func TestCustomCloudInit(t *testing.T) {
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
		CustomUserData: `runcmd:
  - echo 'custom cloud init'
`,
	}

	p := &CloudInitProvisioner{}

	b, err := p.ToProvisionData(c)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)
	assert.Equal(t, `## template: jinja
#cloud-config
write_files:
  - path: /etc/hosts
    content: foobar
    permissions: "0644"
runcmd:
  - echo 'hello world'

#cloud-config
runcmd:
  - echo 'custom cloud init'
`, s)
}

func TestCustomCloudInitWithVars(t *testing.T) {
	withCloudInitVars(t, true)

	input := &InputProvisionData{
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
		Vars: map[VarName]string{
			"myvar": `myvalue "withquotes"`,
		},
		CustomUserData: `runcmd:
  - echo 'custom cloud init'
`,
	}
	p := &CloudInitProvisioner{}
	b, err := p.ToProvisionData(input)
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)

	assert.Equal(t, `## template: jinja
#cloud-config
{% set myvar = 'myvalue \"withquotes\"' %}
{% set k0smotron_files = [
  {
    "path": "/etc/hosts",
    "content": "foobar",
    "permissions": "0644"
  }
] %}
runcmd:
  - echo 'custom cloud init'
`, s)
}

func TestPermissions(t *testing.T) {
	f := File{
		Path:        "/etc/hosts",
		Content:     "foobar",
		Permissions: "0644",
	}

	perm, err := f.PermissionsAsInt()
	assert.NoError(t, err)
	assert.Equal(t, int64(420), perm)
}

// withCloudInitVars sets the gate for one test and puts it back afterwards.
func withCloudInitVars(t *testing.T, enabled bool) {
	t.Helper()

	before := featuregate.IsEnabled(featuregate.CloudInitVars)
	t.Cleanup(func() {
		_ = featuregate.Configure(fmt.Sprintf("CloudInitVars=%t", before), "")
	})

	require.NoError(t, featuregate.Configure(fmt.Sprintf("CloudInitVars=%t", enabled), ""))
}

func TestCloudInitPassesFileOwnerEncodingAndAppendThrough(t *testing.T) {
	// cloud init decodes and applies these itself, so they must reach
	// write_files verbatim rather than being interpreted here.
	withCloudInitVars(t, false)

	input := &InputProvisionData{
		Files: []File{
			{
				Path:        "/etc/hosts",
				Content:     "Zm9vYmFy",
				Permissions: "0600",
				Owner:       "root:root",
				Encoding:    Base64,
				Append:      true,
			},
		},
	}

	b, err := (&CloudInitProvisioner{}).ToProvisionData(input)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `## template: jinja
#cloud-config
write_files:
  - path: /etc/hosts
    content: Zm9vYmFy
    permissions: "0600"
    owner: root:root
    encoding: base64
    append: true
runcmd: []
`, string(b))
}

func TestCloudInitVarsOmitsUnsetFileFields(t *testing.T) {
	withCloudInitVars(t, true)

	input := &InputProvisionData{
		Files: []File{
			// Multi line content guards against the body being escaped twice,
			// which would reach the host as a literal backslash n.
			{Path: "/a", Content: "line1\nline2\n", Permissions: "0644"},
			{Path: "/b", Content: "Zm9v", Permissions: "0600", Owner: "etcd:etcd", Encoding: Base64, Append: true},
		},
		Vars: map[VarName]string{"v": "1"},
	}

	b, err := (&CloudInitProvisioner{}).ToProvisionData(input)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `## template: jinja
#cloud-config
{% set v = '1' %}
{% set k0smotron_files = [
  {
    "path": "/a",
    "content": "line1\nline2\n",
    "permissions": "0644"
  },
  {
    "path": "/b",
    "content": "Zm9v",
    "permissions": "0600",
    "owner": "etcd:etcd",
    "encoding": "base64",
    "append": true
  }
] %}
`, string(b))
}
