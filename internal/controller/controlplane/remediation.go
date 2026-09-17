/*
Copyright 2024.

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

package controlplane

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"k8s.io/utils/ptr"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *K0sController) reconcileUnhealthyMachines(ctx context.Context, scope *controlplane) (retErr error) {
	log := ctrl.LoggerFrom(ctx)

	healthyMachines := scope.activeMachines.Filter(isHealthy)
	// cleanup pending remediation actions not completed if the underlying machine is now back to healthy.
	// machines to be sanitized has the following conditions:
	//
	// HealthCheckSucceeded=True (current machine's state is Health)
	//         AND
	// OwnerRemediated=False (machine was marked as unhealthy previously)
	err := c.sanitizeHealthyMachines(ctx, healthyMachines)
	if err != nil {
		return err
	}
	if _, ok := scope.kcp.Annotations[cpv1beta2.RemediationInProgressAnnotation]; ok {
		log.Info("Another remediation is already in progress. Skipping remediation.")
		return nil
	}

	// retrieve machines marked as unheathy by MHC controller
	unhealthyMachines := scope.activeMachines.Filter(collections.IsUnhealthyAndOwnerRemediated)

	// no unhealthy machines to remediate. Reconciliation can move on to the next stage.
	if len(unhealthyMachines) == 0 {
		return nil
	}
	machineToBeRemediated := unhealthyMachines.Oldest()

	if !machineToBeRemediated.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Machine to remediate is being deleted.")
		return nil
	}
	log = log.WithValues("Machine", machineToBeRemediated)
	// Always patch the machine to be remediated conditions in order to inform about remediation state.
	defer func() {
		derr := c.Status().Patch(ctx, machineToBeRemediated, client.Merge)
		if derr != nil {
			log.Error(derr, "Failed to patch control plane Machine", "Machine", machineToBeRemediated.Name)
			if retErr == nil {
				retErr = errors.Wrapf(err, "failed to patch control plane Machine %s", machineToBeRemediated.Name)
			}
			return
		}
	}()
	// Ensure that the cluster remains available during and after the remediation process. The remediation must not
	// compromise the cluster's ability to serve workloads or cause disruption to the control plane's functionality.
	if ptr.Deref(scope.kcp.Status.Initialization.ControlPlaneInitialized, false) {
		// The cluster MUST have more than one replica, because this is the smallest cluster size that allows any etcd failure tolerance.
		if scope.activeMachines.Len() <= 1 {
			log.Info("A control plane machine needs remediation, but the number of current replicas is less or equal to 1. Skipping remediation", "replicas", scope.activeMachines.Len())
			conditions.Set(machineToBeRemediated, metav1.Condition{
				Type:    string(clusterv1.MachineOwnerRemediatedCondition),
				Status:  metav1.ConditionFalse,
				Reason:  clusterv1.MachineOwnerRemediatedWaitingForRemediationReason,
				Message: "KCP can't remediate if current replicas are less or equal to 1",
			})
			return nil
		}

		// The cluster MUST NOT have healthy machines still being provisioned. This rule prevents KCP taking actions while the cluster is in a transitional state.
		if isProvisioningHealthyMachine(healthyMachines) {
			log.Info("A control plane machine needs remediation, but there are other control-plane machines being provisioned. Skipping remediation")
			conditions.Set(machineToBeRemediated, metav1.Condition{
				Type:    string(clusterv1.MachineOwnerRemediatedCondition),
				Status:  metav1.ConditionFalse,
				Reason:  clusterv1.MachineOwnerRemediatedWaitingForRemediationReason,
				Message: "KCP waiting for control plane machine provisioning to complete before triggering remediation",
			})
			return nil
		}

		// The cluster MUST have no machines with a deletion timestamp. This rule prevents KCP taking actions while the cluster is in a transitional state.
		// activeMachines is built with collections.ActiveMachines, the exact negation
		// of this filter, so asking it for deleting machines never matched.
		if scope.deletedMachines.Len() > 0 {
			log.Info("A control plane machine needs remediation, but there are other control-plane machines being deleted. Skipping remediation")
			conditions.Set(machineToBeRemediated, metav1.Condition{
				Type:    string(clusterv1.MachineOwnerRemediatedCondition),
				Status:  metav1.ConditionFalse,
				Reason:  clusterv1.MachineOwnerRemediatedWaitingForRemediationReason,
				Message: "KCP waiting for control plane machine deletion to complete before triggering remediation",
			})
			return nil
		}
	}

	// After checks, remediation can be carried out.

	if err := c.deleteMachine(ctx, machineToBeRemediated.Name, scope.kcp); err != nil {
		conditions.Set(machineToBeRemediated, metav1.Condition{
			Type:    string(clusterv1.MachineOwnerRemediatedCondition),
			Status:  metav1.ConditionFalse,
			Reason:  cpv1beta2.K0sControlPlaneMachineRemediationFailedReason,
			Message: err.Error(),
		})
		return errors.Wrapf(err, "failed to delete unhealthy machine %s", machineToBeRemediated.Name)
	}
	log.Info("Remediated unhealthy machine, another new machine should take its place soon.")

	// Marks that a remediation is in progress and carries what the replacement inherits. Cleared
	// where that replacement is created, which is also where the value moves onto it.
	annotations.AddAnnotations(scope.kcp, map[string]string{
		cpv1beta2.RemediationInProgressAnnotation: remediationInProgressFor(machineToBeRemediated).marshal(),
	})

	return nil
}

// minHealthyPeriod is how long a replacement has to survive before the next failure counts as a
// new sequence rather than a retry. Upstream's DefaultMinHealthyPeriodSeconds, not yet a spec field.
const minHealthyPeriod = time.Hour

// maxReportedMachineName is the bound status puts on the machine it names, so a longer one is not
// worth carrying. Keep it equal to the MaxLength marker on LastRemediationStatus.Machine.
const maxReportedMachineName = 253

// remediationData is what the in progress marker carries, so the replacement machine can record
// which machine it replaced and how far into a retry sequence the control plane is.
type remediationData struct {
	Machine    string      `json:"machine"`
	Timestamp  metav1.Time `json:"timestamp"`
	RetryCount int32       `json:"retryCount"`
}

// A string, a time and an int32 cannot fail to marshal, so the error is dropped rather than
// carried through callers that could do nothing useful with it.
func (r remediationData) marshal() string {
	value, _ := json.Marshal(r)

	return string(value)
}

// remediationDataFrom reads the payload an annotation carries. Anything unreadable is treated as
// absent, which is also what the literal "true" older versions wrote becomes.
func remediationDataFrom(objAnnotations map[string]string, key string) (remediationData, bool) {
	value, ok := objAnnotations[key]
	if !ok {
		return remediationData{}, false
	}

	var data remediationData
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return remediationData{}, false
	}

	// The bounds status declares, since anyone can write this annotation and a value status
	// rejects would fail the whole control plane patch on every reconcile from then on.
	if data.Machine == "" || len(data.Machine) > maxReportedMachineName {
		return remediationData{}, false
	}
	if data.Timestamp.IsZero() || data.RetryCount < 0 || data.RetryCount >= math.MaxInt32 {
		return remediationData{}, false
	}

	return data, true
}

// remediationInProgressFor describes the remediation about to start. A machine that already carries
// a recent sequence continues it, so a replacement failing again reads as a retry and not a first try.
func remediationInProgressFor(machineToBeRemediated *clusterv1.Machine) remediationData {
	data := remediationData{Machine: machineToBeRemediated.Name, Timestamp: metav1.Now()}

	last, ok := remediationDataFrom(machineToBeRemediated.Annotations, cpv1beta2.RemediationForAnnotation)
	if ok && last.Timestamp.Add(minHealthyPeriod).After(data.Timestamp.Time) {
		data.RetryCount = last.RetryCount + 1
	}

	return data
}

func isHealthy(machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	return conditions.IsTrue(machine, clusterv1.MachineHealthCheckSucceededCondition)
}

func hasNode(machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	return machine.Status.NodeRef.Name != ""
}

func isProvisioningHealthyMachine(healthyMachines collections.Machines) bool {
	return len(healthyMachines.Filter(collections.Not(hasNode))) > 0
}

func (c *K0sController) sanitizeHealthyMachines(ctx context.Context, healthyMachines collections.Machines) error {
	log := ctrl.LoggerFrom(ctx)

	errList := []error{}
	for _, m := range healthyMachines {
		if conditions.IsFalse(m, clusterv1.MachineOwnerRemediatedCondition) && m.DeletionTimestamp.IsZero() {

			conditions.Delete(m, clusterv1.MachineOwnerRemediatedCondition)

			err := c.Status().Patch(ctx, m, client.Merge)
			if err != nil {
				log.Error(err, "Failed to patch control plane Machine to clean machine's unhealthy condition", "Machine", m.Name)
				errList = append(errList, errors.Wrapf(err, "failed to patch control plane Machine %s to clean machine's unhelthy condition", m.Name))
			}
		}
	}
	if len(errList) > 0 {
		return kerrors.NewAggregate(errList)
	}

	return nil
}
