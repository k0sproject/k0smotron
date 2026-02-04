package provisioner

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL3Rlc3QuY29uZiIsCiAgICAgICAgImNvbnRlbnRzIjogewogICAgICAgICAgInNvdXJjZSI6ICJkYXRhOixoZWxsbyUyMHdvcmxkIgogICAgICAgIH0sCiAgICAgICAgIm1vZGUiOiA0MjAKICAgICAgfQogICAgXQogIH0KfQ=="
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
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL2NvbWJpbmVkLmNvbmYiLAogICAgICAgICJjb250ZW50cyI6IHsKICAgICAgICAgICJzb3VyY2UiOiAiZGF0YTosY29tYm8iCiAgICAgICAgfSwKICAgICAgICAibW9kZSI6IDQyMAogICAgICB9CiAgICBdCiAgfSwKICAic3lzdGVtZCI6IHsKICAgICJ1bml0cyI6IFsKICAgICAgewogICAgICAgICJjb250ZW50cyI6ICJbVW5pdF1cbkRlc2NyaXB0aW9uPUswcyBCb290c3RyYXAgQ29tbWFuZHNcbkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldFxuXG5bU2VydmljZV1cblR5cGU9b25lc2hvdFxuRXhlY1N0YXJ0PS9iaW4vc2ggLWMgJ2VjaG8gY29tYm8nXG5SZW1haW5BZnRlckV4aXQ9dHJ1ZVxuXG5bSW5zdGFsbF1cbldhbnRlZEJ5PW11bHRpLXVzZXIudGFyZ2V0IiwKICAgICAgICAiZW5hYmxlZCI6IHRydWUsCiAgICAgICAgIm5hbWUiOiAiazBzLWJvb3RzdHJhcC5zZXJ2aWNlIgogICAgICB9CiAgICBdCiAgfQp9"
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
                "source": "data:application/json;base64,ewogICJpZ25pdGlvbiI6IHsKICAgICJ2ZXJzaW9uIjogIjMuMC4wIgogIH0sCiAgInN0b3JhZ2UiOiB7CiAgICAiZmlsZXMiOiBbCiAgICAgIHsKICAgICAgICAicGF0aCI6ICIvZXRjL3Rlc3QuY29uZiIsCiAgICAgICAgImNvbnRlbnRzIjogewogICAgICAgICAgInNvdXJjZSI6ICJkYXRhOixoZWxsbyUyMHdvcmxkIgogICAgICAgIH0sCiAgICAgICAgIm1vZGUiOiA0MjAKICAgICAgfQogICAgXQogIH0KfQ=="
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
