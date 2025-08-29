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
	"fmt"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *K0sController) reconcileUnhealthyMachines(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (retErr error) {
	log := ctrl.LoggerFrom(ctx)

	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name))
	if err != nil {
		return fmt.Errorf("failed to filter machines for control plane: %w", err)
	}

	healthyMachines := machines.Filter(isHealthy)

	// cleanup pending remediation actions not completed if the underlying machine is now back to healthy.
	// machines to be sanitized has the following conditions:
	//
	// HealthCheckSucceeded=True (current machine's state is Health)
	//         AND
	// OwnerRemediated=False (machine was marked as unhealthy previously)
	err = c.sanitizeHealthyMachines(ctx, healthyMachines)
	if err != nil {
		return err
	}
	if _, ok := kcp.Annotations[cpv1beta1.RemediationInProgressAnnotation]; ok {
		log.Info("Another remediation is already in progress. Skipping remediation.")
		return nil
	}

	// retrieve machines marked as unheathy by MHC controller
	unhealthyMachines := machines.Filter(collections.IsUnhealthyAndOwnerRemediated)

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
			log.Error(err, "Failed to patch control plane Machine", "Machine", machineToBeRemediated.Name)
			if retErr == nil {
				retErr = errors.Wrapf(err, "failed to patch control plane Machine %s", machineToBeRemediated.Name)
			}
			return
		}
	}()
	// Ensure that the cluster remains available during and after the remediation process. The remediation must not
	// compromise the cluster's ability to serve workloads or cause disruption to the control plane's functionality.
	if kcp.Status.Ready {
		// The cluster MUST have more than one replica, because this is the smallest cluster size that allows any etcd failure tolerance.
		if !(machines.Len() > 1) {
			log.Info("A control plane machine needs remediation, but the number of current replicas is less or equal to 1. Skipping remediation", "replicas", machines.Len())
			conditions.MarkFalse(machineToBeRemediated, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP can't remediate if current replicas are less or equal to 1")
			return nil
		}

		// The cluster MUST NOT have healthy machines still being provisioned. This rule prevents KCP taking actions while the cluster is in a transitional state.
		if isProvisioningHealthyMachine(healthyMachines) {
			log.Info("A control plane machine needs remediation, but there are other control-plane machines being provisioned. Skipping remediation")
			conditions.MarkFalse(machineToBeRemediated, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP waiting for control plane machine provisioning to complete before triggering remediation")

			return nil
		}

		// The cluster MUST have no machines with a deletion timestamp. This rule prevents KCP taking actions while the cluster is in a transitional state.
		if len(machines.Filter(collections.HasDeletionTimestamp)) > 0 {
			log.Info("A control plane machine needs remediation, but there are other control-plane machines being deleted. Skipping remediation")
			conditions.MarkFalse(machineToBeRemediated, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP waiting for control plane machine deletion to complete before triggering remediation")
			return nil
		}
	}

	// After checks, remediation can be carried out.

	if err := c.runMachineDeletionSequence(ctx, cluster, kcp, machineToBeRemediated); err != nil {
		conditions.MarkFalse(machineToBeRemediated, clusterv1.MachineOwnerRemediatedCondition, clusterv1.RemediationFailedReason, clusterv1.ConditionSeverityError, err.Error())
		return errors.Wrapf(err, "failed to delete unhealthy machine %s", machineToBeRemediated.Name)
	}
	log.Info("Remediated unhealthy machine, another new machine should take its place soon.")

	// Mark controlplane to track that remediation is in progress and do not proceed until machine is gone.
	// This annotation is removed when new controlplane creates a new machine.
	annotations.AddAnnotations(kcp, map[string]string{
		cpv1beta1.RemediationInProgressAnnotation: "true",
	})

	return nil
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
	return machine.Status.NodeRef != nil
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
