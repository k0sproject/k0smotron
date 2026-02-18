package k0smotronio

import (
	"context"
	"fmt"
	"time"

	k0smotroniov1beta1 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type statusScope struct {
	message        string
	reason         string
	kmcStatefulSet *apps.StatefulSet
}

func (r *ClusterReconciler) updateStatus(ctx context.Context, kmc *k0smotroniov1beta1.Cluster, scope statusScope) {

	if scope.kmcStatefulSet != nil {
		kmc.Status.ReadyReplicas = scope.kmcStatefulSet.Status.ReadyReplicas
		kmc.Status.Replicas = scope.kmcStatefulSet.Status.Replicas
		selector, _ := metav1.LabelSelectorAsSelector(scope.kmcStatefulSet.Spec.Selector)
		kmc.Status.Selector = selector.String()
	}

	setAvailableCondition(ctx, r.Client, kmc)
	setDeletingCondition(kmc, scope.reason, scope.message)
}

func setAvailableCondition(ctx context.Context, c client.Client, kmc *k0smotroniov1beta1.Cluster) {
	workloadClusterClient, err := remote.NewClusterClient(ctx, "k0smotron", c, client.ObjectKeyFromObject(kmc))
	if err != nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.ClusterAvailableInternalErrorReason,
			Message: fmt.Sprintf("Failed to create workload cluster client: %v", err),
		})
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// If we can get 'kube-system' namespace, it's safe to say the API is up-and-running
	ns := &corev1.Namespace{}
	nsKey := types.NamespacedName{
		Namespace: "",
		Name:      "kube-system",
	}
	err = workloadClusterClient.Get(pingCtx, nsKey, ns)
	if err != nil {
		if apierrors.IsNotFound(err) {
			conditions.Set(kmc, metav1.Condition{
				Type:    k0smotroniov1beta1.ClusterAvailableCondition,
				Status:  metav1.ConditionFalse,
				Reason:  k0smotroniov1beta1.ClusterAvailableKubeSystemNamespaceNotFoundReason,
				Message: "Namespace 'kube-system' not found",
			})
			return
		}
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.ClusterAvailableInternalErrorReason,
			Message: fmt.Sprintf("Failed to get namespace 'kube-system': %v", err),
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:   k0smotroniov1beta1.ClusterAvailableCondition,
		Status: metav1.ConditionTrue,
		Reason: k0smotroniov1beta1.ClusterAvailableReason,
	})
}

func setDeletingCondition(kmc *k0smotroniov1beta1.Cluster, reason, message string) {
	if kmc.DeletionTimestamp.IsZero() {
		conditions.Set(kmc, metav1.Condition{
			Type:   k0smotroniov1beta1.ClusterDeletingCondition,
			Status: metav1.ConditionFalse,
			Reason: k0smotroniov1beta1.ClusterNotDeletingReason,
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:    k0smotroniov1beta1.ClusterDeletingCondition,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}
