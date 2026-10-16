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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

var releases = []string{
	"v1.29.9+k0s.0", "v1.30.1+k0s.0", "v1.30.5+k0s.0", "v1.30.5+k0s.1", "v1.30.5+k0s.10", "v1.30.5+k0s.2",
	"v1.31.1+k0s.0", "v1.31.2+k0s.0", "v1.32.0+k0s.0", "v1.32.1-rc.1+k0s.0", "v1.32.1+k0s.0", "v1.33.0+k0s.0",
}

func TestComputeControlPlaneSteps(t *testing.T) {
	tests := []struct {
		name, from, to string
		want           []string
		wantErr        string
	}{
		{name: "same minor", from: "v1.31.1+k0s.0", to: "v1.31.2+k0s.0", want: []string{"v1.31.2+k0s.0"}},
		{name: "adjacent minor", from: "v1.30.1+k0s.0", to: "v1.31.1+k0s.0", want: []string{"v1.31.1+k0s.0"}},
		{
			name: "multiple minors uses latest patch and numeric build",
			from: "v1.29.9+k0s.0", to: "v1.32.0+k0s.0",
			want: []string{"v1.30.5+k0s.10", "v1.31.2+k0s.0", "v1.32.0+k0s.0"},
		},
		{name: "prereleases are not intermediate steps", from: "v1.31.2+k0s.0", to: "v1.33.0+k0s.0", want: []string{"v1.32.1+k0s.0", "v1.33.0+k0s.0"}},
		{name: "build bump only", from: "v1.31.1+k0s.0", to: "v1.31.1+k0s.1", want: []string{"v1.31.1+k0s.1"}},
		{name: "missing suffix is normalized by handler input", from: "v1.31.1+k0s.0", to: "v1.31.2+k0s.0", want: []string{"v1.31.2+k0s.0"}},
		{name: "downgrade", from: "v1.31.2+k0s.0", to: "v1.31.1+k0s.0", wantErr: "must be greater"},
		{name: "missing minor", from: "v1.27.0+k0s.0", to: "v1.30.1+k0s.0", wantErr: "no k0s release found for v1.28"},
		{name: "major change", from: "v1.31.1+k0s.0", to: "v2.0.0+k0s.0", wantErr: "major"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeControlPlaneSteps(tt.from, tt.to, releases)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

type fakeSource struct {
	versions []string
	err      error
}

func (f fakeSource) Versions(context.Context, string) ([]string, error) { return f.versions, f.err }

func TestDoGenerateUpgradePlan(t *testing.T) {
	req := &runtimehooksv1.GenerateUpgradePlanRequest{
		FromControlPlaneKubernetesVersion: "v1.29.9",
		ToKubernetesVersion:               "v1.31.2",
	}

	resp := &runtimehooksv1.GenerateUpgradePlanResponse{}
	NewHandler(fakeSource{versions: releases}).DoGenerateUpgradePlan(context.Background(), req, resp)
	require.Equal(t, runtimehooksv1.ResponseStatusSuccess, resp.Status)
	require.Equal(t, []runtimehooksv1.UpgradeStep{{Version: "v1.30.5+k0s.10"}, {Version: "v1.31.2+k0s.0"}}, resp.ControlPlaneUpgrades)
	require.Empty(t, resp.WorkersUpgrades)

	resp = &runtimehooksv1.GenerateUpgradePlanResponse{}
	NewHandler(fakeSource{err: errors.New("boom")}).DoGenerateUpgradePlan(context.Background(), req, resp)
	require.Equal(t, runtimehooksv1.ResponseStatusFailure, resp.Status)
	require.Contains(t, resp.Message, "boom")
}
