/*


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

package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_createDownloadCommands(t *testing.T) {
	tests := []struct {
		name            string
		preInstalledK0s bool
		url             string
		version         string
		installPath     string
		want            []string
	}{
		{
			name:            "with pre-installed k0s",
			preInstalledK0s: true,
			url:             "",
			version:         "",
			want:            nil,
		},
		{
			name:    "with default config",
			version: "",
			url:     "",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | sh",
			},
		},
		{
			name:    "with custom version",
			version: "v1.2.3",
			url:     "",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_VERSION=v1.2.3 sh",
			},
		},
		{
			name:    "with custom download URL",
			version: "",
			url:     "https://example.com/k0s",
			want: []string{
				"curl -sSfL --retry 5 https://example.com/k0s -o /usr/local/bin/k0s",
				"chmod +x /usr/local/bin/k0s",
			},
		},
		{
			name:        "with custom install path",
			version:     "",
			url:         "",
			installPath: "/opt/bin",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_INSTALL_PATH=/opt/bin sh",
			},
		},
		{
			name:        "with custom version and install path",
			version:     "v1.2.3",
			url:         "",
			installPath: "/opt/bin",
			want: []string{
				"curl -sSfL --retry 5 https://get.k0s.sh | K0S_VERSION=v1.2.3 K0S_INSTALL_PATH=/opt/bin sh",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, DownloadCommands(tt.preInstalledK0s, tt.url, tt.version, tt.installPath))
		})
	}
}
