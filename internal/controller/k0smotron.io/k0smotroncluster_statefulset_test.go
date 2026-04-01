//go:build !envtest

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

package k0smotronio

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_pathToVolumeName(t *testing.T) {
	tests := []struct {
		path       string
		want       string // exact expected name for simple/clean paths (no hash)
		wantPrefix string // expected prefix for sanitized paths (ends with "-")
	}{
		{
			// Simple path: already valid, no hash added (backward-compatible)
			path: "/etc/kubernetes/pki",
			want: "etc-kubernetes-pki",
		},
		{
			// Simple path used in e2e upgrade test - must stay stable across upgrades
			path: "/tmp/test",
			want: "tmp-test",
		},
		{
			// Underscore is invalid -> sanitized with hash
			path:       "/my_config/file.conf",
			wantPrefix: "my-config-file-conf-",
		},
		{
			// Dot is invalid -> sanitized with hash
			path:       "/etc/ssl/certs/ca-certificates.crt",
			wantPrefix: "etc-ssl-certs-ca-certificates-crt-",
		},
		{
			// Uppercase is invalid -> sanitized with hash
			path:       "/VAR/lib/K0s",
			wantPrefix: "var-lib-k0s-",
		},
		{
			// Single alphanumeric: valid, no hash
			path: "/a",
			want: "a",
		},
		{
			// Dot in path segment -> sanitized with hash
			path:       "/root/.aws/credentials",
			wantPrefix: "root-aws-credentials-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pathToVolumeName(tt.path)

			// Must be at most 63 chars (DNS label limit)
			assert.LessOrEqual(t, len(got), 63, "volume name exceeds DNS label limit")

			// Must be a valid DNS label
			assert.Regexp(t, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, got, "volume name must be a valid DNS label")

			if tt.want != "" {
				assert.Equal(t, tt.want, got)
			} else {
				assert.True(t, strings.HasPrefix(got, tt.wantPrefix), "expected prefix %q, got %q", tt.wantPrefix, got)
			}
		})
	}

	t.Run("unique names for paths that sanitize to the same string", func(t *testing.T) {
		paths := []string{
			"/my_path",
			"/my.path",
		}
		names := make(map[string]string)
		for _, p := range paths {
			name := pathToVolumeName(p)
			for prev, prevName := range names {
				assert.NotEqual(t, prevName, name, "paths %q and %q produced the same volume name %q", prev, p, name)
			}
			names[p] = name
		}
	})

	t.Run("long path is truncated to 63 chars", func(t *testing.T) {
		longPath := "/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/aa/bb/cc/dd/ee/ff"
		got := pathToVolumeName(longPath)
		assert.LessOrEqual(t, len(got), 63)
		assert.Regexp(t, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, got)
	})
}
