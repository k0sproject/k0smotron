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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vincent-petithory/dataurl"
)

func normalizeJSON(t *testing.T, in []byte) []byte {
	var v any
	require.NoError(t, json.Unmarshal(in, &v))
	out, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return out
}

func TestToProvisionData(t *testing.T) {
	tests := []struct {
		name        string
		provisioner IgnitionProvisioner
		input       *InputProvisionData
		wantErr     bool
		wantJSON    string
	}{
		{
			name: "files only",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
			},
			input: &InputProvisionData{
				Files: []File{
					{Path: "/etc/test.conf", Content: "hello world", Permissions: "0644"},
				},
			},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL3Rlc3QuY29uZiIsCiAgICAgICAgImNvbnRlbnRzIjogewogICAgICAgICAgImNvbXByZXNzaW9uIjogIiIsCiAgICAgICAgICAic291cmNlIjogImRhdGE6LGhlbGxvJTIwd29ybGQiCiAgICAgICAgfSwKICAgICAgICAibW9kZSI6IDQyMAogICAgICB9CiAgICBdCiAgfQp9"
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "with commands",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
			},
			input: &InputProvisionData{
				Commands: []string{"echo hello"},
			},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN5c3RlbWQiOiB7CiAgICAidW5pdHMiOiBbCiAgICAgIHsKICAgICAgICAiY29udGVudHMiOiAiW1VuaXRdXG5EZXNjcmlwdGlvbj1LMHMgQm9vdHN0cmFwIENvbW1hbmRzXG5BZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXRcblxuW1NlcnZpY2VdXG5UeXBlPW9uZXNob3RcbkV4ZWNTdGFydD0vYmluL3NoIC1jICdlY2hvIGhlbGxvJ1xuUmVtYWluQWZ0ZXJFeGl0PXRydWVcblxuW0luc3RhbGxdXG5XYW50ZWRCeT1tdWx0aS11c2VyLnRhcmdldCIsCiAgICAgICAgImVuYWJsZWQiOiB0cnVlLAogICAgICAgICJuYW1lIjogImswcy1ib290c3RyYXAuc2VydmljZSIKICAgICAgfQogICAgXQogIH0KfQ=="
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "files + commands",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
			},
			input: &InputProvisionData{
				Files: []File{
					{Path: "/etc/combined.conf", Content: "combo", Permissions: "0644"},
				},
				Commands: []string{"echo combo"},
			},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL2NvbWJpbmVkLmNvbmYiLAogICAgICAgICJjb250ZW50cyI6IHsKICAgICAgICAgICJjb21wcmVzc2lvbiI6ICIiLAogICAgICAgICAgInNvdXJjZSI6ICJkYXRhOixjb21ibyIKICAgICAgICB9LAogICAgICAgICJtb2RlIjogNDIwCiAgICAgIH0KICAgIF0KICB9LAogICJzeXN0ZW1kIjogewogICAgInVuaXRzIjogWwogICAgICB7CiAgICAgICAgImNvbnRlbnRzIjogIltVbml0XVxuRGVzY3JpcHRpb249SzBzIEJvb3RzdHJhcCBDb21tYW5kc1xuQWZ0ZXI9bmV0d29yay1vbmxpbmUudGFyZ2V0XG5cbltTZXJ2aWNlXVxuVHlwZT1vbmVzaG90XG5FeGVjU3RhcnQ9L2Jpbi9zaCAtYyAnZWNobyBjb21ibydcblJlbWFpbkFmdGVyRXhpdD10cnVlXG5cbltJbnN0YWxsXVxuV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQiLAogICAgICAgICJlbmFibGVkIjogdHJ1ZSwKICAgICAgICAibmFtZSI6ICJrMHMtYm9vdHN0cmFwLnNlcnZpY2UiCiAgICAgIH0KICAgIF0KICB9Cn0="
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "invalid permissions",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
			},
			input: &InputProvisionData{
				Files: []File{
					{Path: "/etc/bad.conf", Content: "oops", Permissions: "not-a-mode"},
				},
			},
			wantErr: true,
		},
		{
			name: "with additional config",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `
variant: fcos
version: 1.0.0
systemd:
  units:
  - name: extra.service
    enabled: true
    contents: "echo extra"`,
			},
			input: &InputProvisionData{},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0KfQ=="
              },
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN5c3RlbWQiOiB7CiAgICAidW5pdHMiOiBbCiAgICAgIHsKICAgICAgICAiY29udGVudHMiOiAiZWNobyBleHRyYSIsCiAgICAgICAgImVuYWJsZWQiOiB0cnVlLAogICAgICAgICJuYW1lIjogImV4dHJhLnNlcnZpY2UiCiAgICAgIH0KICAgIF0KICB9Cn0="
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "error: with additional config but different versions",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.2.0",
				AdditionalConfig: `
variant: fcos
version: 1.1.0
systemd:
  units:
  - name: extra.service
    enabled: true
    contents: "echo extra"`,
			},
			input:   &InputProvisionData{},
			wantErr: true,
		},
		{
			name: "additional config with file",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `
variant: fcos
version: 1.0.0
storage:
  files:
  - path: /etc/extra.conf
    mode: 420
    contents:
      inline: "from additional config"`,
			},
			input: &InputProvisionData{},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0KfQ=="
              },
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL2V4dHJhLmNvbmYiLAogICAgICAgICJjb250ZW50cyI6IHsKICAgICAgICAgICJzb3VyY2UiOiAiZGF0YTosZnJvbSUyMGFkZGl0aW9uYWwlMjBjb25maWciCiAgICAgICAgfSwKICAgICAgICAibW9kZSI6IDQyMAogICAgICB9CiAgICBdCiAgfQp9"
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "files + additional config",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `
variant: fcos
version: 1.0.0
storage:
  files:
  - path: /etc/extra.conf
    mode: 420
    contents:
    inline: "from additional config"`,
			},
			input: &InputProvisionData{
				Files: []File{
					{Path: "/etc/test.conf", Content: "hello world", Permissions: "0644"},
				},
			},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL3Rlc3QuY29uZiIsCiAgICAgICAgImNvbnRlbnRzIjogewogICAgICAgICAgImNvbXByZXNzaW9uIjogIiIsCiAgICAgICAgICAic291cmNlIjogImRhdGE6LGhlbGxvJTIwd29ybGQiCiAgICAgICAgfSwKICAgICAgICAibW9kZSI6IDQyMAogICAgICB9CiAgICBdCiAgfQp9"
              },
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL2V4dHJhLmNvbmYiLAogICAgICAgICJtb2RlIjogNDIwCiAgICAgIH0KICAgIF0KICB9Cn0="
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "commands + additional config unit",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `
variant: fcos
version: 1.0.0
systemd:
  units:
  - name: extra.service
    enabled: true
    contents: "echo extra"`,
			},
			input: &InputProvisionData{
				Commands: []string{"echo hello"},
			},
			wantJSON: `
      {
        "ignition": {
          "config": {
            "merge": [
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN5c3RlbWQiOiB7CiAgICAidW5pdHMiOiBbCiAgICAgIHsKICAgICAgICAiY29udGVudHMiOiAiW1VuaXRdXG5EZXNjcmlwdGlvbj1LMHMgQm9vdHN0cmFwIENvbW1hbmRzXG5BZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXRcblxuW1NlcnZpY2VdXG5UeXBlPW9uZXNob3RcbkV4ZWNTdGFydD0vYmluL3NoIC1jICdlY2hvIGhlbGxvJ1xuUmVtYWluQWZ0ZXJFeGl0PXRydWVcblxuW0luc3RhbGxdXG5XYW50ZWRCeT1tdWx0aS11c2VyLnRhcmdldCIsCiAgICAgICAgImVuYWJsZWQiOiB0cnVlLAogICAgICAgICJuYW1lIjogImswcy1ib290c3RyYXAuc2VydmljZSIKICAgICAgfQogICAgXQogIH0KfQ=="
              },
              {
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN5c3RlbWQiOiB7CiAgICAidW5pdHMiOiBbCiAgICAgIHsKICAgICAgICAiY29udGVudHMiOiAiZWNobyBleHRyYSIsCiAgICAgICAgImVuYWJsZWQiOiB0cnVlLAogICAgICAgICJuYW1lIjogImV4dHJhLnNlcnZpY2UiCiAgICAgIH0KICAgIF0KICB9Cn0="
              }
            ]
          },
          "version": "3.0.0"
        }
      }`,
		},
		{
			name: "invalid additional config YAML",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `
variant: fcos
version: 1.0.0
systemd: [invalid_yaml_here`,
			},
			input:   &InputProvisionData{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.provisioner.ToProvisionData(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			wantNorm := normalizeJSON(t, []byte(tt.wantJSON))
			gotNorm := normalizeJSON(t, got)

			if !bytes.Equal(gotNorm, wantNorm) {
				t.Errorf("unexpected JSON:\n--- want ---\n%s\n--- got ---\n%s", wantNorm, gotNorm)
			}
		})
	}
}

// decodeFirstMergeSource returns the inner Ignition config embedded as the
// first merge source, so assertions can read as JSON rather than base64.
func decodeFirstMergeSource(t *testing.T, out []byte) []byte {
	t.Helper()

	var outer struct {
		Ignition struct {
			Config struct {
				Merge []struct {
					Source string `json:"source"`
				} `json:"merge"`
			} `json:"config"`
		} `json:"ignition"`
	}
	require.NoError(t, json.Unmarshal(out, &outer))
	require.NotEmpty(t, outer.Ignition.Config.Merge)

	const prefix = "data:application/json;base64,"
	src := outer.Ignition.Config.Merge[0].Source
	require.True(t, strings.HasPrefix(src, prefix), "unexpected merge source %q", src)

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(src, prefix))
	require.NoError(t, err)
	return raw
}

// fileEntry pulls a single storage.files entry out of the config that
// ToProvisionData embeds as its first merge source.
func fileEntry(t *testing.T, out []byte) map[string]any {
	t.Helper()

	var inner struct {
		Storage struct {
			Files []map[string]any `json:"files"`
		} `json:"storage"`
	}
	require.NoError(t, json.Unmarshal(decodeFirstMergeSource(t, out), &inner))
	require.Len(t, inner.Storage.Files, 1)
	return inner.Storage.Files[0]
}

// bodyBytes decodes the data URL that carries a file body, so a test can assert
// on the bytes the node would actually write.
func bodyBytes(t *testing.T, resource map[string]any) []byte {
	t.Helper()

	src, ok := resource["source"].(string)
	require.True(t, ok, "resource has no source: %v", resource)

	decoded, err := dataurl.DecodeString(src)
	require.NoError(t, err)
	return decoded.Data
}

func TestIgnitionFileOwnerAndAppend(t *testing.T) {
	t.Run("owner becomes user and group", func(t *testing.T) {
		out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(
			&InputProvisionData{Files: []File{{Path: "/a", Content: "x", Permissions: "0600", Owner: "etcd:etcd"}}})
		require.NoError(t, err)

		entry := fileEntry(t, out)
		require.Equal(t, map[string]any{"name": "etcd"}, entry["user"])
		require.Equal(t, map[string]any{"name": "etcd"}, entry["group"])
	})

	t.Run("owner without a group sets only user", func(t *testing.T) {
		out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(
			&InputProvisionData{Files: []File{{Path: "/a", Content: "x", Permissions: "0644", Owner: "nobody"}}})
		require.NoError(t, err)

		entry := fileEntry(t, out)
		require.Equal(t, map[string]any{"name": "nobody"}, entry["user"])
		require.NotContains(t, entry, "group")
	})

	t.Run("append populates the append list and leaves contents unset", func(t *testing.T) {
		out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(
			&InputProvisionData{Files: []File{{Path: "/a", Content: "tail\n", Permissions: "0644", Append: true}}})
		require.NoError(t, err)

		entry := fileEntry(t, out)
		require.NotContains(t, entry, "contents")

		list, ok := entry["append"].([]any)
		require.True(t, ok, "append is not a list: %v", entry["append"])
		require.Len(t, list, 1)
		require.Equal(t, "tail\n", string(bodyBytes(t, list[0].(map[string]any))))
	})
}

// TestIgnitionAppendContentRoundTrip guards the append path, which is new and
// carries a data URL, so any byte sequence survives.
func TestIgnitionAppendContentRoundTrip(t *testing.T) {
	bodies := map[string]string{
		"plain":                  "hello world",
		"trailing newline":       "tail\n",
		"leading newline":        "\nfoo\n",
		"only a newline":         "\n",
		"several newlines":       "\n\n\n",
		"leading space":          " foo\n",
		"leading tab":            "\thi\n",
		"indented yaml fragment": "  extraArgs:\n    foo: bar\n",
		"systemd drop in":        "\n[Service]\nEnvironment=FOO=bar\n",
		"hosts comment block":    "\n# added by k0smotron\n10.0.0.1 api\n",
		"non ascii":              "café München\n",
		"carriage return":        "a\r\nb\n",
		"nul and high bytes":     "\x00\xff\x1b",
	}

	for _, version := range []string{"1.0.0", "1.5.0"} {
		for name, body := range bodies {
			t.Run(version+" "+name, func(t *testing.T) {
				out, err := (&IgnitionProvisioner{Variant: "fcos", Version: version}).ToProvisionData(
					&InputProvisionData{Files: []File{{Path: "/a", Content: body, Permissions: "0644", Append: true}}})
				require.NoError(t, err)

				entry := fileEntry(t, out)
				require.NotContains(t, entry, "contents")

				list, ok := entry["append"].([]any)
				require.True(t, ok, "append is not a list")
				require.Len(t, list, 1)
				require.Equal(t, body, string(bodyBytes(t, list[0].(map[string]any))))
			})
		}
	}
}

// TestIgnitionReplaceUsesContents pins the shape of a plain replace, a contents
// entry with no append and no compression requested.
func TestIgnitionReplaceUsesContents(t *testing.T) {
	out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(
		&InputProvisionData{Files: []File{{Path: "/a", Content: "hello world", Permissions: "0644"}}})
	require.NoError(t, err)

	entry := fileEntry(t, out)
	require.NotContains(t, entry, "append")

	contents, ok := entry["contents"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "", contents["compression"], "no compression is requested for a file body")
	require.Equal(t, "hello world", string(bodyBytes(t, contents)))
}

func TestIgnitionEncodedContentIsDecodedOnce(t *testing.T) {
	plain := "  indented: true\n"

	for name, f := range map[string]File{
		"base64": {Path: "/a", Permissions: "0644", Encoding: Base64, Content: base64.StdEncoding.EncodeToString([]byte(plain))},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(&InputProvisionData{Files: []File{f}})
			require.NoError(t, err)

			entry := fileEntry(t, out)
			require.Equal(t, plain, string(bodyBytes(t, entry["contents"].(map[string]any))))
		})
	}
}

// TestIgnitionOpenshiftVariantCannotRender records that the API accepts a variant the
// provisioner can never translate, since openshift wants a MachineConfig wrapper.
func TestIgnitionOpenshiftVariantCannotRender(t *testing.T) {
	_, err := (&IgnitionProvisioner{Variant: "openshift", Version: "4.19.0"}).ToProvisionData(
		&InputProvisionData{Files: []File{{Path: "/a", Content: "x", Permissions: "0644"}}})

	require.ErrorContains(t, err, "error translating butane config")
}

func TestIgnitionRejectsUndecodableContent(t *testing.T) {
	p := &IgnitionProvisioner{Variant: "fcos", Version: "1.0.0"}
	_, err := p.ToProvisionData(&InputProvisionData{
		Files: []File{{Path: "/a", Content: "!!!", Permissions: "0644", Encoding: Base64}},
	})
	require.ErrorContains(t, err, "failed to base64 decode")
}

// TestIgnitionPreservesContentYAMLCannotCarry covers what the old inline scalar got
// wrong, a leading newline and a leading tab. The rest round tripped and stay as guards.
func TestIgnitionPreservesContentYAMLCannotCarry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"a leading newline", "\nfoo\n"},
		{"nothing but a newline", "\n"},
		{"a leading tab", "\tindented\n"},
		{"a leading space", "  indented\n"},
		{"trailing whitespace", "foo   \n"},
		{"crlf", "a\r\nb\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := (&IgnitionProvisioner{Variant: "fcos", Version: "1.5.0"}).ToProvisionData(
				&InputProvisionData{Files: []File{{Path: "/a", Content: tc.content, Permissions: "0644"}}})
			require.NoError(t, err)

			entry := fileEntry(t, out)
			require.Equal(t, tc.content, string(bodyBytes(t, entry["contents"].(map[string]any))))
		})
	}
}

// TestIgnitionPreservesContentAcrossSpecVersions covers the same body on every stable
// variant and version Butane registers, apart from openshift, which cannot render here.
func TestIgnitionPreservesContentAcrossSpecVersions(t *testing.T) {
	for _, v := range []struct{ variant, version string }{
		{"fcos", "1.0.0"}, {"fcos", "1.1.0"}, {"fcos", "1.2.0"}, {"fcos", "1.3.0"},
		{"fcos", "1.4.0"}, {"fcos", "1.5.0"}, {"fcos", "1.6.0"},
		{"flatcar", "1.0.0"}, {"flatcar", "1.1.0"},
		{"fiot", "1.0.0"},
		{"r4e", "1.0.0"}, {"r4e", "1.1.0"},
	} {
		t.Run(v.variant+" "+v.version, func(t *testing.T) {
			out, err := (&IgnitionProvisioner{Variant: v.variant, Version: v.version}).ToProvisionData(
				&InputProvisionData{Files: []File{{Path: "/a", Content: "\n\tboth\n", Permissions: "0644"}}})
			require.NoError(t, err)

			entry := fileEntry(t, out)
			require.Equal(t, "\n\tboth\n", string(bodyBytes(t, entry["contents"].(map[string]any))))
		})
	}
}
