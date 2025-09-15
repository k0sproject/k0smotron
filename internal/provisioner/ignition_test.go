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
			wantJSON: `{
  "ignition": {
    "config": { 
		"replace": { 
			"verification": {} 
		} 
	},
    "proxy": {},
    "security": { 
		"tls": {} 
	},
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {
    "files": [
      {
        "contents": {
          "compression": "",
          "source": "data:,hello%20world",
          "verification": {}
        },
        "group": {},
        "mode": 420,
        "path": "/etc/test.conf",
        "user": {}
      }
    ]
  },
  "systemd": {}
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
			wantJSON: `{
  "ignition": {
    "config": { 
		"replace": { 
			"verification": {} 
		} 
	},
    "proxy": {},
    "security": { 
		"tls": {} 
	},
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {},
  "systemd": {
    "units": [
      {
        "contents": "[Unit]\nDescription=K0s Bootstrap Commands\nAfter=network-online.target\n\n[Service]\nType=oneshot\nExecStart=/bin/sh -c 'echo hello'\nRemainAfterExit=true\n\n[Install]\nWantedBy=multi-user.target",
        "enabled": true,
        "name": "k0s-bootstrap.service"
      }
    ]
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
			wantJSON: `{
  "ignition": {
    "config": { 
		"replace": { 
			"verification": {} 
		} 
	},
    "proxy": {},
    "security": { 
		"tls": {} 
	},
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {
    "files": [
      {
        "contents": {
          "compression": "",
          "source": "data:,combo",
          "verification": {}
        },
        "group": {},
        "mode": 420,
        "path": "/etc/combined.conf",
        "user": {}
      }
    ]
  },
  "systemd": {
    "units": [
      {
        "contents": "[Unit]\nDescription=K0s Bootstrap Commands\nAfter=network-online.target\n\n[Service]\nType=oneshot\nExecStart=/bin/sh -c 'echo combo'\nRemainAfterExit=true\n\n[Install]\nWantedBy=multi-user.target",
        "enabled": true,
        "name": "k0s-bootstrap.service"
      }
    ]
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
				AdditionalConfig: `variant: fcos
version: 1.0.0
systemd:
  units:
    - name: extra.service
      enabled: true
      contents: "echo extra"`,
			},
			input: &InputProvisionData{},
			wantJSON: `{
  "ignition": {
    "config": { "replace": { "verification": {} } },
    "proxy": {},
    "security": { "tls": {} },
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {},
  "systemd": {
    "units": [
      {
        "contents": "echo extra",
        "enabled": true,
        "name": "extra.service"
      }
    ]
  }
}`,
		},
		{
			name: "additional config with file",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `variant: fcos
version: 1.0.0
storage:
  files:
    - path: /etc/extra.conf
      mode: 420
      contents:
        inline: "from additional config"`,
			},
			input: &InputProvisionData{},
			wantJSON: `{
  "ignition": {
    "config": { "replace": { "verification": {} } },
    "proxy": {},
    "security": { "tls": {} },
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {
    "files": [
      {
        "contents": {
          "compression": "",
          "source": "data:,from%20additional%20config",
          "verification": {}
        },
        "group": {},
        "mode": 420,
        "path": "/etc/extra.conf",
        "user": {}
      }
    ]
  },
  "systemd": {}
}`,
		},
		{
			name: "files + additional config",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `variant: fcos
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
			wantJSON: `{
  "ignition": {
    "config": { "replace": { "verification": {} } },
    "proxy": {},
    "security": { "tls": {} },
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {
    "files": [
      {
        "contents": {
          "compression": "",
          "source": "data:,hello%20world",
          "verification": {}
        },
        "group": {},
        "mode": 420,
        "path": "/etc/test.conf",
        "user": {}
      },
      {
        "contents": {
          "compression": "",
          "source": "data:,from%20additional%20config",
          "verification": {}
        },
        "group": {},
        "mode": 420,
        "path": "/etc/extra.conf",
        "user": {}
      }
    ]
  },
  "systemd": {}
}`,
		},
		{
			name: "commands + additional config unit",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `variant: fcos
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
			wantJSON: `{
  "ignition": {
    "config": { "replace": { "verification": {} } },
    "proxy": {},
    "security": { "tls": {} },
    "timeouts": {},
    "version": "3.4.0"
  },
  "kernelArguments": {},
  "passwd": {},
  "storage": {},
  "systemd": {
    "units": [
      {
        "contents": "[Unit]\nDescription=K0s Bootstrap Commands\nAfter=network-online.target\n\n[Service]\nType=oneshot\nExecStart=/bin/sh -c 'echo hello'\nRemainAfterExit=true\n\n[Install]\nWantedBy=multi-user.target",
        "enabled": true,
        "name": "k0s-bootstrap.service"
      },
      {
        "contents": "echo extra",
        "enabled": true,
        "name": "extra.service"
      }
    ]
  }
}`,
		},
		{
			name: "invalid additional config YAML",
			provisioner: IgnitionProvisioner{
				Variant: "fcos",
				Version: "1.0.0",
				AdditionalConfig: `variant: fcos
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
