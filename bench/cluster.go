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

package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkOperatorReady verifies the k0smotron controller-manager deployment is
// present and has ready replicas. Fails fast so we don't waste time watching
// StatefulSets that will never be created.
func checkOperatorReady(ctx context.Context, kc *kubernetes.Clientset) error {
	const ns = "k0smotron"
	const name = "k0smotron-controller-manager"

	dep, err := kc.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s/%s not found — install k0smotron operator: "+
				"kubectl apply -f https://docs.k0smotron.io/stable/install.yaml --server-side=true", ns, name)
		}
		return fmt.Errorf("get operator deployment: %w", err)
	}
	if dep.Status.ReadyReplicas < 1 {
		return fmt.Errorf("deployment %s/%s has 0 ready replicas", ns, name)
	}
	return nil
}

// ClusterTiming records the measured provisioning latency for a single HCP.
type ClusterTiming struct {
	Name      string
	StartTime time.Time
	ReadyTime time.Time
	Duration  time.Duration
}

// clusterBody is the JSON body sent to the k0smotron API.
type clusterBody struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   clusterMeta `json:"metadata"`
	Spec       clusterSpec `json:"spec"`
}

type clusterMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type clusterSpec struct {
	Replicas        int32          `json:"replicas"`
	Version         string         `json:"version"`
	ExternalAddress string         `json:"externalAddress,omitempty"`
	K0sConfig       map[string]any `json:"k0sConfig,omitempty"`
	Service         km.ServiceSpec `json:"service"`
	Storage         km.StorageSpec `json:"storage"`
}

// createCluster POSTs a new k0smotron Cluster resource via the raw REST client.
func createCluster(ctx context.Context, kc *kubernetes.Clientset, name, ns string, cfg ScenarioConfig) error {
	svcType := cfg.ServiceType
	if svcType == "" {
		svcType = "ClusterIP"
	}
	body := clusterBody{
		APIVersion: "k0smotron.io/v1beta2",
		Kind:       "Cluster",
		Metadata: clusterMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: clusterSpec{
			Replicas:        1,
			Version:         cfg.K0sVersion,
			ExternalAddress: cfg.ExternalAddress,
			K0sConfig:       k0sConfigWithSANs(cfg.APISANs),
			Service: km.ServiceSpec{
				Type:             svcType,
				APIPort:          hcpAPINodePort,
				KonnectivityPort: 30132,
			},
			Storage: km.StorageSpec{
				Type: cfg.StorageType,
				Kine: cfg.StorageKine,
				Etcd: cfg.StorageEtcd,
				NATS: cfg.StorageNATS,
			},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal cluster body: %w", err)
	}

	path := fmt.Sprintf("/apis/k0smotron.io/v1beta2/namespaces/%s/clusters", ns)
	return kc.RESTClient().
		Post().
		AbsPath(path).
		Body(data).
		Do(ctx).
		Error()
}

func k0sConfigWithSANs(sans []string) map[string]any {
	if len(sans) == 0 {
		return nil
	}
	return map[string]any{
		"apiVersion": "k0s.k0sproject.io/v1beta1",
		"kind":       "ClusterConfig",
		"spec": map[string]any{
			"api": map[string]any{
				"sans": sans,
			},
		},
	}
}

// deleteCluster DELETEs the named k0smotron Cluster resource.
func deleteCluster(ctx context.Context, kc *kubernetes.Clientset, name, ns string) error {
	path := fmt.Sprintf("/apis/k0smotron.io/v1beta2/namespaces/%s/clusters/%s", ns, name)
	return kc.RESTClient().
		Delete().
		AbsPath(path).
		Do(ctx).
		Error()
}

func waitClusterDeleted(ctx context.Context, kc *kubernetes.Clientset, name, ns string) error {
	path := fmt.Sprintf("/apis/k0smotron.io/v1beta2/namespaces/%s/clusters/%s", ns, name)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		err := kc.RESTClient().
			Get().
			AbsPath(path).
			Do(ctx).
			Error()
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get cluster %s while waiting for deletion: %w", name, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// waitClusterReady polls the StatefulSet for the cluster until all desired replicas are ready.
// The StatefulSet name follows the k0smotron convention: "kmc-<clusterName>".
// The caller should set an appropriate deadline on ctx.
func waitClusterReady(ctx context.Context, kc *kubernetes.Clientset, name, ns string) error {
	stsName := "kmc-" + name
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		sts, err := kc.AppsV1().StatefulSets(ns).Get(ctx, stsName, metav1.GetOptions{})
		if err == nil {
			desired := int32(1)
			if sts.Spec.Replicas != nil {
				desired = *sts.Spec.Replicas
			}
			if sts.Status.ReadyReplicas >= desired {
				return nil
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get statefulset %s: %w", stsName, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// createClusters parallel-creates cfg.ClusterCount clusters and waits for each to become
// ready. It returns per-cluster timing records.
func createClusters(ctx context.Context, kc *kubernetes.Clientset, cfg ScenarioConfig) ([]ClusterTiming, error) {
	timings := make([]ClusterTiming, cfg.ClusterCount)
	sem := make(chan struct{}, cfg.Parallelism)

	eg, egCtx := errgroup.WithContext(ctx)
	for i := 0; i < cfg.ClusterCount; i++ {
		i := i
		eg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			clusterName := fmt.Sprintf("hcp-%03d", i)
			start := time.Now()

			if err := createCluster(egCtx, kc, clusterName, cfg.Namespace, cfg); err != nil {
				// Tolerate AlreadyExists so reruns in the same namespace work.
				if !apierrors.IsAlreadyExists(err) {
					return fmt.Errorf("create cluster %s: %w", clusterName, err)
				}
			}

			waitCtx, cancel := context.WithTimeout(egCtx, 15*time.Minute)
			defer cancel()

			if err := waitClusterReady(waitCtx, kc, clusterName, cfg.Namespace); err != nil {
				return fmt.Errorf("wait cluster %s ready: %w", clusterName, err)
			}

			ready := time.Now()
			timings[i] = ClusterTiming{
				Name:      clusterName,
				StartTime: start,
				ReadyTime: ready,
				Duration:  ready.Sub(start),
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return timings, nil
}

// deleteAllClusters lists all k0smotron Cluster resources in the namespace and deletes them.
func deleteAllClusters(ctx context.Context, kc *kubernetes.Clientset, ns string) error {
	path := fmt.Sprintf("/apis/k0smotron.io/v1beta2/namespaces/%s/clusters", ns)

	raw, err := kc.RESTClient().
		Get().
		AbsPath(path).
		DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}

	var list struct {
		Items []struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("parse cluster list: %w", err)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	for _, item := range list.Items {
		name := item.Metadata.Name
		eg.Go(func() error {
			if err := deleteCluster(egCtx, kc, name, ns); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete cluster %s: %w", name, err)
			}
			return nil
		})
	}
	return eg.Wait()
}
