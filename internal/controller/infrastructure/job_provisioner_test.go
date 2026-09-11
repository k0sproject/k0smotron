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

package infrastructure

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

func TestExtractCloudInitDecodesAndChowns(t *testing.T) {
	p := &JobProvisioner{
		remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root"}},
		provisionJob: &api.ProvisionJob{
			SSHCommand:  "ssh",
			SCPCommand:  "scp",
			JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
		},
	}

	_, _, secretData, err := p.extractCloudInit(&provisioner.InputProvisionData{
		Files: []provisioner.File{{
			Path:        "/etc/thing",
			Content:     base64.StdEncoding.EncodeToString([]byte("decoded body")),
			Encoding:    provisioner.Base64,
			Permissions: "0640",
			Owner:       "etcd:etcd",
		}},
	})
	require.NoError(t, err)

	var staged string
	for name, data := range secretData {
		if name != "k0smotron-entrypoint.sh" {
			staged = string(data)
		}
	}
	require.Equal(t, "decoded body", staged, "content must be decoded before it is staged")

	script := string(secretData["k0smotron-entrypoint.sh"])
	// The job entrypoint is a shell script, so the path and the owner must be quoted.
	require.Contains(t, script, "chown -- 'etcd:etcd' '/etc/thing'")
	require.Contains(t, script, "chmod 0640 '/etc/thing'")
}

func TestExtractCloudInitFileMode(t *testing.T) {
	newJobProvisioner := func() *JobProvisioner {
		return &JobProvisioner{
			remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root"}},
			provisionJob: &api.ProvisionJob{
				SSHCommand:  "ssh",
				SCPCommand:  "scp",
				JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
			},
		}
	}

	t.Run("an unset mode falls back instead of emitting an empty one", func(t *testing.T) {
		_, _, secretData, err := newJobProvisioner().extractCloudInit(&provisioner.InputProvisionData{
			Files: []provisioner.File{{Path: "/etc/thing", Content: "body"}},
		})
		require.NoError(t, err)

		script := string(secretData["k0smotron-entrypoint.sh"])
		require.Contains(t, script, "chmod 0644 '/etc/thing'")
		require.NotContains(t, script, "chmod ''")
	})

	t.Run("a mode with no leading zero still renders as octal", func(t *testing.T) {
		_, _, secretData, err := newJobProvisioner().extractCloudInit(&provisioner.InputProvisionData{
			Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Permissions: "755"}},
		})
		require.NoError(t, err)

		require.Contains(t, string(secretData["k0smotron-entrypoint.sh"]), "chmod 0755 '/etc/thing'")
	})

	t.Run("an unparseable mode is reported against the file", func(t *testing.T) {
		_, _, _, err := newJobProvisioner().extractCloudInit(&provisioner.InputProvisionData{
			Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Permissions: "rw-r--r--"}},
		})
		require.ErrorContains(t, err, "failed to parse permissions of file /etc/thing")
	})
}

func TestExtractCloudInitQuotesOwnerAgainstInjection(t *testing.T) {
	p := &JobProvisioner{
		remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root"}},
		provisionJob: &api.ProvisionJob{
			SSHCommand:  "ssh",
			SCPCommand:  "scp",
			JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
		},
	}

	_, _, secretData, err := p.extractCloudInit(&provisioner.InputProvisionData{
		Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Owner: "root; rm -rf /"}},
	})
	require.NoError(t, err)

	script := string(secretData["k0smotron-entrypoint.sh"])
	require.Contains(t, script, "chown -- "+shellQuote("root; rm -rf /"))
	require.NotContains(t, script, "chown -- root; rm")
}

func TestExtractCloudInitQuotesScpDestination(t *testing.T) {
	p := &JobProvisioner{
		remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root"}},
		provisionJob: &api.ProvisionJob{
			SSHCommand:  "ssh",
			SCPCommand:  "scp",
			JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
		},
	}

	_, _, secretData, err := p.extractCloudInit(&provisioner.InputProvisionData{
		Files: []provisioner.File{{Path: "/etc/a b; touch /tmp/pwned", Content: "body"}},
	})
	require.NoError(t, err)

	script := string(secretData["k0smotron-entrypoint.sh"])
	require.Contains(t, script, " "+shellQuote("root@host:/etc/a b; touch /tmp/pwned")+"\n")
	require.NotContains(t, script, " root@host:", "an unquoted destination lets the path split the scp command")
}

func TestExtractCloudInitUsesSudoWhenRequested(t *testing.T) {
	// Giving a file to another user needs privilege, the same way the commands
	// in the same script get it.
	for _, useSudo := range []bool{false, true} {
		p := &JobProvisioner{
			remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root", UseSudo: useSudo}},
			provisionJob: &api.ProvisionJob{
				SSHCommand:  "ssh",
				SCPCommand:  "scp",
				JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
			},
		}

		_, _, secretData, err := p.extractCloudInit(&provisioner.InputProvisionData{
			Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Permissions: "0640", Owner: "etcd:etcd"}},
		})
		require.NoError(t, err)

		script := string(secretData["k0smotron-entrypoint.sh"])
		if useSudo {
			require.Contains(t, script, "ssh root@host sudo chown -- 'etcd:etcd'")
			require.Contains(t, script, "ssh root@host sudo chmod 0640")
		} else {
			require.Contains(t, script, "ssh root@host chown -- 'etcd:etcd'")
			require.NotContains(t, script, "sudo chown")
		}
	}
}

func TestExtractCloudInitRejectsAppend(t *testing.T) {
	p := &JobProvisioner{
		remoteMachine: &api.RemoteMachine{Spec: api.RemoteMachineSpec{Address: "host", User: "root"}},
		provisionJob: &api.ProvisionJob{
			SSHCommand:  "ssh",
			SCPCommand:  "scp",
			JobTemplate: &batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "job"}},
		},
	}

	_, _, _, err := p.extractCloudInit(&provisioner.InputProvisionData{
		Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Append: true}},
	})

	require.ErrorContains(t, err, "not supported when provisioning through a job")
}
