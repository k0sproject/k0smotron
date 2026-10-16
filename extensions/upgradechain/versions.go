//go:build extension

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

package upgradechain

import (
	"fmt"

	"github.com/k0sproject/version"
)

// k0sVersion represents a k0s release version.
type k0sVersion struct {
	*version.Version
}

func parseK0sVersion(v string) (k0sVersion, error) {
	parsed, err := version.NewVersion(v)
	if err != nil {
		return k0sVersion{}, fmt.Errorf("invalid k0s version %q: %w", v, err)
	}

	return k0sVersion{Version: parsed}, nil
}

// minor returns the minor version of the k0s version.
func (v k0sVersion) majorMinor() [2]int {
	s := v.Segments()
	return [2]int{s[0], s[1]}
}
