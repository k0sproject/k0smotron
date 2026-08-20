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
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/pkg/rigfs"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/provisioner"
)

// fakeFile records what was written to it.
type fakeFile struct {
	buf bytes.Buffer
}

func (f *fakeFile) Write(p []byte) (int, error)           { return f.buf.Write(p) }
func (f *fakeFile) Read(_ []byte) (int, error)            { return 0, io.EOF }
func (f *fakeFile) Close() error                          { return nil }
func (f *fakeFile) Stat() (fs.FileInfo, error)            { return fakeInfo{name: "file"}, nil }
func (f *fakeFile) Seek(_ int64, _ int) (int64, error)    { return 0, nil }
func (f *fakeFile) CopyFrom(src io.Reader) (int64, error) { return io.Copy(&f.buf, src) }
func (f *fakeFile) CopyTo(dst io.Writer) (int64, error)   { return io.Copy(dst, &f.buf) }

// fakeInfo stands in for a real FileInfo so the fakes never hand back a nil
// one, which the io/fs contract forbids.
type fakeInfo struct {
	name string
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return 0o755 | fs.ModeDir }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return true }
func (i fakeInfo) Sys() any           { return nil }

// fakeFsys is the smallest rigfs.Fsys that uploadFile exercises.
type fakeFsys struct {
	file      *fakeFile
	openFlag  int
	openPerm  fs.FileMode
	openPath  string
	mkdirPath string
	mkdirPerm fs.FileMode
	dirExists bool
}

func (f *fakeFsys) OpenFile(path string, flag int, perm fs.FileMode) (rigfs.File, error) {
	f.openPath, f.openFlag, f.openPerm = path, flag, perm
	f.file = &fakeFile{}
	return f.file, nil
}

func (f *fakeFsys) MkDirAll(path string, perm fs.FileMode) error {
	f.mkdirPath, f.mkdirPerm = path, perm
	return nil
}

func (f *fakeFsys) Stat(name string) (fs.FileInfo, error) {
	if f.dirExists {
		return fakeInfo{name: name}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (f *fakeFsys) Open(_ string) (fs.File, error)  { return nil, nil }
func (f *fakeFsys) Sha256(_ string) (string, error) { return "", nil }
func (f *fakeFsys) Remove(_ string) error           { return nil }
func (f *fakeFsys) RemoveAll(_ string) error        { return nil }

// fakeRunner records the commands uploadFile asks the host to run, how many
// exec options came with them, and what the file looked like at that moment.
type fakeRunner struct {
	fsys       *fakeFsys
	cmds       []string
	optCounts  []int
	bodyAtExec []string
	err        error
}

func (r *fakeRunner) ExecOutput(cmd string, opts ...exec.Option) (string, error) {
	r.cmds = append(r.cmds, cmd)
	r.optCounts = append(r.optCounts, len(opts))

	body := ""
	if r.fsys != nil && r.fsys.file != nil {
		body = r.fsys.file.buf.String()
	}
	r.bodyAtExec = append(r.bodyAtExec, body)

	return "", r.err
}

func newProvisioner() *SSHProvisioner {
	return &SSHProvisioner{log: logr.Discard()}
}

func TestUploadFileWritesDecodedContent(t *testing.T) {
	fsys, runner := &fakeFsys{}, &fakeRunner{}

	err := newProvisioner().uploadFile(runner, nil, fsys, provisioner.File{
		Path:        "/etc/thing",
		Content:     base64.StdEncoding.EncodeToString([]byte("secret body")),
		Encoding:    provisioner.Base64,
		Permissions: "0600",
	})
	require.NoError(t, err)

	require.Equal(t, "secret body", fsys.file.buf.String(), "content must be decoded before it is written")
	require.Equal(t, "/etc/thing", fsys.openPath)
	require.Equal(t, fs.FileMode(0o600), fsys.openPerm)
	require.Empty(t, runner.cmds, "no owner means no chown")
}

func TestUploadFileTruncates(t *testing.T) {
	fsys := &fakeFsys{}

	err := newProvisioner().uploadFile(&fakeRunner{}, nil, fsys, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0644",
	})
	require.NoError(t, err)

	require.Equal(t, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fsys.openFlag)
	require.Zero(t, fsys.openFlag&os.O_APPEND)
}

func TestUploadFileRejectsAppend(t *testing.T) {
	fsys, runner := &fakeFsys{}, &fakeRunner{}

	err := newProvisioner().uploadFile(runner, nil, fsys, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0644", Append: true,
	})

	require.ErrorContains(t, err, "not supported when provisioning over ssh")
	require.Nil(t, fsys.file, "nothing should be written when the file is rejected")
	require.Empty(t, runner.cmds)
}

// TestUploadFileCreatesDirectoryTraversable covers the edge case where a
// restrictive file mode would have left the parent directory unusable.
func TestUploadFileCreatesDirectoryTraversable(t *testing.T) {
	// Reusing the file mode gave a directory with no execute bit for modes
	// like 0600, so the owner of the file could not reach it.
	for _, permissions := range []string{"", "0600", "0400", "0640", "0644", "0700", "0755"} {
		fsys := &fakeFsys{}

		err := newProvisioner().uploadFile(&fakeRunner{}, nil, fsys, provisioner.File{
			Path: "/var/lib/k0s/thing", Content: "body", Permissions: permissions,
		})
		require.NoError(t, err, "permissions %q", permissions)

		require.Equal(t, "/var/lib/k0s", fsys.mkdirPath, "permissions %q", permissions)
		require.Equal(t, fs.FileMode(0o755), fsys.mkdirPerm,
			"the directory mode must not follow the file mode %q", permissions)

		// The file itself still gets exactly what was asked for.
		want := fs.FileMode(0o644)
		if permissions != "" {
			parsed, err := provisioner.File{Permissions: permissions}.PermissionsAsInt()
			require.NoError(t, err)
			want = fs.FileMode(parsed)
		}
		require.Equal(t, want, fsys.openPerm, "file mode for permissions %q", permissions)
	}
}

func TestUploadFileChownsQuotedAndAfterTheWrite(t *testing.T) {
	fsys := &fakeFsys{}
	runner := &fakeRunner{fsys: fsys}

	err := newProvisioner().uploadFile(runner, nil, fsys, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0640", Owner: "etcd:etcd",
	})
	require.NoError(t, err)

	require.Equal(t, []string{"chown -- 'etcd:etcd' '/etc/thing'"}, runner.cmds)

	// rig creates the file on open, so a chown before the write would target a
	// path that does not exist yet.
	require.Equal(t, []string{"body"}, runner.bodyAtExec,
		"the content must already be written when the chown runs")
}

func TestUploadFileSkipsMkdirWhenParentExists(t *testing.T) {
	fsys := &fakeFsys{dirExists: true}

	err := newProvisioner().uploadFile(&fakeRunner{}, nil, fsys, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0644",
	})
	require.NoError(t, err)

	require.Empty(t, fsys.mkdirPath, "an existing parent must not be recreated")
}

func TestUploadFileChownInheritsExecOptions(t *testing.T) {
	// Without these the chown runs unprivileged on every machine that asked
	// for sudo.
	fsys := &fakeFsys{}
	runner := &fakeRunner{fsys: fsys}
	execOpts := []exec.Option{exec.HideOutput()}

	err := newProvisioner().uploadFile(runner, execOpts, fsys, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0644", Owner: "nobody",
	})
	require.NoError(t, err)

	require.Equal(t, []int{len(execOpts)}, runner.optCounts)
}

func TestUploadFileChownQuotingResistsInjection(t *testing.T) {
	// The webhook rejects these, but the objects it does not cover reach here,
	// so the quoting has to hold on its own.
	for _, owner := range []string{"root; rm -rf /", "$(id -u)", "a'b", "root\nid"} {
		runner := &fakeRunner{}

		err := newProvisioner().uploadFile(runner, nil, &fakeFsys{}, provisioner.File{
			Path: "/etc/thing", Content: "body", Permissions: "0644", Owner: owner,
		})
		require.NoError(t, err)
		require.Len(t, runner.cmds, 1)

		quoted := shellQuote(owner)
		require.Equal(t, "chown -- "+quoted+" '/etc/thing'", runner.cmds[0])

		// The payload is still present, but inert. What matters is that it is
		// wrapped and that every embedded quote is escaped.
		require.True(t, strings.HasPrefix(quoted, "'"))
		require.True(t, strings.HasSuffix(quoted, "'"))
		inner := strings.ReplaceAll(quoted[1:len(quoted)-1], `'\''`, "")
		require.NotContains(t, inner, "'", "a bare quote would end the wrapper early")
	}
}

func TestUploadFileSurfacesChownFailure(t *testing.T) {
	runner := &fakeRunner{err: errors.New("operation not permitted")}

	err := newProvisioner().uploadFile(runner, nil, &fakeFsys{}, provisioner.File{
		Path: "/etc/thing", Content: "body", Permissions: "0644", Owner: "nobody",
	})

	require.ErrorContains(t, err, "failed to set owner")
	require.ErrorContains(t, err, "nobody")
}

func TestUploadFileRejectsUndecodableContent(t *testing.T) {
	fsys := &fakeFsys{}

	err := newProvisioner().uploadFile(&fakeRunner{}, nil, fsys, provisioner.File{
		Path: "/etc/thing", Content: "!!!", Encoding: provisioner.Base64, Permissions: "0644",
	})

	require.ErrorContains(t, err, "failed to base64 decode")
	require.Nil(t, fsys.file, "nothing should be written when content cannot be decoded")
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"etcd":           `'etcd'`,
		"etcd:etcd":      `'etcd:etcd'`,
		"a'b":            `'a'\''b'`,
		"root; rm -rf /": `'root; rm -rf /'`,
		"$(id)":          `'$(id)'`,
	} {
		require.Equal(t, want, shellQuote(in), "quoting %q", in)
	}
}

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
	// The job entrypoint is a shell script, so both must be quoted.
	require.Contains(t, script, "chown -- 'etcd:etcd' '/etc/thing'")
	require.Contains(t, script, "chmod '0640' '/etc/thing'")
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
			require.Contains(t, script, "ssh root@host sudo chmod '0640'")
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
