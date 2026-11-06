/*
Copyright 2023 k0s authors

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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomString(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"zero length", 0},
		{"single char", 1},
		{"typical length", 16},
		{"long length", 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RandomString(tt.length)
			require.Len(t, result, tt.length)
			for _, c := range result {
				require.True(t, strings.ContainsRune(letters, c), "unexpected character %q in result %q", c, result)
			}
		})
	}
}

func TestRandomStringUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s := RandomString(16)
		require.False(t, seen[s], "RandomString produced a duplicate value: %s", s)
		seen[s] = true
	}
}
