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
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/k0sproject/version"
)

// k0sVersion is a k0s release version: a semantic version plus the k0s build number ("+k0s.N").
type k0sVersion struct {
	*version.Version
	raw   string
	build int
}

func parseK0sVersion(v string) (k0sVersion, error) {
	parsed, err := version.NewVersion(v)
	if err != nil {
		return k0sVersion{}, fmt.Errorf("invalid k0s version %q: %w", v, err)
	}

	build := 0
	// Accept both "+k0s.N" and "-k0s.N", the latter being the form used in some container image tags.
	if _, suffix, ok := strings.Cut(strings.Replace(v, "-k0s.", "+k0s.", 1), "+k0s."); ok {
		if build, err = strconv.Atoi(suffix); err != nil {
			return k0sVersion{}, fmt.Errorf("invalid k0s build number in %q: %w", v, err)
		}
	}

	return k0sVersion{Version: parsed, raw: v, build: build}, nil
}

// minor returns the major.minor pair of the version.
func (v k0sVersion) minor() [2]int {
	s := v.Segments()
	return [2]int{s[0], s[1]}
}

func (v k0sVersion) isPrerelease() bool {
	return v.Prerelease() != ""
}

// compare orders versions by semantic version first and then by numeric k0s build number.
func (v k0sVersion) compare(o k0sVersion) int {
	if c := v.Version.Version.Compare(&o.Version.Version); c != 0 {
		return c
	}
	return cmp.Compare(v.build, o.build)
}
