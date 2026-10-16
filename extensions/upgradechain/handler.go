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

// Package upgradechain implements the GenerateUpgradePlan Cluster API runtime hook for k0s.
package upgradechain

import (
	"context"
	"fmt"

	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Handler generates k0s upgrade plans out of the available k0s releases.
type Handler struct {
	source VersionSource
}

// NewHandler creates a Handler that looks up available versions in source.
func NewHandler(source VersionSource) *Handler {
	return &Handler{source: source}
}

// DoGenerateUpgradePlan implements the GenerateUpgradePlan hook.
func (h *Handler) DoGenerateUpgradePlan(ctx context.Context, req *runtimehooksv1.GenerateUpgradePlanRequest, resp *runtimehooksv1.GenerateUpgradePlanResponse) {
	log := ctrl.LoggerFrom(ctx).WithValues("Cluster", req.Cluster.Name)
	log.Info("GenerateUpgradePlan is called", "from", req.FromControlPlaneKubernetesVersion, "to", req.ToKubernetesVersion)

	from := util.NormalizeK0sVersion(req.FromControlPlaneKubernetesVersion)
	to := util.NormalizeK0sVersion(req.ToKubernetesVersion)

	available, err := h.source.Versions(ctx, from)
	if err != nil {
		log.Error(err, "Failed to list available k0s versions")
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = err.Error()
		return
	}

	steps, err := computeControlPlaneSteps(from, to, available)
	if err != nil {
		log.Error(err, "Failed to compute upgrade plan")
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = err.Error()
		return
	}

	for _, s := range steps {
		resp.ControlPlaneUpgrades = append(resp.ControlPlaneUpgrades, runtimehooksv1.UpgradeStep{Version: s})
	}
	// WorkersUpgrades is left empty so that Cluster API computes the minimal worker steps.
	resp.Status = runtimehooksv1.ResponseStatusSuccess
}

// computeControlPlaneSteps returns the versions the control plane must go through to reach to. k0s only supports
// upgrading one minor at a time, so for every minor between from (excluded) and to (excluded) the latest available
// release is used, and to is always the last step.
func computeControlPlaneSteps(from, to string, available []string) ([]string, error) {
	f, err := parseK0sVersion(from)
	if err != nil {
		return nil, err
	}
	t, err := parseK0sVersion(to)
	if err != nil {
		return nil, err
	}

	if f.minor()[0] != t.minor()[0] {
		return nil, fmt.Errorf("upgrading across major versions (%s to %s) is not supported", from, to)
	}
	if f.compare(t) >= 0 {
		return nil, fmt.Errorf("target version %s must be greater than current version %s", to, from)
	}

	// latest release per minor, stable releases only.
	latest := map[int]k0sVersion{}
	for _, a := range available {
		v, err := parseK0sVersion(a)
		if err != nil || v.isPrerelease() || v.minor()[0] != f.minor()[0] {
			continue
		}
		if cur, ok := latest[v.minor()[1]]; !ok || v.compare(cur) > 0 {
			latest[v.minor()[1]] = v
		}
	}

	var steps []string
	for minor := f.minor()[1] + 1; minor < t.minor()[1]; minor++ {
		v, ok := latest[minor]
		if !ok {
			return nil, fmt.Errorf("no k0s release found for v%d.%d, required to upgrade from %s to %s", f.minor()[0], minor, from, to)
		}
		steps = append(steps, v.raw)
	}
	return append(steps, to), nil
}
