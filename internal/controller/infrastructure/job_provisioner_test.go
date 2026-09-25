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
	"os/exec"
	"strings"
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
	// Quoted for the target, then again for the shell running ssh.
	require.Contains(t, script, "ssh root@host "+shellQuote("chown -- 'etcd:etcd' '/etc/thing'"))
	require.Contains(t, script, "ssh root@host "+shellQuote("chmod 0640 '/etc/thing'"))
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
		require.Contains(t, script, shellQuote("chmod 0644 '/etc/thing'"))
		require.NotContains(t, script, "chmod ''")
	})

	t.Run("a mode with no leading zero still renders as octal", func(t *testing.T) {
		_, _, secretData, err := newJobProvisioner().extractCloudInit(&provisioner.InputProvisionData{
			Files: []provisioner.File{{Path: "/etc/thing", Content: "body", Permissions: "755"}},
		})
		require.NoError(t, err)

		require.Contains(t, string(secretData["k0smotron-entrypoint.sh"]), shellQuote("chmod 0755 '/etc/thing'"))
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
	require.Contains(t, script, shellQuote("chown -- "+shellQuote("root; rm -rf /")+" '/etc/thing'"))
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
			require.Contains(t, script, "ssh root@host "+shellQuote("sudo chown -- 'etcd:etcd' '/etc/thing'"))
			require.Contains(t, script, "ssh root@host "+shellQuote("sudo chmod 0640 '/etc/thing'"))
		} else {
			require.Contains(t, script, "ssh root@host "+shellQuote("chown -- 'etcd:etcd' '/etc/thing'"))
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

// Two shells parse a line before the target runs it, so both run here.
func remoteArgv(t *testing.T, script string, call int) []string {
	t.Helper()

	// ssh reports its arguments, one per line, a blank line between calls.
	out := runSh(t, "scp() { :; }\nssh() { printf '%s\\n' \"$@\"; echo; }\n"+script)

	calls := strings.Split(strings.TrimRight(out, "\n"), "\n\n")
	require.Greater(t, len(calls), call, "the entrypoint made fewer ssh calls than that")

	argv := strings.Split(calls[call], "\n")
	require.Len(t, argv, 2, "ssh must get the destination and the whole command as one word each")

	// The target's login shell parses that word again.
	return strings.Split(runSh(t, "set -- "+argv[1]+"; printf '%s\\n' \"$@\""), "\n")
}

func runSh(t *testing.T, script string) string {
	t.Helper()

	out, err := exec.Command("sh", "-c", script).Output()
	require.NoError(t, err, "the generated entrypoint has to be valid shell")

	return strings.TrimRight(string(out), "\n")
}

func TestExtractCloudInitSurvivesTheTargetShellParse(t *testing.T) {
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
			Path:    "/etc/a b; touch /tmp/pwned",
			Content: "body",
			Owner:   "root; rm -rf /",
		}},
		Commands: []string{"echo $HOME `hostname`"},
	})
	require.NoError(t, err)

	script := string(secretData["k0smotron-entrypoint.sh"])

	require.Equal(t, []string{"chmod", "0644", "/etc/a b; touch /tmp/pwned"}, remoteArgv(t, script, 0),
		"the path has to reach the target as one argument rather than as a second command")
	require.Equal(t, []string{"chown", "--", "root; rm -rf /", "/etc/a b; touch /tmp/pwned"}, remoteArgv(t, script, 1),
		"the owner has to reach the target as one argument")

	// A command arrives whole, for the target's shell to expand.
	require.Contains(t, script, "ssh root@host "+shellQuote("echo $HOME `hostname`"))
}
