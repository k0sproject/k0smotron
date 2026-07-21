//go:build e2e

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

package util

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	reconcileMetricName = "controller_runtime_reconcile_total"
	metricsRemotePort   = 8080
)

// ReconcileCounters maps controller name (from the controller-runtime "controller" label)
// to a reconcile-total count.
type ReconcileCounters map[string]float64

// PodReconcileCounters maps pod name -> per-controller reconcile counts. Keeping pod
// identity lets a later Diff handle pod restarts (counter resets to zero in the new pod).
type PodReconcileCounters map[string]ReconcileCounters

// Total returns counts summed across pods.
func (p PodReconcileCounters) Total() ReconcileCounters {
	out := ReconcileCounters{}
	for _, c := range p {
		for k, v := range c {
			out[k] += v
		}
	}
	return out
}

// Diff returns per-controller delta vs prev, summed across pods. Pods present in cur
// but not in prev contribute their full count (treated as new). A negative delta within
// a pod (counter regression after a restart inside the window) is treated as the current
// value.
func (p PodReconcileCounters) Diff(prev PodReconcileCounters) ReconcileCounters {
	out := ReconcileCounters{}
	for pod, cur := range p {
		base := prev[pod]
		for k, v := range cur {
			d := v - base[k]
			if d < 0 {
				d = v
			}
			out[k] += d
		}
	}
	return out
}

// EnableInsecureMetricsForNamespace patches every Deployment in the namespace so that
// the manager container exposes its metrics endpoint over plain HTTP without auth. This
// is for e2e only — production manifests keep auth+TLS. Waits for rollout.
func EnableInsecureMetricsForNamespace(ctx context.Context, cs kubernetes.Interface, namespace string) error {
	deps, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app!=extension-webhook",
	})
	if err != nil {
		return fmt.Errorf("list deployments in %s: %w", namespace, err)
	}
	if len(deps.Items) == 0 {
		return fmt.Errorf("no deployments in namespace %s", namespace)
	}

	patched := false
	for i := range deps.Items {
		d := &deps.Items[i]
		if !ensureInsecureMetricsArgs(d) {
			continue
		}
		if _, err := cs.AppsV1().Deployments(namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("patch deployment %s/%s: %w", namespace, d.Name, err)
		}
		patched = true
	}
	if !patched {
		return nil
	}

	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		fresh, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for i := range fresh.Items {
			d := &fresh.Items[i]
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			if d.Generation != d.Status.ObservedGeneration ||
				d.Status.UpdatedReplicas < desired ||
				d.Status.AvailableReplicas < desired ||
				d.Status.Replicas != d.Status.UpdatedReplicas {
				return false, nil
			}
		}
		return true, nil
	})
}

func ensureInsecureMetricsArgs(d *appsv1.Deployment) bool {
	changed := false
	for ci := range d.Spec.Template.Spec.Containers {
		c := &d.Spec.Template.Spec.Containers[ci]
		hasInsecure := false
		for _, a := range c.Args {
			if strings.HasPrefix(a, "--insecure-diagnostics") {
				hasInsecure = true
				break
			}
		}
		if !hasInsecure {
			c.Args = append(c.Args, "--insecure-diagnostics=true")
			changed = true
		}
	}
	return changed
}

// scrapePodReconcileCounters fetches /metrics from a pod via the apiserver pod-proxy
// (delegating TLS and auth to the apiserver) and returns reconcile counts aggregated by
// controller label.
func scrapePodReconcileCounters(ctx context.Context, cs kubernetes.Interface, pod, namespace string) (ReconcileCounters, error) {
	data, err := cs.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		Name(fmt.Sprintf("%s:%d", pod, metricsRemotePort)).
		SubResource("proxy").
		Suffix("metrics").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("scrape metrics %s/%s: %w", namespace, pod, err)
	}
	return parseReconcileCounters(bytes.NewReader(data))
}

// ScrapeDeploymentReconcileCounters returns counters keyed by pod for every Running pod
// behind the deployment's selector.
func ScrapeDeploymentReconcileCounters(ctx context.Context, cs kubernetes.Interface, deployment, namespace string) (PodReconcileCounters, error) {
	dep, err := cs.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", namespace, deployment, err)
	}
	if dep.Spec.Selector == nil {
		return nil, fmt.Errorf("deployment %s/%s has no selector", namespace, deployment)
	}
	sel := labels.SelectorFromSet(dep.Spec.Selector.MatchLabels).String()
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, fmt.Errorf("list pods for %s/%s: %w", namespace, deployment, err)
	}

	out := PodReconcileCounters{}
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		c, err := scrapePodReconcileCounters(ctx, cs, p.Name, namespace)
		if err != nil {
			return nil, err
		}
		out[p.Name] = c
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Running pods scraped for deployment %s/%s", namespace, deployment)
	}
	return out, nil
}

func parseReconcileCounters(r io.Reader) (ReconcileCounters, error) {
	out := ReconcileCounters{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, reconcileMetricName+"{") {
			continue
		}
		controller := extractPromLabel(line, "controller")
		if controller == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("malformed metrics line %q", line)
		}
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse value %q: %w", line, err)
		}
		out[controller] += v
	}
	return out, scanner.Err()
}

func extractPromLabel(line, name string) string {
	needle := name + `="`
	i := strings.Index(line, needle)
	if i < 0 {
		return ""
	}
	start := i + len(needle)
	j := strings.Index(line[start:], `"`)
	if j < 0 {
		return ""
	}
	return line[start : start+j]
}

type deploymentRef struct{ Namespace, Name string }

func (d deploymentRef) key() string { return d.Namespace + "/" + d.Name }

// ReconcileStormGuard captures a baseline of per-pod reconcile counts across one or more
// controller-manager deployments and, on Assert, flags any controller whose reconcile-rate
// over the elapsed window exceeds a threshold.
type ReconcileStormGuard struct {
	cs          kubernetes.Interface
	deployments []deploymentRef

	baseline   map[string]PodReconcileCounters // keyed by deploymentRef.key()
	baselineAt time.Time
}

// NewReconcileStormGuard snapshots the current counters for a single deployment as a
// baseline. Use Assert later in the same test to validate the rate-of-change.
func NewReconcileStormGuard(ctx context.Context, cs kubernetes.Interface, deployment, namespace string) (*ReconcileStormGuard, error) {
	return newGuardForRefs(ctx, cs, []deploymentRef{{Namespace: namespace, Name: deployment}})
}

// NewReconcileStormGuardForDeployments snapshots the current counters for every supplied
// controller-manager deployment.
func NewReconcileStormGuardForDeployments(ctx context.Context, cs kubernetes.Interface, deployments ...*appsv1.Deployment) (*ReconcileStormGuard, error) {
	refs := make([]deploymentRef, 0, len(deployments))
	for _, d := range deployments {
		refs = append(refs, deploymentRef{Namespace: d.GetNamespace(), Name: d.GetName()})
	}
	return newGuardForRefs(ctx, cs, refs)
}

// NewReconcileStormGuardForNamespace snapshots the current counters for every Deployment
// found in the given namespace. Intended for the k0smotron-controller namespace where the
// only deployments are k0smotron's own controllers.
func NewReconcileStormGuardForNamespace(ctx context.Context, cs kubernetes.Interface, namespace string) (*ReconcileStormGuard, error) {
	deps, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app!=extension-webhook",
	})
	if err != nil {
		return nil, fmt.Errorf("list deployments in %s: %w", namespace, err)
	}
	if len(deps.Items) == 0 {
		return nil, fmt.Errorf("no deployments in namespace %s", namespace)
	}
	refs := make([]deploymentRef, 0, len(deps.Items))
	for i := range deps.Items {
		refs = append(refs, deploymentRef{Namespace: deps.Items[i].GetNamespace(), Name: deps.Items[i].GetName()})
	}
	return newGuardForRefs(ctx, cs, refs)
}

func newGuardForRefs(ctx context.Context, cs kubernetes.Interface, refs []deploymentRef) (*ReconcileStormGuard, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("no deployments supplied")
	}
	base := make(map[string]PodReconcileCounters, len(refs))
	for _, r := range refs {
		c, err := ScrapeDeploymentReconcileCounters(ctx, cs, r.Name, r.Namespace)
		if err != nil {
			return nil, err
		}
		base[r.key()] = c
	}
	return &ReconcileStormGuard{
		cs: cs, deployments: refs,
		baseline: base, baselineAt: time.Now(),
	}, nil
}

// Assert scrapes counters for every tracked deployment, computes per-controller reconcile
// rate since baseline and fails the test if any controller exceeds perControllerMax
// (defaulting to defaultMax). perControllerMax may be nil. Logs all observed rates.
func (g *ReconcileStormGuard) Assert(ctx context.Context, t *testing.T, defaultMax float64, perControllerMax map[string]float64) ReconcileCounters {
	t.Helper()
	elapsed := time.Since(g.baselineAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	total := ReconcileCounters{}
	for _, r := range g.deployments {
		cur, err := ScrapeDeploymentReconcileCounters(ctx, g.cs, r.Name, r.Namespace)
		require.NoError(t, err)
		delta := cur.Diff(g.baseline[r.key()])
		for controller, d := range delta {
			max := defaultMax
			if v, ok := perControllerMax[controller]; ok {
				max = v
			}
			rate := d / elapsed
			t.Logf("reconcile: deployment=%s/%s controller=%s delta=%.0f elapsed=%.1fs rate=%.2f/s threshold=%.2f/s", r.Namespace, r.Name, controller, d, elapsed, rate, max)
			if rate > max {
				t.Errorf("reconcile storm: deployment=%s/%s controller=%s delta=%.0f elapsed=%.1fs rate=%.2f/s exceeds %.2f/s", r.Namespace, r.Name, controller, d, elapsed, rate, max)
			}
			total[controller] += d
		}
	}
	return total
}
