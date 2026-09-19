/*
Copyright 2025.

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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	// ControlPlaneAvailableReason documents the fact that the control plane is reachable.
	ControlPlaneAvailableReason = "Available"
	// ControlPlaneNotAvailableReason surfaces when the workload cluster API did
	// not answer.
	ControlPlaneNotAvailableReason = clusterv1.NotAvailableReason
	// ControlPlaneConnectionDownReason surfaces while reads of the workload cluster
	// API are failing but have not failed for long enough to report an outage.
	ControlPlaneConnectionDownReason = clusterv1.ConnectionDownReason
	// ControlPlaneAvailableUnknownReason surfaces while the workload cluster API has
	// not been reached even once, so nothing can be said about its availability.
	ControlPlaneAvailableUnknownReason = clusterv1.AvailableUnknownReason

	// K0sControlPlaneMachineRemediationFailedReason surfaces on a Machine whose
	// remediation failed. A local string, since upstream only has a v1beta1 one.
	K0sControlPlaneMachineRemediationFailedReason = "RemediationFailed"

	// K0sControlPlaneMachineEtcdMemberHealthyCondition reports whether a control plane
	// machine's etcd member is joined. Absent when the cluster keeps no etcd members.
	K0sControlPlaneMachineEtcdMemberHealthyCondition = "EtcdMemberHealthy"

	// K0sControlPlaneMachineEtcdMemberHealthyReason surfaces when the member is joined.
	K0sControlPlaneMachineEtcdMemberHealthyReason = "Healthy"

	// K0sControlPlaneMachineEtcdMemberNotHealthyReason surfaces when the member has left
	// the cluster or k0s could not reconcile it.
	K0sControlPlaneMachineEtcdMemberNotHealthyReason = "NotHealthy"

	// K0sControlPlaneMachineEtcdMemberHealthyUnknownReason surfaces while the member has
	// not been read, covering a machine still joining and a cluster that cannot be reached.
	K0sControlPlaneMachineEtcdMemberHealthyUnknownReason = "NotReported"

	// K0sControlPlaneMachineNoEtcdMembersReason surfaces when the control plane keeps no
	// etcd members, because it uses kine or an external cluster.
	K0sControlPlaneMachineNoEtcdMembersReason = "NoEtcdMembers"

	// K0sControlPlaneScalingUpCondition is true if actual replicas < desired replicas.
	// Note: In case a K0sControlPlane preflight check is preventing scale up, this will surface in the condition message.
	K0sControlPlaneScalingUpCondition = clusterv1.ScalingUpCondition

	// K0sControlPlaneScalingUpReason surfaces when actual replicas < desired replicas.
	K0sControlPlaneScalingUpReason = clusterv1.ScalingUpReason

	// K0sControlPlaneNotScalingUpReason surfaces when actual replicas >= desired replicas.
	K0sControlPlaneNotScalingUpReason = clusterv1.NotScalingUpReason

	// K0sControlPlaneScalingDownCondition is true if actual replicas > desired replicas.
	// Note: In case a K0sControlPlane preflight check is preventing scale down, this will surface in the condition message.
	K0sControlPlaneScalingDownCondition = clusterv1.ScalingDownCondition

	// K0sControlPlaneScalingDownReason surfaces when actual replicas > desired replicas.
	K0sControlPlaneScalingDownReason = clusterv1.ScalingDownReason

	// K0sControlPlaneNotScalingDownReason surfaces when actual replicas <= desired replicas.
	K0sControlPlaneNotScalingDownReason = clusterv1.NotScalingDownReason
)
