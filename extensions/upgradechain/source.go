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
	"net/url"
	"strings"

	"github.com/google/go-github/v82/github"
)

// VersionSource lists the k0s versions available for upgrade.
type VersionSource interface {
	// Versions returns the available k0s release tags, e.g. "v1.31.2+k0s.0", in no particular order. Only releases
	// from the minor of the "from" version onwards are guaranteed to be returned.
	Versions(ctx context.Context, from string) ([]string, error)
}

// GitHubVersionSource lists k0s releases from the GitHub releases API. The result is cached for ttl,
// and a stale cache is served if a refresh fails, so that a transient outage doesn't block upgrades.
type GitHubVersionSource struct {
	Client *github.Client
}

// NewGitHubVersionSource creates a GitHubVersionSource for the given GitHub API base URL.
func NewGitHubVersionSource(baseURL string) (*GitHubVersionSource, error) {
	client := github.NewClient(nil)

	if baseURL != "" {
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}

		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub API URL %q: %w", baseURL, err)
		}
		client.BaseURL = u
	}

	return &GitHubVersionSource{Client: client}, nil
}

// Versions implements VersionSource.
func (s *GitHubVersionSource) Versions(ctx context.Context, from string) ([]string, error) {
	f, err := parseK0sVersion(from)
	if err != nil {
		return nil, err
	}

	versions, err := s.fetch(ctx, f)
	if err != nil {
		return nil, err
	}

	return versions, nil
}

const (
	k0sOwner = "k0sproject"
	k0sRepo  = "k0s"
)

// fetch pages through the releases and returns all k0s release tags from the "from" version onwards.
func (s *GitHubVersionSource) fetch(ctx context.Context, from k0sVersion) ([]string, error) {
	var tags []string

	opts := &github.ListOptions{PerPage: 100}
	for page := 0; true; page++ {
		releases, resp, err := s.Client.Repositories.ListReleases(ctx, k0sOwner, k0sRepo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing k0s releases: %w", err)
		}

		for _, r := range releases {
			tagName := r.GetTagName()
			v, err := parseK0sVersion(tagName)
			if err != nil {
				continue
			}

			// Skip releases older than the "from" version.
			if v.Version.LessThan(from.Version) {
				continue
			}

			// Include only non-draft and non-prerelease versions.
			if r.GetDraft() || r.GetPrerelease() {
				continue
			}

			tags = append(tags, tagName)
		}
		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no k0s releases found for %s/%s", k0sOwner, k0sRepo)
	}
	return tags, nil
}
