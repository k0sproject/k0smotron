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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"text/template"

	"github.com/coreos/butane/config"
	bcommon "github.com/coreos/butane/config/common"
	"gopkg.in/yaml.v3"
)

const ignitionSystemdTemplate = `[Unit]
Description=K0s Bootstrap Commands
After=network-online.target

[Service]
Type=oneshot
{{- range . }}
ExecStart=/bin/sh -c '{{ . }}'
{{- end }}
RemainAfterExit=true

[Install]
WantedBy=multi-user.target`

// IgnitionProvisioner implements the Provisioner interface for Ignition.
type IgnitionProvisioner struct {
	Variant          string
	Version          string
	AdditionalConfig string
}

// ToProvisionData converts the input data to Ignition user data.
func (i *IgnitionProvisioner) ToProvisionData(input *InputProvisionData) ([]byte, error) {
	files := []map[string]interface{}{}
	for _, f := range input.Files {
		mi, err := strconv.ParseInt(f.Permissions, 8, 32)
		if err != nil {
			return nil, err
		}
		files = append(files, map[string]interface{}{
			"path":     f.Path,
			"contents": map[string]string{"inline": f.Content},
			"mode":     int(mi),
		})
	}

	units := []map[string]interface{}{}
	if len(input.Commands) > 0 {
		var buf bytes.Buffer
		tmpl, err := template.New("systemd").Parse(ignitionSystemdTemplate)
		if err != nil {
			return nil, fmt.Errorf("error parsing systemd template: %w", err)
		}
		err = tmpl.Execute(&buf, input.Commands)
		if err != nil {
			return nil, fmt.Errorf("error executing systemd template: %w", err)
		}

		units = append(units, map[string]interface{}{
			"name":     "k0s-bootstrap.service",
			"enabled":  true,
			"contents": buf.String(),
		})
	}

	// translate initial Butane config (without additionalConfig) to Ignition JSON
	initialButaneCfg := map[string]interface{}{
		"variant": i.Variant,
		"version": i.Version,
		"storage": map[string]interface{}{"files": files},
		"systemd": map[string]interface{}{"units": units},
	}
	butaneYaml, err := yaml.Marshal(initialButaneCfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling butane config: %w", err)
	}
	initIgn, _, err := config.TranslateBytes(
		butaneYaml,
		bcommon.TranslateBytesOptions{
			TranslateOptions: bcommon.TranslateOptions{NoResourceAutoCompression: true},
			Pretty:           true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error translating butane config: %w", err)
	}

	// Get ignition spec version from initial config
	type ignVersion struct {
		Ignition struct {
			Version string `json:"version"`
		} `json:"ignition"`
	}

	var initIgnVersion ignVersion
	if err := json.Unmarshal(initIgn, &initIgnVersion); err != nil {
		return nil, fmt.Errorf("error unmarshaling ignition version: %w", err)
	}

	initIgnEncoded := base64.StdEncoding.EncodeToString(initIgn)

	ignMerge := []map[string]string{
		{
			"source": "data:application/json;base64," + initIgnEncoded,
		},
	}

	if i.AdditionalConfig != "" {
		// translate additional Butane YAML to Ignition JSON
		addIgn, _, err := config.TranslateBytes(
			[]byte(i.AdditionalConfig),
			bcommon.TranslateBytesOptions{
				TranslateOptions: bcommon.TranslateOptions{NoResourceAutoCompression: true},
				Pretty:           true,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("error translating additional config: %w", err)
		}

		additionalIgnEncoded := base64.StdEncoding.EncodeToString(addIgn)

		ignMerge = append(ignMerge, map[string]string{
			"source": "data:application/json;base64," + additionalIgnEncoded,
		})

		// Get ignition spec version from additional config
		var additionalCfgIgnVersion ignVersion
		if err := json.Unmarshal(addIgn, &additionalCfgIgnVersion); err != nil {
			return nil, fmt.Errorf("error unmarshaling ignition version: %w", err)
		}

		if initIgnVersion.Ignition.Version != additionalCfgIgnVersion.Ignition.Version {
			return nil, fmt.Errorf("mismatched Ignition versions between initial config (%s) and additional config (%s)",
				initIgnVersion.Ignition.Version, additionalCfgIgnVersion.Ignition.Version)
		}

	}

	// build final Ignition config with merge sources (initial + additional)
	finalIgnitionCfg := map[string]interface{}{
		"ignition": map[string]interface{}{
			"version": initIgnVersion.Ignition.Version,
			"config": map[string]interface{}{
				"merge": ignMerge,
			},
		},
	}

	finalIgnitionBytes, err := json.Marshal(finalIgnitionCfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling final ignition config: %w", err)
	}

	return finalIgnitionBytes, nil
}

// GetFormat returns the format 'ignition' of the provisioner.
func (i *IgnitionProvisioner) GetFormat() ProvisioningFormat {
	return IgnitionProvisioningFormat
}
