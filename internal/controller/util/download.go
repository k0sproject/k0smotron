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
	"strings"
)

// DownloadCommands constructs the download commands for a given URL and version.
func DownloadCommands(preInstalledK0s bool, url string, version string, k0sBinPath string) []string {
	if preInstalledK0s {
		return nil
	}

	if url != "" {
		return []string{
			fmt.Sprintf("curl -sSfL --retry 5 %s -o /usr/local/bin/k0s", url),
			"chmod +x /usr/local/bin/k0s",
		}
	}

	var scriptVars []string

	if version != "" {
		scriptVars = append(scriptVars, fmt.Sprintf("K0S_VERSION=%s", version))
	}
	if k0sBinPath != "" {
		scriptVars = append(scriptVars, fmt.Sprintf("K0S_INSTALL_PATH=%s", k0sBinPath))
	}

	cmd := "curl -sSfL --retry 5 https://get.k0s.sh"
	if len(scriptVars) > 0 {
		cmd = fmt.Sprintf("%s | %s sh", cmd, strings.Join(scriptVars, " "))
	} else {
		cmd = fmt.Sprintf("%s | sh", cmd)
	}

	return []string{cmd}
}
