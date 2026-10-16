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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGitHubVersionSourceStopsPagingBelowFrom(t *testing.T) {
	pages := map[string]string{
		"1": `[{"tag_name":"v1.32.0+k0s.0"},{"tag_name":"v1.31.2+k0s.0"},{"tag_name":"v1.31.1-rc.1+k0s.0","prerelease":true}]`,
		"2": `[{"tag_name":"v1.31.1+k0s.0"},{"tag_name":"v1.30.5+k0s.0"}]`,
		"3": `[{"tag_name":"v1.29.9+k0s.0"},{"tag_name":"v1.28.1+k0s.0"}]`,
		"4": `[{"tag_name":"v1.27.1+k0s.0"}]`,
	}
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		requested = append(requested, p)
		body, ok := pages[p]
		if !ok {
			body = "[]"
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	s := NewGitHubVersionSource(srv.URL, time.Hour)
	got, err := s.Versions(context.Background(), "v1.30.5+k0s.0")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"v1.32.0+k0s.0", "v1.31.2+k0s.0", "v1.31.1+k0s.0", "v1.30.5+k0s.0"}, got)
	// page 3 only has releases older than from's minor, so paging stops there.
	require.Equal(t, []string{"1", "2", "3"}, requested)

	// a cached list covering from is reused, an older from forces a refetch.
	_, err = s.Versions(context.Background(), "v1.31.0+k0s.0")
	require.NoError(t, err)
	require.Len(t, requested, 3)
	_, err = s.Versions(context.Background(), "v1.28.0+k0s.0")
	require.NoError(t, err)
	require.Greater(t, len(requested), 3)
}
