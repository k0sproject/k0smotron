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

package util

import "strings"

// DefaultK0sVersionSuffix is appended to versions without a k0s suffix.
const DefaultK0sVersionSuffix = "k0s.0"

// NormalizeK0sVersion returns the version in the "vX.Y.Z+k0s.N" form, converting "-k0s.N" and appending "+k0s.0" if no suffix is set.
func NormalizeK0sVersion(version string) string {
	if version == "" {
		return version
	}

	version = strings.Replace(version, "-k0s.", "+k0s.", 1)
	if !strings.Contains(version, "+k0s.") {
		version += "+" + DefaultK0sVersionSuffix
	}

	return version
}
