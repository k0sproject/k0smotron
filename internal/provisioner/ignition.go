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
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"text/template"

	butane "github.com/coreos/butane/config"
	bcommon "github.com/coreos/butane/config/common"
)

const ignitionSystemdTemplate = `[Unit]
Description=K0s Bootstrap Commands
After=network-online.target

[Service]
Type=oneshot
{{- range . }}
ExecStart=/usr/bin/bash -c '{{ . }}'
{{- end }}
Restart=no

[Install]
WantedBy=multi-user.target`

// IgnitionProvisioner implements the Provisioner interface for Ignition.
type IgnitionProvisioner struct {
	Variant string
	Version string
}

// ToProvisionData converts the input data to Ignition user data.
func (i *IgnitionProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	files := []map[string]interface{}{}
	for _, f := range input.Files {
		// string to int
		modeInt, err := strconv.Atoi(f.Permissions)
		if err != nil {
			return nil, err
		}
		files = append(files, map[string]interface{}{
			"path": f.Path,
			"contents": map[string]interface{}{
				"inline": f.Content,
			},
			"mode": modeInt,
		})
	}

	tmpl, err := template.New("systemd").Parse(ignitionSystemdTemplate)
	if err != nil {
		return nil, fmt.Errorf("error parsing systemd template: %w", err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, input.Commands)
	if err != nil {
		return nil, fmt.Errorf("error executing systemd template: %w", err)
	}

	butaneConfig := map[string]interface{}{
		"variant": i.Variant,
		"version": i.Version,
		"storage": map[string]interface{}{
			"files": files,
		},
		"systemd": map[string]interface{}{
			"units": []map[string]interface{}{
				{
					"name":     "k0s-bootstrap.service",
					"enabled":  true,
					"contents": buf.String(),
				},
			},
		},
	}
	butaneConfigBytes, err := json.Marshal(butaneConfig)
	if err != nil {
		return nil, err
	}

	ignData, report, err := butane.TranslateBytes(butaneConfigBytes, bcommon.TranslateBytesOptions{})
	if err != nil {
		return nil, fmt.Errorf("error translating butane config to ignition: report: %s: %w", report.String(), err)
	}

	return ignData, nil
}
