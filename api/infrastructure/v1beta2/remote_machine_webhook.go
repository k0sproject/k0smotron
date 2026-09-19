/*

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

package v1beta2

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-pooledremotemachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines,verbs=create;update,versions=v1beta2,name=validate-pooledremotemachine-v1beta2.k0smotron.io,admissionReviewVersions=v1

// PooledRemoteMachineValidator prevents changing the snapshot used by a
// RemoteMachine after the pooled machine has been claimed.
type PooledRemoteMachineValidator struct{}

var _ webhook.CustomValidator = &PooledRemoteMachineValidator{}

func (v *PooledRemoteMachineValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	if _, ok := obj.(*PooledRemoteMachine); !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a PooledRemoteMachine but got a %T", obj))
	}
	return nil, nil
}

func (v *PooledRemoteMachineValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldMachine, ok := oldObj.(*PooledRemoteMachine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an old PooledRemoteMachine but got a %T", oldObj))
	}
	newMachine, ok := newObj.(*PooledRemoteMachine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a new PooledRemoteMachine but got a %T", newObj))
	}

	if oldMachine.Status.Reserved && !reflect.DeepEqual(oldMachine.Spec, newMachine.Spec) {
		return nil, apierrors.NewInvalid(
			GroupVersion.WithKind("PooledRemoteMachine").GroupKind(),
			newMachine.Name,
			field.ErrorList{field.Forbidden(field.NewPath("spec"), "cannot be changed while the pooled remote machine is reserved")},
		)
	}

	return nil, nil
}

func (v *PooledRemoteMachineValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	if _, ok := obj.(*PooledRemoteMachine); !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a PooledRemoteMachine but got a %T", obj))
	}
	return nil, nil
}

// SetupPooledRemoteMachineWebhookWithManager registers the webhook for pooled remote machines in the manager.
func SetupPooledRemoteMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&PooledRemoteMachine{}).
		WithValidator(&PooledRemoteMachineValidator{}).
		Complete()
}
