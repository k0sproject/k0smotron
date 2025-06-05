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
)

// DownloadCommands constructs the download commands for a given URL and version.
func DownloadCommands(preInstalledK0s bool, url string, version string) []string {
	if preInstalledK0s {
		return nil
	}
	if url != "" {
		return []string{
			fmt.Sprintf("curl -sSfL --retry 5 %s -o /usr/local/bin/k0s", url),
			"chmod +x /usr/local/bin/k0s",
		}
	}

	if version != "" {
		return []string{
			fmt.Sprintf("curl -sSfL --retry 5 https://get.k0s.sh | K0S_VERSION=%s sh", version),
		}
	}

	// Default to k0s get script to download the latest version
	return []string{
		"curl -sSfL --retry 5 https://get.k0s.sh | sh",
	}
}
