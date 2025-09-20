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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/k0sproject/k0smotron/internal/featuregate"
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
	featuregate.Configure("CloudInitVars=true")

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
			"myvar": "myvalue",
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
{% set myvar = "myvalue" %}
{% set k0smotron_files = [
  {
    "path": "/etc/hosts",
    "content": "foobar",
    "permissions": "0644"
  }
] %}
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
