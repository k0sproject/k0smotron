/*
Copyright 2026.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
)

const (
	etcdMembersAPIPath = "/apis/etcd.k0sproject.io/v1beta1/etcdmembers"

	etcdMemberReconcileStatusFailed = "Failed"

	kineStorageType = "kine"
)

// The pinned k0s module carries no etcd API package, so the fields read here are declared
// rather than imported. Only what member health needs is included.
type etcdMemberList struct {
	Items []etcdMember `json:"items"`
}

type etcdMember struct {
	metav1.ObjectMeta `json:"metadata"`

	Status etcdMemberStatus `json:"status"`
}

type etcdMemberStatus struct {
	ReconcileStatus string                `json:"reconcileStatus,omitempty"`
	Conditions      []etcdMemberCondition `json:"conditions,omitempty"`
}

type etcdMemberCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// etcdManaged reports whether k0s keeps etcd members for this control plane. Kine replaces
// etcd outright, and an external cluster is not k0s's to track, so neither has members.
func etcdManaged(kcp *cpv1beta2.K0sControlPlane) (bool, error) {
	if kcp.Spec.K0sConfigSpec.K0s == nil {
		return true, nil
	}
	k0s := kcp.Spec.K0sConfigSpec.K0s.Object

	// k0s fills in a default data source when the type alone says kine, so the type has
	// to be read as well as the data source.
	storageType, _, err := unstructured.NestedString(k0s, "spec", "storage", "type")
	if err != nil {
		return false, err
	}
	if storageType == kineStorageType {
		return false, nil
	}

	kine, _, err := unstructured.NestedString(k0s, "spec", "storage", "kine", "dataSource")
	if err != nil {
		return false, err
	}
	if kine != "" {
		return false, nil
	}

	external, _, err := unstructured.NestedMap(k0s, "spec", "storage", "etcd", "externalCluster")
	if err != nil {
		return false, err
	}

	return len(external) == 0, nil
}

// etcdMemberHealth reports each machine's etcd member state, keyed by machine name. A
// machine is left out when its state is unknown, so an unreachable cluster reports nothing.
func (c *K0sController) etcdMemberHealth(ctx context.Context, cluster *clusterv1.Cluster, machines collections.Machines) map[string]metav1.ConditionStatus {
	logger := log.FromContext(ctx)

	clientset, err := c.getWorkloadClusterClientset(ctx, cluster)
	if err != nil {
		logger.V(1).Info("Cannot reach the workload cluster to read etcd members", "reason", err.Error())

		return nil
	}

	// One list rather than a request per machine. The control plane bounds the result, so
	// it does not need the paging a node list does.
	raw, err := clientset.RESTClient().Get().AbsPath(etcdMembersAPIPath).Do(ctx).Raw()
	if err != nil {
		logger.V(1).Info("Cannot list etcd members", "reason", err.Error())

		return nil
	}

	var list etcdMemberList
	if err := json.Unmarshal(raw, &list); err != nil {
		logger.Error(err, "Cannot decode the etcd member list")

		return nil
	}

	members := make(map[string]etcdMember, len(list.Items))
	for _, member := range list.Items {
		members[member.Name] = member
	}

	health := make(map[string]metav1.ConditionStatus, len(machines))
	for _, machine := range machines {
		member, ok := members[machine.Name]
		if !ok && machine.Status.NodeRef.IsDefined() {
			// Only k0s v1.31.1 and above names the member after the machine, older
			// versions name it after the node.
			member, ok = members[machine.Status.NodeRef.Name]
		}
		// A machine that is still joining has no member yet, which is not a failure, so
		// it stays out of the map rather than being reported either way.
		if !ok {
			continue
		}

		if etcdMemberUnhealthy(member) {
			health[machine.Name] = metav1.ConditionFalse
		} else {
			health[machine.Name] = metav1.ConditionTrue
		}
	}

	return health
}

// etcdMemberUnhealthy reports a member that has left or that k0s failed to reconcile. An
// absent or Unknown condition is a transient state rather than a failure.
func etcdMemberUnhealthy(member etcdMember) bool {
	if member.Status.ReconcileStatus == etcdMemberReconcileStatusFailed {
		return true
	}

	for _, condition := range member.Status.Conditions {
		if condition.Type == etcdMemberConditionTypeJoined {
			return condition.Status == string(metav1.ConditionFalse)
		}
	}

	return false
}
