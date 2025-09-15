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

	butil "github.com/coreos/butane/base/util"
	bbase "github.com/coreos/butane/base/v0_5"
	bcommon "github.com/coreos/butane/config/common"
	"github.com/coreos/butane/translate"
	"gopkg.in/yaml.v2"
	"k8s.io/utils/ptr"
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
	files := []bbase.File{}
	for _, f := range input.Files {
		modeInt, err := strconv.ParseInt(f.Permissions, 8, 32)
		if err != nil {
			return nil, err
		}
		mode := int(modeInt)

		files = append(files, bbase.File{
			Path: f.Path,
			Contents: bbase.Resource{
				Inline: &f.Content,
			},
			Mode: &mode,
		})
	}

	units := []bbase.Unit{}
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

		units = append(units, bbase.Unit{
			Name:     "k0s-bootstrap.service",
			Enabled:  ptr.To(true),
			Contents: ptr.To(buf.String()),
		})
	}

	butaneConfig := bbase.Config{
		Variant: i.Variant,
		Version: i.Version,
		Storage: bbase.Storage{
			Files: files,
		},
		Systemd: bbase.Systemd{
			Units: units,
		},
	}

	initConfig, _, _ := butaneConfig.ToIgn3_4Unvalidated(bcommon.TranslateOptions{NoResourceAutoCompression: true})

	if i.AdditionalConfig != "" {
		ac := &bbase.Config{}
		err := yaml.Unmarshal([]byte(i.AdditionalConfig), ac)
		if err != nil {
			return nil, err
		}

		additionalConfig, _, _ := ac.ToIgn3_4Unvalidated(bcommon.TranslateOptions{NoResourceAutoCompression: true})
		mergedConfig, _ := butil.MergeTranslatedConfigs(initConfig, translate.TranslationSet{}, additionalConfig, translate.TranslationSet{})

		ignData, err := json.MarshalIndent(mergedConfig, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("error marshalling ignition config: %w", err)
		}

		return ignData, nil
	}

	ignData, err := json.MarshalIndent(initConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling ignition config: %w", err)
	}

	return ignData, nil
}
