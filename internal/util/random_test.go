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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomString(t *testing.T) {
	require.Empty(t, RandomString(0))

	s := RandomString(16)
	require.Len(t, s, 16)
	for _, r := range s {
		require.Contains(t, letters, string(r))
	}

	require.NotEqual(t, RandomString(16), RandomString(16), "two random strings of reasonable length should not collide")
}
