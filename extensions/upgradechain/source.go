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
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DefaultReleasesURL is the GitHub API endpoint listing k0s releases.
const DefaultReleasesURL = "https://api.github.com/repos/k0sproject/k0s/releases"

const maxReleasePages = 10

// VersionSource lists the k0s versions available for upgrade.
type VersionSource interface {
	// Versions returns the available k0s release tags, e.g. "v1.31.2+k0s.0", in no particular order. Only releases
	// from the minor of the "from" version onwards are guaranteed to be returned.
	Versions(ctx context.Context, from string) ([]string, error)
}

// GitHubVersionSource lists k0s releases from the GitHub releases API. The result is cached for ttl,
// and a stale cache is served if a refresh fails, so that a transient outage doesn't block upgrades.
type GitHubVersionSource struct {
	URL    string
	Client *http.Client
	TTL    time.Duration

	mu        sync.Mutex
	cached    []string
	coverFrom [2]int // oldest major.minor the cached list is guaranteed to cover
	fetchedAt time.Time
}

// NewGitHubVersionSource creates a GitHubVersionSource for the given releases URL.
func NewGitHubVersionSource(url string, ttl time.Duration) *GitHubVersionSource {
	return &GitHubVersionSource{URL: url, Client: &http.Client{Timeout: 15 * time.Second}, TTL: ttl}
}

// Versions implements VersionSource.
func (s *GitHubVersionSource) Versions(ctx context.Context, from string) ([]string, error) {
	f, err := parseK0sVersion(from)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	covers := s.cached != nil && !minorLess(f.minor(), s.coverFrom)
	if covers && time.Since(s.fetchedAt) < s.TTL {
		return s.cached, nil
	}

	versions, err := s.fetch(ctx, f.minor())
	if err != nil {
		if covers {
			return s.cached, nil
		}
		return nil, err
	}

	s.cached, s.coverFrom, s.fetchedAt = versions, f.minor(), time.Now()
	return versions, nil
}

func minorLess(a, b [2]int) bool {
	return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1])
}

// fetch pages through the releases (newest first) and stops as soon as a page has no release from the given minor
// onwards, so older history is not downloaded.
func (s *GitHubVersionSource) fetch(ctx context.Context, from [2]int) ([]string, error) {
	var tags []string
	for page := 1; page <= maxReleasePages; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", s.URL, page), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := s.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("listing k0s releases: %w", err)
		}
		var releases []struct {
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
		}
		err = func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("listing k0s releases: unexpected status %d", resp.StatusCode)
			}
			return json.NewDecoder(resp.Body).Decode(&releases)
		}()
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			break
		}
		relevant := false
		for _, r := range releases {
			v, err := parseK0sVersion(r.Tag)
			if err != nil || minorLess(v.minor(), from) {
				continue
			}
			relevant = true
			if !r.Draft && !r.Prerelease {
				tags = append(tags, r.Tag)
			}
		}
		if !relevant {
			break
		}
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no k0s releases found at %s", s.URL)
	}
	return tags, nil
}
