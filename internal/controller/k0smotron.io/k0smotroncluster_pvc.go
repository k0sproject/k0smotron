package k0smotronio

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

func (r *ClusterReconciler) reconcilePVC(ctx context.Context, kmc km.Cluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PVC")

	// volumeClaimTemplates are immutable, so we need to
	// update PVC, then delete the StatefulSet with --cascade=orphan and recreate it

	err := r.reconcileControlPlanePVC(ctx, kmc)
	if err != nil {
		return fmt.Errorf("failed to reconcile control plane PVC: %w", err)
	}
	err = r.reconcileEtcdPVC(ctx, kmc)
	if err != nil {
		return fmt.Errorf("failed to reconcile control plane PVC: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) reconcileControlPlanePVC(ctx context.Context, kmc km.Cluster) error {
	// Do nothing if the persistence type is not PVC
	if kmc.Spec.Persistence.Type != "pvc" {
		return nil
	}

	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: kmc.GetStatefulSetName()}, &sts)
	if err != nil {
		// Do nothing if StatefulSet does not exist yet
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to get statefulset: %w", err)
	}

	// Do nothing if the sizes match
	if kmc.Spec.Persistence.PersistentVolumeClaim.Spec.Resources.Requests.Storage().Cmp(*sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage()) == 0 {
		return nil
	}

	// Update the PVC size
	var allowExpansion *bool
	for i := 0; i < int(kmc.Spec.Replicas); i++ {

		if kmc.Spec.Persistence.PersistentVolumeClaim.Name == "" {
			kmc.Spec.Persistence.PersistentVolumeClaim.Name = kmc.GetVolumeName()
		}
		name := fmt.Sprintf("%s-%s-%d", kmc.Spec.Persistence.PersistentVolumeClaim.Name, kmc.GetStatefulSetName(), i)

		var pvc corev1.PersistentVolumeClaim
		err := r.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: name}, &pvc)
		if err != nil {
			return fmt.Errorf("failed to get PVC: %w", err)
		}

		if allowExpansion == nil {
			var sc storagev1.StorageClass
			err = r.Get(ctx, client.ObjectKey{Name: *pvc.Spec.StorageClassName}, &sc)
			if err != nil {
				return fmt.Errorf("failed to get StorageClass: %w", err)
			}
			allowExpansion = sc.AllowVolumeExpansion
		}

		if allowExpansion != nil && *allowExpansion {
			pvc.Spec.Resources.Requests[corev1.ResourceStorage] = kmc.Spec.Persistence.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
			err = r.Update(ctx, &pvc)
			if err != nil {
				return fmt.Errorf("failed to update PVC: %w", err)
			}

			// Remove pod to trigger file system resize
			err = r.Delete(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d", kmc.GetStatefulSetName(), i),
					Namespace: kmc.Namespace,
				},
			}, &client.DeleteOptions{})

			if err != nil {
				return fmt.Errorf("failed to delete pod for resizing: %w", err)
			}
		} else {
			break
		}
	}

	return r.Delete(ctx, &sts, &client.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationOrphan)})
}

func (r *ClusterReconciler) reconcileEtcdPVC(ctx context.Context, kmc km.Cluster) error {
	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: kmc.GetEtcdStatefulSetName()}, &sts)
	if err != nil {
		// Do nothing if StatefulSet does not exist yet
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to get etcd statefulset: %w", err)
	}

	// Do nothing if the sizes match
	if kmc.Spec.Etcd.Persistence.Size.Cmp(*sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage()) == 0 {
		return nil
	}

	// Update the PVC size
	var allowExpansion *bool
	for i := 0; i < int(calculateDesiredReplicas(&kmc)); i++ {
		var pvc corev1.PersistentVolumeClaim

		name := fmt.Sprintf("etcd-data-%s-%d", kmc.GetEtcdStatefulSetName(), i)
		err := r.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: name}, &pvc)
		if err != nil {
			return fmt.Errorf("failed to get etcd PVC: %w", err)
		}

		if allowExpansion == nil {
			var sc storagev1.StorageClass
			err = r.Get(ctx, client.ObjectKey{Name: *pvc.Spec.StorageClassName}, &sc)
			if err != nil {
				return fmt.Errorf("failed to get etcd StorageClass: %w", err)
			}
			allowExpansion = sc.AllowVolumeExpansion
		}

		if allowExpansion != nil && *allowExpansion {
			pvc.Spec.Resources.Requests[corev1.ResourceStorage] = kmc.Spec.Etcd.Persistence.Size
			err = r.Update(ctx, &pvc)
			if err != nil {
				return fmt.Errorf("failed to update etcd PVC: %w", err)
			}

			// Remove pod to trigger file system resize
			err = r.Delete(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d", kmc.GetStatefulSetName(), i),
					Namespace: kmc.Namespace,
				},
			}, &client.DeleteOptions{})

			if err != nil {
				return fmt.Errorf("failed to delete etcd pod for resizing: %w", err)
			}
		} else {
			break
		}
	}

	return r.Delete(ctx, &sts, &client.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationOrphan)})
}
