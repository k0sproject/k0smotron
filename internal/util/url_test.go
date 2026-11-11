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
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostPortURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		want string
	}{
		{name: "hostname", host: "cp.example.com", want: "https://cp.example.com:6443"},
		{name: "IPv4", host: "192.0.2.10", want: "https://192.0.2.10:6443"},
		{name: "IPv6", host: "2001:db8::10", want: "https://[2001:db8::10]:6443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := HostPortURL("https", tc.host, 6443)
			require.Equal(t, tc.want, got)

			// The URL has to be one Go's own clients accept, with the host and
			// port coming back out unchanged.
			u, err := url.Parse(got)
			require.NoError(t, err)
			require.Equal(t, tc.host, u.Hostname())
			require.Equal(t, "6443", u.Port())
		})
	}
}
