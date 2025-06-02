package util

import (
	"context"
	"sort"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func DefaultK0smotronClusterLabels(kmc *km.Cluster) map[string]string {
	return map[string]string{
		"app":     "k0smotron",
		"cluster": kmc.Name,
	}
}

func LabelsForK0smotronCluster(kmc *km.Cluster) map[string]string {
	labels := DefaultK0smotronClusterLabels(kmc)
	for k, v := range kmc.Labels {
		labels[k] = v
	}
	labels["component"] = "cluster"
	return labels
}

func LabelsForK0smotronControlPlane(kmc *km.Cluster) map[string]string {
	labels := LabelsForK0smotronCluster(kmc)
	labels["cluster.x-k8s.io/control-plane"] = "true"
	return labels
}

func LabelsForEtcdK0smotronCluster(kmc *km.Cluster) map[string]string {
	labels := LabelsForK0smotronCluster(kmc)
	labels["component"] = "etcd"
	return labels
}

func AnnotationsForK0smotronCluster(kmc *km.Cluster) map[string]string {
	return kmc.Annotations
}

// AddToExistingSans merges original sans list with a new sans slice avoiding duplicated values.
func AddToExistingSans(existing []string, new []string) []string {
	uniques := make(map[string]struct{})
	for _, val := range existing {
		uniques[val] = struct{}{}
	}
	for _, val := range new {
		uniques[val] = struct{}{}
	}
	finalSans := make([]string, 0, len(uniques))
	for key := range uniques {
		finalSans = append(finalSans, key)
	}

	// Sort the sans to ensure stable output order
	sort.Strings(finalSans)

	return finalSans
}

// EnsureFinalizer adds a finalizer if the object doesn't have a deletionTimestamp set
// and if the finalizer is not already set.
// This util is usually used in reconcilers directly after the reconciled object was retrieved
// and before pause is handled or "defer patch" with the patch helper.
//
// TODO: This function is copied from https://github.com/kubernetes-sigs/cluster-api/blob/v1.9.0/util/finalizers/finalizers.go.
// Use it once the CAPI dependency is bumped to >=v1.9.0.
func EnsureFinalizer(ctx context.Context, c client.Client, o client.Object, finalizer string) (finalizerAdded bool, err error) {
	// Finalizers can only be added when the deletionTimestamp is not set.
	if !o.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	if controllerutil.ContainsFinalizer(o, finalizer) {
		return false, nil
	}

	patchHelper, err := patch.NewHelper(o, c)
	if err != nil {
		return false, err
	}

	controllerutil.AddFinalizer(o, finalizer)

	if err := patchHelper.Patch(ctx, o); err != nil {
		return false, err
	}

	return true, nil
}
