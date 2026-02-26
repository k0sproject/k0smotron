package k0smotronio

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudflare/cfssl/log"
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

func (r *ClusterReconciler) updateStatus(ctx context.Context, kmc *k0smotroniov1beta1.Cluster, scope currentReconcileState) {
	// Set the status fields based on the current state of the cluster
	translateStatusFromStatefulSet(kmc, scope.controlplane.sts)
	// Set conditions based on the current state of the cluster
	setControlPlaneScalingCondition(kmc, scope.controlplane.sts)
	setControlPlaneUpToDateCondition(kmc, scope.controlplane.sts)
	setControlPlaneExposedCondition(kmc, scope.controlplane.svc)
	setControlPlaneKubeconfigAvailableCondition(kmc, scope.controlplane)
	setControlPlaneFunctionalCondition(ctx, r.Client, kmc, scope.controlplane.sts)
	setCertificatesAvailableCondition(kmc)
	setDeletingCondition(kmc, scope.reason, scope.message)
	setAvailableCondition(kmc)
}

func translateStatusFromStatefulSet(kmc *k0smotroniov1beta1.Cluster, sts *apps.StatefulSet) {
	if sts != nil {
		kmc.Status.ReadyReplicas = sts.Status.ReadyReplicas
		kmc.Status.Replicas = sts.Status.Replicas
		kmc.Status.UpdatedReplicas = sts.Status.UpdatedReplicas
		selector, _ := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		kmc.Status.Selector = selector.String()
	}
}

func setAvailableCondition(kmc *k0smotroniov1beta1.Cluster) {
	summaryOpts := []conditions.SummaryOption{
		conditions.ForConditionTypes{
			k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
			k0smotroniov1beta1.ClusterKubeconfigSecretAvailableCondition,
			k0smotroniov1beta1.ClusterControlPlaneExposedCondition,
		},
		// Instruct summary to consider Deleting condition with negative polarity.
		conditions.NegativePolarityConditionTypes{k0smotroniov1beta1.ClusterDeletingCondition},
	}
	availableCondition, err := conditions.NewSummaryCondition(kmc, k0smotroniov1beta1.ClusterAvailableCondition, summaryOpts...)
	if err != nil {
		log.Error(err, "Failed to set Available condition")
		availableCondition = &metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterAvailableCondition,
			Status:  metav1.ConditionUnknown,
			Reason:  k0smotroniov1beta1.InternalErrorReason,
			Message: "Please check controller logs for errors",
		}
	}

	conditions.Set(kmc, *availableCondition)

}

func setCertificatesAvailableCondition(kmc *k0smotroniov1beta1.Cluster) {
	if len(kmc.Spec.CertificateRefs) == 0 {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterCertificatesAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.NotFoundReason,
			Message: "Check controller logs for more details",
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:   k0smotroniov1beta1.ClusterCertificatesAvailableCondition,
		Status: metav1.ConditionTrue,
		Reason: k0smotroniov1beta1.CreatedReason,
	})
}

func setControlPlaneKubeconfigAvailableCondition(kmc *k0smotroniov1beta1.Cluster, controlPlaneState controlplaneState) {
	reason := k0smotroniov1beta1.InternalErrorReason
	if controlPlaneState.kubeconfig.message == "" {
		if controlPlaneState.sts == nil {
			controlPlaneState.kubeconfig.message = "Control plane StatefulSet not found"
		} else if controlPlaneState.sts.Status.ReadyReplicas == 0 {
			controlPlaneState.kubeconfig.message = "It is needed to have at least 1 ready replica to generate the workload kubeconfig"
			reason = k0smotroniov1beta1.ClusterControlPlaneNoReadyReplicasReason
		} else {
			controlPlaneState.kubeconfig.message = "Please check controller logs for errors"
		}
	}

	if controlPlaneState.kubeconfig.data == nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterKubeconfigSecretAvailableCondition,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: controlPlaneState.kubeconfig.message,
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:   k0smotroniov1beta1.ClusterKubeconfigSecretAvailableCondition,
		Status: metav1.ConditionTrue,
		Reason: k0smotroniov1beta1.CreatedReason,
	})
}

func setControlPlaneExposedCondition(kmc *k0smotroniov1beta1.Cluster, svc *corev1.Service) {
	if svc == nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneExposedCondition,
			Status:  metav1.ConditionUnknown,
			Reason:  k0smotroniov1beta1.NotFoundReason,
			Message: "Check controller logs for more details",
		})
		return
	}

	if kmc.Spec.ExternalAddress != "" {
		conditions.Set(kmc, metav1.Condition{
			Type:   k0smotroniov1beta1.ClusterControlPlaneExposedCondition,
			Status: metav1.ConditionTrue,
			Reason: k0smotroniov1beta1.ClusterControlPlaneExposedReason,
		})
	} else {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneExposedCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneExposedReason,
			Message: "Waiting for external address. Check controller logs for more details.",
		})
	}

	// TODO: check ingress required resources are deployed accordingly.
}

func setControlPlaneScalingCondition(kmc *k0smotroniov1beta1.Cluster, sts *apps.StatefulSet) {
	if sts == nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.NotFoundReason,
			Message: "Check controller logs for more details",
		})
		return
	}

	if kmc.Spec.Replicas > sts.Status.Replicas {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
			Status:  metav1.ConditionTrue,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneScaledUpReason,
			Message: fmt.Sprintf("Control plane is scaling up: %d/%d replicas required", sts.Status.Replicas, kmc.Spec.Replicas),
		})
		return
	}

	if sts.Status.Replicas > kmc.Spec.Replicas {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
			Status:  metav1.ConditionTrue,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneScaledDownReason,
			Message: fmt.Sprintf("Control plane is scaling down: %d/%d replicas desired", sts.Status.Replicas, kmc.Spec.Replicas),
		})
		return
	}

	if sts.Status.UpdatedReplicas < kmc.Spec.Replicas {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
			Status:  metav1.ConditionTrue,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneScaledUpReason,
			Message: fmt.Sprintf("Control plane is scaling up: %d/%d replicas created", sts.Status.UpdatedReplicas, kmc.Spec.Replicas),
		})
		return
	}

	if sts.Status.UpdatedReplicas > kmc.Spec.Replicas {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
			Status:  metav1.ConditionTrue,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneScaledDownReason,
			Message: fmt.Sprintf("Control plane is scaling down: %d/%d replicas desired", sts.Status.UpdatedReplicas, kmc.Spec.Replicas),
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:   k0smotroniov1beta1.ClusterControlPlaneScalingCondition,
		Status: metav1.ConditionFalse,
		Reason: k0smotroniov1beta1.ClusterControlPlaneNotScalingReason,
	})
}

func setControlPlaneUpToDateCondition(kmc *k0smotroniov1beta1.Cluster, sts *apps.StatefulSet) {
	if sts == nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneUpToDateCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.NotFoundReason,
			Message: "Check controller logs for more details",
		})
		return
	}

	if kmc.Spec.Replicas != sts.Status.UpdatedReplicas {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneUpToDateCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneNotAllReplicasUpToDateReason,
			Message: fmt.Sprintf("Control plane is updating: %d/%d replicas with desired template", sts.Status.UpdatedReplicas, kmc.Spec.Replicas),
		})
	} else {
		conditions.Set(kmc, metav1.Condition{
			Type:   k0smotroniov1beta1.ClusterControlPlaneUpToDateCondition,
			Status: metav1.ConditionTrue,
			Reason: k0smotroniov1beta1.ClusterControlPlaneReplicasUpToDateReason,
		})
	}
}

func setControlPlaneFunctionalCondition(ctx context.Context, c client.Client, kmc *k0smotroniov1beta1.Cluster, sts *apps.StatefulSet) {
	if sts == nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.NotFoundReason,
			Message: "Check controller logs for more details",
		})
		return
	}

	if !conditions.IsTrue(kmc, k0smotroniov1beta1.ClusterKubeconfigSecretAvailableCondition) {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.ClusterControlPlaneFunctionalReason,
			Message: "Waiting for kubeconfig secret to be available",
		})
		return
	}

	workloadClusterClient, err := remote.NewClusterClient(ctx, "k0smotron", c, client.ObjectKeyFromObject(kmc))
	if err != nil {
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.InternalErrorReason,
			Message: fmt.Sprintf("failed to create workload cluster client: %v", err),
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
				Type:    k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
				Status:  metav1.ConditionFalse,
				Reason:  k0smotroniov1beta1.ClusterControlPlaneFunctionalReason,
				Message: "Namespace 'kube-system' not found",
			})
			return
		}
		conditions.Set(kmc, metav1.Condition{
			Type:    k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
			Status:  metav1.ConditionFalse,
			Reason:  k0smotroniov1beta1.InternalErrorReason,
			Message: fmt.Sprintf("failed to get namespace 'kube-system': %v", err),
		})
		return
	}

	conditions.Set(kmc, metav1.Condition{
		Type:   k0smotroniov1beta1.ClusterControlPlaneFunctionalCondition,
		Status: metav1.ConditionTrue,
		Reason: k0smotroniov1beta1.ClusterControlPlaneFunctionalReason,
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
