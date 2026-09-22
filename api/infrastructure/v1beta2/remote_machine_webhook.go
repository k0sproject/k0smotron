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
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-pooledremotemachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines,verbs=create;update,versions=v1beta2,name=validate-pooledremotemachine-v1beta2.k0smotron.io,admissionReviewVersions=v1

// PooledRemoteMachineValidator prevents changing the snapshot used by a
// RemoteMachine after the pooled machine has been claimed.
// +kubebuilder:object:generate=false
type PooledRemoteMachineValidator struct{}

var _ admission.Validator[*PooledRemoteMachine] = &PooledRemoteMachineValidator{}

// ValidateCreate accepts a PooledRemoteMachine without additional validation.
func (v *PooledRemoteMachineValidator) ValidateCreate(_ context.Context, obj *PooledRemoteMachine) (admission.Warnings, error) {
	if obj == nil {
		return nil, fmt.Errorf("expected a PooledRemoteMachine object but got nil")
	}
	return nil, nil
}

// ValidateUpdate prevents changes to the spec of a reserved PooledRemoteMachine.
func (v *PooledRemoteMachineValidator) ValidateUpdate(_ context.Context, oldMachine, newMachine *PooledRemoteMachine) (admission.Warnings, error) {
	if oldMachine == nil {
		return nil, fmt.Errorf("expected an old PooledRemoteMachine object but got nil")
	}
	if newMachine == nil {
		return nil, fmt.Errorf("expected a new PooledRemoteMachine object but got nil")
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

// ValidateDelete accepts deletion of a PooledRemoteMachine.
func (v *PooledRemoteMachineValidator) ValidateDelete(_ context.Context, _ *PooledRemoteMachine) (admission.Warnings, error) {
	return nil, nil
}

// SetupPooledRemoteMachineWebhookWithManager registers the webhook for pooled remote machines in the manager.
func SetupPooledRemoteMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &PooledRemoteMachine{}).
		WithValidator(&PooledRemoteMachineValidator{}).
		Complete()
}
