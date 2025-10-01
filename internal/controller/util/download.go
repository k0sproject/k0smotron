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
	"fmt"
	"net/url"
	"strings"
)

const (
	k0sBinName = "k0s"
)

// DownloadCommands constructs the download commands for a given URL and version.
func DownloadCommands(preInstalledK0s bool, downloadURL string, version string, k0sInstallPath string) ([]string, error) {
	if preInstalledK0s {
		return nil, nil
	}

	if k0sInstallPath == "" {
		k0sInstallPath = "/usr/local/bin"
	}

	k0sBinPath := fmt.Sprintf("%s/%s", k0sInstallPath, k0sBinName)

	if downloadURL != "" {
		parsedURL, err := url.Parse(downloadURL)
		if err != nil {
			return []string{fmt.Sprintf("echo 'Invalid download URL: %s'", downloadURL)}, nil
		}

		switch parsedURL.Scheme {
		case "https", "http":
			return []string{
				fmt.Sprintf("curl -sSfL --retry 5 %s -o %s", downloadURL, k0sBinPath),
				fmt.Sprintf("chmod +x %s", k0sBinPath),
			}, nil
		case "oci":
			// oras expects the part after oci:// in the URL: example.com/k0s@sha256:abcdef1234567890
			artifactRef := fmt.Sprintf("%s%s", parsedURL.Host, parsedURL.Path)
			return []string{
				"export HOME=/root", // oras requires HOME to be set. During cloud-init execution, HOME is not set because the user is root at that point.
				fmt.Sprintf("oras blob fetch --output %s %s", k0sBinPath, artifactRef),
				fmt.Sprintf("chmod +x %s", k0sBinPath),
			}, nil
		default:
			return nil, fmt.Errorf("unsupported URL scheme '%s'", parsedURL.Scheme)
		}
	}

	scriptVars := []string{
		fmt.Sprintf("K0S_INSTALL_PATH=%s", k0sInstallPath),
	}
	if version != "" {
		scriptVars = append(scriptVars, fmt.Sprintf("K0S_VERSION=%s", version))
	}

	cmd := "curl -sSfL --retry 5 https://get.k0s.sh"
	if len(scriptVars) > 0 {
		cmd = fmt.Sprintf("%s | %s sh", cmd, strings.Join(scriptVars, " "))
	} else {
		cmd = fmt.Sprintf("%s | sh", cmd)
	}

	return []string{cmd}, nil
}
