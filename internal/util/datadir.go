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
//nolint:revive
package util

import (
	"strings"

	"github.com/mattn/go-shellwords"
)

// DefaultK0sDataDir is the data directory k0s uses when --data-dir is not set.
const DefaultK0sDataDir = "/var/lib/k0s"

// GetDataDir returns the k0s data directory for the given k0s command-line arguments,
// falling back to DefaultK0sDataDir when --data-dir is absent, empty or the arguments
// cannot be tokenized.
func GetDataDir(args []string) string {
	const dataDirFlag = "--data-dir"
	dataDirValue := DefaultK0sDataDir
	for _, arg := range args {
		parsed, err := shellwords.Parse(arg)
		if err != nil || len(parsed) == 0 {
			continue
		}
		switch {
		case parsed[0] == dataDirFlag && len(parsed) == 2:
			// Case: [--data-dir, /some/path]
			dataDirValue = parsed[1]
		case strings.HasPrefix(parsed[0], dataDirFlag+"="):
			// Case: [--data-dir=/some/path]
			dataDirValue = strings.TrimPrefix(parsed[0], dataDirFlag+"=")
		}
	}
	if dataDirValue == "" {
		return DefaultK0sDataDir
	}
	return dataDirValue
}
