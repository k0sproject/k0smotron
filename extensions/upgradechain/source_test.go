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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubVersionSourceStopsPagingBelowFrom(t *testing.T) {
	pages := map[string]string{
		"1": `[{"tag_name":"v1.29.9+k0s.0"},{"tag_name":"v1.28.1+k0s.0"}]`,
		"2": `[{"tag_name":"v1.27.1+k0s.0"}]`,
		"3": `[{"tag_name":"v1.32.0+k0s.0"},{"tag_name":"v1.31.2+k0s.0"},{"tag_name":"v1.31.1-rc.1+k0s.0","prerelease":true}]`,
		"4": `[{"tag_name":"v1.31.1+k0s.0"},{"tag_name":"v1.30.5+k0s.0"}]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		if p == "" {
			p = "1"
		}
		body, ok := pages[p]
		if !ok {
			body = "[]"
		}
		if n, err := strconv.Atoi(p); err == nil && n < len(pages) {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/k0sproject/k0s/releases?per_page=100&page=%d>; rel="next"`, r.Host, n+1))
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	s, err := NewGitHubVersionSource(srv.URL)
	require.NoError(t, err)
	got, err := s.Versions(context.Background(), "v1.30.5+k0s.0")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"v1.32.0+k0s.0", "v1.31.2+k0s.0", "v1.31.1+k0s.0", "v1.30.5+k0s.0"}, got)
}
