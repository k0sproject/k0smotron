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

package provisioner

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodedContent(t *testing.T) {
	const plain = "hello\nworld\n"

	t.Run("no encoding is verbatim", func(t *testing.T) {
		got, err := File{Content: plain}.DecodedContent()
		require.NoError(t, err)
		assert.Equal(t, plain, string(got))
	})

	t.Run("base64", func(t *testing.T) {
		got, err := File{Content: base64.StdEncoding.EncodeToString([]byte(plain)), Encoding: Base64}.DecodedContent()
		require.NoError(t, err)
		assert.Equal(t, plain, string(got))
	})

	t.Run("base64 tolerates wrapped whitespace", func(t *testing.T) {
		enc := base64.StdEncoding.EncodeToString([]byte(plain))
		got, err := File{Content: enc[:4] + "\n  " + enc[4:], Encoding: Base64}.DecodedContent()
		require.NoError(t, err)
		assert.Equal(t, plain, string(got))
	})

	t.Run("binary payload survives base64", func(t *testing.T) {
		raw := []byte{0x00, 0xff, 0x1b, 0x7f, 0x0a}
		got, err := File{Content: base64.StdEncoding.EncodeToString(raw), Encoding: Base64}.DecodedContent()
		require.NoError(t, err)
		assert.Equal(t, raw, got)
	})

	t.Run("decoding never grows the input", func(t *testing.T) {
		// This is why no size cap is needed. Anything base64 can express is
		// smaller than the field that carried it.
		raw := make([]byte, 4096)
		enc := base64.StdEncoding.EncodeToString(raw)

		got, err := File{Content: enc, Encoding: Base64}.DecodedContent()
		require.NoError(t, err)
		require.Less(t, len(got), len(enc))
	})

	t.Run("invalid base64 errors", func(t *testing.T) {
		_, err := File{Path: "/tmp/x", Content: "!!!not base64!!!", Encoding: Base64}.DecodedContent()
		require.ErrorContains(t, err, "failed to base64 decode")
	})

	t.Run("gzip is not an accepted encoding", func(t *testing.T) {
		// Decoding gzip would mean expanding untrusted input in the controller,
		// so it is rejected rather than supported.
		for _, enc := range []Encoding{"gzip", "gzip+base64"} {
			_, err := File{Path: "/etc/thing", Content: "x", Encoding: enc}.DecodedContent()
			require.ErrorContains(t, err, "unsupported encoding")
		}
	})

	t.Run("unknown encoding errors and names the file", func(t *testing.T) {
		_, err := File{Path: "/etc/thing", Content: "x", Encoding: Encoding("rot13")}.DecodedContent()
		require.ErrorContains(t, err, `unsupported encoding "rot13"`)
		require.ErrorContains(t, err, "/etc/thing")
	})
}

func TestOwnerUserAndGroup(t *testing.T) {
	for _, tc := range []struct {
		owner, user, group string
	}{
		{"", "", ""},
		{"root", "root", ""},
		{"root:root", "root", "root"},
		{"etcd:etcd", "etcd", "etcd"},
	} {
		u, g := File{Owner: tc.owner}.OwnerUserAndGroup()
		assert.Equal(t, tc.user, u, "user for owner %q", tc.owner)
		assert.Equal(t, tc.group, g, "group for owner %q", tc.owner)
	}
}
