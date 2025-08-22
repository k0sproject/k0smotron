package util

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureExternalOwner ensures that an external owner resource with the given name exists in the specified namespace.
// An external owner resource is used as owner reference for objects that are not in the management cluster. That way
// we can garbage collect all objects related to a k0smotron cluster deployed in an external cluster by deleting the
// external owner.
func EnsureExternalOwner(ctx context.Context, name, namespace string, c client.Client) (client.Object, error) {
	externalOwner, err := GetExternalOwner(ctx, name, namespace, c)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getExternalOwnerName(name),
					Namespace: namespace,
				},
				Data: map[string]string{},
			}

			err := c.Create(ctx, cm)
			if err != nil {
				return nil, err
			}

			return cm, nil
		}
		return nil, err
	}

	return externalOwner, nil
}

// GetExternalOwner retrieves the external owner used in the external cluster for the given name and namespace.
func GetExternalOwner(ctx context.Context, name, namespace string, c client.Client) (client.Object, error) {
	externalOwner := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{Name: getExternalOwnerName(name), Namespace: namespace}, externalOwner)
	if err != nil {
		return nil, err
	}

	return externalOwner, nil
}

// SetExternalOwnerReference sets the owner reference for the given object trying to use the external owner if provided.
func SetExternalOwnerReference(owner metav1.Object, controlled metav1.Object, scheme *runtime.Scheme, externalOwner metav1.Object) error {
	if externalOwner != nil {
		return ctrl.SetControllerReference(externalOwner, controlled, scheme)
	}

	return ctrl.SetControllerReference(owner, controlled, scheme)
}

// GetExternalControllerRef returns the controller reference for the given external owner object.
func GetExternalControllerRef(externalOwner metav1.Object) *metav1.OwnerReference {
	return metav1.NewControllerRef(externalOwner, corev1.SchemeGroupVersion.WithKind("ConfigMap"))
}

func getExternalOwnerName(kmcName string) string {
	return kmcName + "-root-owner"
}
