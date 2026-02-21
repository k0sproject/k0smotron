/*
Copyright 2023.

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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/imdario/mergo"
	"github.com/k0sproject/version"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/collections"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
)

const (
	etcdMemberConditionTypeJoined = "Joined"
)

var (
	errMachineWithoutK0sConfigAnnotation = fmt.Errorf("k0s config annotation not found on machine")
)

func (c *K0sController) createMachine(ctx context.Context, name string, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane, infraRef clusterv1.ContractVersionedObjectReference, failureDomain string) (*clusterv1.Machine, error) {
	machine, err := c.generateMachine(ctx, name, cluster, kcp, infraRef, failureDomain)
	if err != nil {
		return nil, fmt.Errorf("error generating machine: %w", err)
	}
	_ = ctrl.SetControllerReference(kcp, machine, c.Client.Scheme())

	err = c.Client.Patch(ctx, machine, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	})
	if err != nil {
		return machine, err
	}

	// Remove the annotation tracking that a remediation is in progress.
	// A remediation is completed when the replacement machine has been created above.
	delete(kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation)

	return machine, nil
}

func (c *K0sController) deleteMachine(ctx context.Context, name string, kcp *cpv1beta2.K0sControlPlane) error {
	machine := &clusterv1.Machine{

		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kcp.Namespace,
		},
	}

	err := c.Client.Delete(ctx, machine)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("error deleting machine: %w", err)
	}
	return nil
}

func (c *K0sController) generateMachine(_ context.Context, name string, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane, infraRef clusterv1.ContractVersionedObjectReference, failureDomain string) (*clusterv1.Machine, error) {
	v := kcp.K0sVersion()

	labels := controlPlaneCommonLabelsForCluster(kcp, cluster.Name)

	for _, arg := range kcp.Spec.K0sConfigSpec.Args {
		if arg == "--enable-worker" || arg == "--enable-worker=true" {
			labels["k0smotron.io/control-plane-worker-enabled"] = "true"
			break
		}
	}

	annotations := map[string]string{
		cpv1beta2.K0ControlPlanePreTerminateHookCleanupAnnotation: "",
	}
	// Add the annotations from the MachineTemplate.
	// Note: we intentionally don't use the map directly to ensure we don't modify the map in KCP.
	for k, v := range kcp.Spec.MachineTemplate.ObjectMeta.Annotations {
		annotations[k] = v
	}

	k0sConfigAnnotationValue, err := generateK0sConfigAnnotationValueForMachine(kcp, name)
	if err != nil {
		return nil, err
	}
	annotations[cpv1beta2.MachineK0sConfigAnnotation] = k0sConfigAnnotationValue

	machine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   kcp.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: clusterv1.MachineSpec{
			Version:       v,
			ClusterName:   cluster.Name,
			FailureDomain: failureDomain,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Kind:     "K0sControllerConfig",
					Name:     name,
					APIGroup: bootstrapv2.GroupVersion.Group,
				},
			},
			InfrastructureRef: infraRef,
		},
	}

	return machine, nil
}

func generateK0sConfigAnnotationValueForMachine(kcp *cpv1beta2.K0sControlPlane, machineName string) (string, error) {
	// We make a copy of the K0sControlPlane to avoid modifying the original object with a value that is specific to a machine.
	kcpCopy := kcp.DeepCopy()

	currentKCPVersion, err := version.NewVersion(kcpCopy.K0sVersion())
	if err != nil {
		return "", fmt.Errorf("error parsing k0s version: %w", err)
	}

	if currentKCPVersion.GreaterThanOrEqual(minVersionForETCDName) {
		if kcpCopy.Spec.K0sConfigSpec.K0s == nil {
			kcpCopy.Spec.K0sConfigSpec.K0s = &unstructured.Unstructured{
				Object: make(map[string]interface{}),
			}
		}
		// If it is not explicitly indicated to use Kine storage, we use the machine name to name the ETCD member.
		kineStorage, found, err := unstructured.NestedString(kcpCopy.Spec.K0sConfigSpec.K0s.Object, "spec", "storage", "kine", "dataSource")
		if err != nil {
			return "", fmt.Errorf("error retrieving storage.kine.datasource: %w", err)
		}
		if !found || kineStorage == "" {
			err = unstructured.SetNestedMap(kcpCopy.Spec.K0sConfigSpec.K0s.Object, map[string]interface{}{}, "spec", "storage", "etcd", "extraArgs")
			if err != nil {
				return "", fmt.Errorf("error ensuring intermediate maps spec.storage.etcd.extraArgs: %w", err)
			}
			err = unstructured.SetNestedField(kcpCopy.Spec.K0sConfigSpec.K0s.Object, machineName, "spec", "storage", "etcd", "extraArgs", "name")
			if err != nil {
				return "", fmt.Errorf("error setting storage.etcd.extraArgs.name: %w", err)
			}
		}
	}

	k0sConfigSpec, err := json.Marshal(kcpCopy.Spec.K0sConfigSpec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal K0sConfigSpec: %w", err)
	}
	return string(k0sConfigSpec), nil
}

func (c *K0sController) getInfraMachines(ctx context.Context, machines collections.Machines) (map[string]*unstructured.Unstructured, error) {
	result := map[string]*unstructured.Unstructured{}
	for _, m := range machines {
		infraMachine, err := external.GetObjectFromContractVersionedRef(ctx, c.Client, m.Spec.InfrastructureRef, m.Namespace)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("failed to retrieve infra machine for machine object %s: %w", m.Name, err)
		}
		result[m.Name] = infraMachine
	}
	return result, nil
}

func (c *K0sController) getBootstrapConfigs(ctx context.Context, machines collections.Machines) (map[string]bootstrapv2.K0sControllerConfig, error) {
	result := map[string]bootstrapv2.K0sControllerConfig{}
	for _, m := range machines {
		var b bootstrapv2.K0sControllerConfig
		err := c.Client.Get(ctx, client.ObjectKey{Namespace: m.Namespace, Name: m.Name}, &b)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("failed to retrieve bootstrap data for machine object %s: %w", m.Name, err)
		}
		result[m.Name] = b
	}
	return result, nil
}

func (c *K0sController) createMachineFromTemplate(ctx context.Context, name string, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane) (*unstructured.Unstructured, error) {
	infraMachine, err := c.generateMachineFromTemplate(ctx, name, cluster, kcp)
	if err != nil {
		return nil, err
	}

	existingInfraMachine := &unstructured.Unstructured{}
	existingInfraMachine.SetAPIVersion(infraMachine.GetAPIVersion())
	existingInfraMachine.SetKind(infraMachine.GetKind())
	err = c.Get(ctx, client.ObjectKey{Namespace: infraMachine.GetNamespace(), Name: infraMachine.GetName()}, existingInfraMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err = c.Client.Patch(ctx, infraMachine, client.Apply, &client.PatchOptions{
				FieldManager: "k0smotron",
			}); err != nil {
				return nil, fmt.Errorf("error apply patching: %w", err)
			}
			return infraMachine, nil
		}

		return nil, fmt.Errorf("error getting machine implementation: %w", err)
	}

	err = mergo.Merge(existingInfraMachine, infraMachine, mergo.WithSliceDeepCopy)
	if err != nil {
		return nil, err
	}

	spec, _, _ := unstructured.NestedMap(existingInfraMachine.Object, "spec")
	patch := unstructured.Unstructured{Object: map[string]interface{}{
		"spec": spec,
	}}
	data, err := patch.MarshalJSON()
	if err != nil {
		return nil, err
	}

	pluralName := ""
	resList, _ := c.ClientSet.Discovery().ServerResourcesForGroupVersion(existingInfraMachine.GetAPIVersion())
	for _, apiRes := range resList.APIResources {
		if apiRes.Kind == existingInfraMachine.GetKind() && !strings.Contains(apiRes.Name, "/") {
			pluralName = apiRes.Name
			break
		}
	}
	req := c.ClientSet.RESTClient().Patch(types.MergePatchType).
		Body(data).
		AbsPath("apis", infraMachine.GetAPIVersion(), "namespaces", infraMachine.GetNamespace(), pluralName, infraMachine.GetName())
	_, err = req.DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("error patching: %w", err)
	}
	return infraMachine, nil
}

func (c *K0sController) generateMachineFromTemplate(ctx context.Context, name string, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane) (*unstructured.Unstructured, error) {
	infraMachineTemplate, err := c.getMachineTemplate(ctx, kcp)
	if err != nil {
		return nil, err
	}

	_ = ctrl.SetControllerReference(cluster, infraMachineTemplate, c.Client.Scheme())
	err = c.Client.Patch(ctx, infraMachineTemplate, client.Merge, &client.PatchOptions{FieldManager: "k0smotron"})
	if err != nil {
		return nil, err
	}

	template, found, err := unstructured.NestedMap(infraMachineTemplate.UnstructuredContent(), "spec", "template")
	if !found {
		return nil, fmt.Errorf("missing spec.template on %v %q", infraMachineTemplate.GroupVersionKind(), infraMachineTemplate.GetName())
	} else if err != nil {
		return nil, fmt.Errorf("error getting spec.template map on %v %q: %w", infraMachineTemplate.GroupVersionKind(), infraMachineTemplate.GetName(), err)
	}

	infraMachine := &unstructured.Unstructured{Object: template}
	infraMachine.SetName(name)
	infraMachine.SetNamespace(kcp.Namespace)

	annotations := map[string]string{}
	for key, value := range kcp.Annotations {
		annotations[key] = value
	}

	for k, v := range kcp.Spec.MachineTemplate.ObjectMeta.Annotations {
		annotations[k] = v
	}

	annotations[clusterv1.TemplateClonedFromNameAnnotation] = kcp.Spec.MachineTemplate.Spec.InfrastructureRef.Name
	annotations[clusterv1.TemplateClonedFromGroupKindAnnotation] = kcp.Spec.MachineTemplate.Spec.InfrastructureRef.GroupKind().String()
	infraMachine.SetAnnotations(annotations)

	infraMachine.SetLabels(controlPlaneCommonLabelsForCluster(kcp, cluster.GetName()))

	infraMachine.SetAPIVersion(infraMachineTemplate.GetAPIVersion())
	infraMachine.SetKind(strings.TrimSuffix(infraMachineTemplate.GetKind(), clusterv1.TemplateSuffix))

	return infraMachine, nil
}

func hasControllerConfigChanged(bootstrapConfigs map[string]bootstrapv2.K0sControllerConfig, kcp *cpv1beta2.K0sControlPlane, machine *clusterv1.Machine) bool {
	// Skip the check if the K0sControlPlane is not ready
	if kcp.Status.Initialization.ControlPlaneInitialized == nil ||
		!*kcp.Status.Initialization.ControlPlaneInitialized ||
		kcp.Spec.Replicas != ptr.Deref(kcp.Status.Replicas, 0) {
		return false
	}

	if machine == nil {
		return false
	}

	if machine.Status.Phase != string(clusterv1.MachinePhaseRunning) &&
		machine.Status.Phase != string(clusterv1.MachinePhaseProvisioned) &&
		machine.Status.Phase != string(clusterv1.MachinePhaseProvisioning) {
		return false
	}

	bootstrapConfig, found := bootstrapConfigs[machine.Name]
	if !found {
		return false
	}

	// If the machine has the k0s config annotation, use it for comparison instead of manually comparing the K0sConfigSpec.
	// We will fall back to the manual comparison only if the annotation is missing or invalid. This is required to support
	// the scenario where the machine was created using old k0smotron versions where the k0s config was a mutable resource.
	machineK0sConfig, err := getMachineK0sConfig(machine)
	if err != nil {
		// TODO: Remove this fallback logic in a future release.
		if errors.Is(err, errMachineWithoutK0sConfigAnnotation) {
			return deprecatedIsK0sConfigChanged(&bootstrapConfig, kcp, machine)
		}

		return false
	}

	// IMPORTANT: make a copy of the K0sConfigSpec from the K0sControlPlane, as we will modify it.
	kcpK0sConfig := kcp.Spec.K0sConfigSpec.DeepCopy()
	// ClusterConfig values are reconciled using dynamic config, so leave it out of the comparison
	kcpK0sConfig.K0s = nil
	machineK0sConfig.K0s = nil

	return cmp.Diff(kcpK0sConfig, machineK0sConfig) != ""

}

// Deprecated: This function is kept for backward compatibility with clusters created with versions that does not add an annotation in the
// Machine with the k0s config. Due to its complexity, it is also prone to bugs, so it should not be used in new code and remove its support
// in future releases.
//
// deprecatedIsK0sConfigChanged compares the K0sConfigSpec in the K0sControlPlane with the one in the K0sControllerConfig used to bootstrap
// the Machine.
func deprecatedIsK0sConfigChanged(bootstrapConfig *bootstrapv2.K0sControllerConfig, kcp *cpv1beta2.K0sControlPlane, machine *clusterv1.Machine) bool {
	kcpK0sConfigSpecCopy := kcp.Spec.K0sConfigSpec.DeepCopy()
	bootstrapConfigCopy := bootstrapConfig.DeepCopy()
	kcpK0sConfigSpecCopy.Args = uniqueArgs(kcpK0sConfigSpecCopy.Args)

	removeArgsGeneratedInControllerConfigReconcile(bootstrapConfigCopy)
	bootstrapConfigSpecCopy := bootstrapConfigCopy.Spec.K0sConfigSpec.DeepCopy()

	// k0s config will be reconciled using dynamic config, so leave it out of the comparison
	bootstrapAPIConfig, _, _ := unstructured.NestedMap(bootstrapConfigSpecCopy.K0s.Object, "spec", "api")
	kcpAPIConfig, _, _ := unstructured.NestedMap(kcpK0sConfigSpecCopy.K0s.Object, "spec", "api")
	bootstrapStorageConfig, _, _ := unstructured.NestedMap(bootstrapConfigSpecCopy.K0s.Object, "spec", "storage")
	kcpStorageConfig, _, _ := unstructured.NestedMap(kcpK0sConfigSpecCopy.K0s.Object, "spec", "storage")

	// Handle nil cases consistently - convert nil to empty map for comparison
	if bootstrapStorageConfig == nil {
		bootstrapStorageConfig = make(map[string]interface{})
	}
	if kcpStorageConfig == nil {
		kcpStorageConfig = make(map[string]interface{})
	}

	// Bootstrap controller did set etcd name to the K0sControllerConfig, so we need to compare it with the name set in the K0sControlPlane
	kcpStorageConfigEtcdWithName, _, _ := unstructured.NestedMap(kcpK0sConfigSpecCopy.K0s.Object, "spec", "storage")
	if kcpStorageConfigEtcdWithName == nil {
		kcpStorageConfigEtcdWithName = make(map[string]interface{})
	}
	_ = unstructured.SetNestedField(kcpStorageConfigEtcdWithName, machine.Name, "etcd", "extraArgs", "name")

	bootstrapConfigCopy.Spec.K0sConfigSpec.K0s = kcpK0sConfigSpecCopy.K0s

	// leave out the tunneling spec for the bootstrap config
	bootstrapConfigCopy.Spec.K0sConfigSpec.Tunneling = kcpK0sConfigSpecCopy.Tunneling

	return !reflect.DeepEqual(kcpK0sConfigSpecCopy, bootstrapConfigCopy.Spec.K0sConfigSpec) ||
		!reflect.DeepEqual(kcpAPIConfig, bootstrapAPIConfig) ||
		(!reflect.DeepEqual(kcpStorageConfig, bootstrapStorageConfig) && !reflect.DeepEqual(kcpStorageConfigEtcdWithName, bootstrapStorageConfig))
}

func getMachineK0sConfig(machine *clusterv1.Machine) (*bootstrapv2.K0sConfigSpec, error) {
	k0sConfigAnnotationValue, ok := machine.GetAnnotations()[cpv1beta2.MachineK0sConfigAnnotation]
	if !ok {
		return nil, errMachineWithoutK0sConfigAnnotation
	}

	k0sConfigSpec := &bootstrapv2.K0sConfigSpec{}
	if err := json.Unmarshal([]byte(k0sConfigAnnotationValue), k0sConfigSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal K0sConfigSpec: %w", err)
	}

	return k0sConfigSpec, nil
}

func matchesTemplateClonedFrom(infraMachines map[string]*unstructured.Unstructured, kcp *cpv1beta2.K0sControlPlane, machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	infraMachine, found := infraMachines[machine.Name]
	if !found {
		return false
	}

	clonedFromName := infraMachine.GetAnnotations()[clusterv1.TemplateClonedFromNameAnnotation]
	clonedFromGroupKind := infraMachine.GetAnnotations()[clusterv1.TemplateClonedFromGroupKindAnnotation]

	return clonedFromName == kcp.Spec.MachineTemplate.Spec.InfrastructureRef.Name &&
		clonedFromGroupKind == kcp.Spec.MachineTemplate.Spec.InfrastructureRef.GroupKind().String()
}

func (c *K0sController) checkMachineLeft(ctx context.Context, name string, clientset *kubernetes.Clientset) (bool, error) {
	var etcdMember unstructured.Unstructured
	err := clientset.RESTClient().
		Get().
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Do(ctx).
		Into(&etcdMember)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("error getting etcd member: %w", err)
	}

	conditions, _, err := unstructured.NestedSlice(etcdMember.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("error getting etcd member conditions: %w", err)
	}

	for _, condition := range conditions {
		conditionMap := condition.(map[string]interface{})
		if conditionMap["type"] == etcdMemberConditionTypeJoined && conditionMap["status"] == "False" {
			err = clientset.RESTClient().
				Delete().
				AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
				Do(ctx).
				Into(&etcdMember)
			if err != nil && !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("error deleting etcd member %s: %w", name, err)
			}

			return true, nil
		}
	}
	return false, nil
}

func (c *K0sController) markChildControlNodeToLeave(ctx context.Context, name string, clientset *kubernetes.Clientset) error {
	if clientset == nil {
		return nil
	}

	logger := log.FromContext(ctx).WithValues("controlNode", name)

	err := clientset.RESTClient().
		Patch(types.MergePatchType).
		AbsPath("/apis/etcd.k0sproject.io/v1beta1/etcdmembers/" + name).
		Body([]byte(`{"spec":{"leave":true}, "metadata": {"annotations": {"k0smotron.io/marked-to-leave-at": "` + time.Now().String() + `"}}}`)).
		Do(ctx).
		Error()
	if err != nil {
		logger.Error(err, "error marking etcd member to leave. Trying to mark control node to leave")
		err := clientset.RESTClient().
			Patch(types.MergePatchType).
			AbsPath("/apis/autopilot.k0sproject.io/v1beta2/controlnodes/" + name).
			Body([]byte(`{"metadata":{"annotations":{"k0smotron.io/leave":"true"}}}`)).
			Do(ctx).
			Error()
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("error marking control node to leave: %w", err)
		}
	}
	logger.Info("marked etcd to leave")

	return nil
}

func (c *K0sController) deleteOldControlNodes(ctx context.Context, cluster *clusterv1.Cluster) error {
	kubeClient, err := c.getKubeClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("error getting kube client: %w", err)
	}
	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name))
	if err != nil {
		return fmt.Errorf("error getting all machines: %w", err)
	}

	var controlNodeList unstructured.UnstructuredList
	err = kubeClient.RESTClient().
		Get().
		AbsPath("/apis/autopilot.k0sproject.io/v1beta2/controlnodes").
		Do(ctx).
		Into(&controlNodeList)

	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	existingMachineNames := make(map[string]struct{})
	for _, n := range machines.Names() {
		existingMachineNames[n] = struct{}{}
	}

	for _, controlNode := range controlNodeList.Items {
		if _, ok := existingMachineNames[controlNode.GetName()]; !ok {
			err := c.deleteControlNode(ctx, controlNode.GetName(), kubeClient)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *K0sController) deleteControlNode(ctx context.Context, name string, clientset *kubernetes.Clientset) error {
	if clientset == nil {
		return nil
	}

	err := clientset.RESTClient().
		Delete().
		AbsPath("/apis/autopilot.k0sproject.io/v1beta2/controlnodes/" + name).
		Do(ctx).
		Error()
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (c *K0sController) createAutopilotPlan(ctx context.Context, kcp *cpv1beta2.K0sControlPlane, cluster *clusterv1.Cluster, clientset *kubernetes.Clientset) error {
	if clientset == nil {
		return nil
	}

	k0sVer := kcp.K0sVersion()

	var existingPlan unstructured.Unstructured
	err := clientset.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Into(&existingPlan)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("error getting autopilot plan: %w", err)
	}

	state, found, err := unstructured.NestedString(existingPlan.Object, "status", "state")
	if err != nil {
		return fmt.Errorf("error getting autopilot plan's state: %w", err)
	}
	if found {
		commands, found, err := unstructured.NestedSlice(existingPlan.Object, "spec", "commands")
		if err != nil || !found || len(commands) == 0 {
			return fmt.Errorf("error getting current autopilot plan's commands: %w", err)
		}

		version, found, err := unstructured.NestedString(commands[0].(map[string]interface{}), "k0supdate", "version")
		if err != nil || !found {
			return fmt.Errorf("error getting current autopilot plan's version: %w", err)
		}
		if state == "Schedulable" || state == "SchedulableWait" {
			// it is necessary to check if the current autopilot process corresponds to a previous update by comparing the current
			// version of the resource with the desired one. If that is the case, the state is not yet ready to proceed with a new plan.
			if version != k0sVer {
				return fmt.Errorf("previous autopilot is not finished: %w", ErrNotReady)
			}

			return nil
		}

		if state == "Completed" {
			// If the state is completed, it is necessary to check if the current version of the resource corresponds to the desired one.
			// If that is the case, it is not necessary to proceed with a new plan.
			if version == k0sVer {
				return nil
			}
		}
	}

	err = clientset.RESTClient().Delete().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Error()
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("error deleting autopilot plan: %w", err)
	}

	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
	if err != nil {
		return fmt.Errorf("error getting control plane machines: %w", err)
	}

	amd64DownloadURL := `https://get.k0sproject.io/` + k0sVer + `/k0s-` + k0sVer + `-amd64`
	arm64DownloadURL := `https://get.k0sproject.io/` + k0sVer + `/k0s-` + k0sVer + `-arm64`
	armDownloadURL := `https://get.k0sproject.io/` + k0sVer + `/k0s-` + k0sVer + `-arm`
	if kcp.Spec.K0sConfigSpec.DownloadURL != "" {
		amd64DownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
		arm64DownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
		armDownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	plan := []byte(`
	{
		"apiVersion": "autopilot.k0sproject.io/v1beta2",
		"kind": "Plan",
		"metadata": {
		  "name": "autopilot"
		},
		"spec": {
			"id": "id-` + kcp.Name + `-` + timestamp + `",
			"timestamp": "` + timestamp + `",
			"commands": [{
				"k0supdate": {
					"version": "` + k0sVer + `",
					"platforms": {
						"linux-amd64": {
							"url": "` + amd64DownloadURL + `"
						},
						"linux-arm64": {
							"url": "` + arm64DownloadURL + `"
						},
						"linux-arm": {
							"url": "` + armDownloadURL + `"
						}
					},
					"targets": {
						"controllers": {
							"discovery": {
							    "static": {
									"nodes": ["` + strings.Join(machines.Names(), `","`) + `"]
								}
							}
						}
					}
				}
			}]
		}
	}`)

	return clientset.RESTClient().Post().
		AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans").
		Body(plan).
		Do(ctx).
		Error()
}

func filterControlPlaneFailureDomains(cluster clusterv1.Cluster) []clusterv1.FailureDomain {
	var cpFailureDomains []clusterv1.FailureDomain
	for _, spec := range cluster.Status.FailureDomains {
		if ptr.Deref(spec.ControlPlane, false) {
			cpFailureDomains = append(cpFailureDomains, spec)
		}
	}

	return cpFailureDomains
}

// minVersion returns the minimum version from a list of machines
func minVersion(machines collections.Machines) (string, error) {
	if machines == nil || machines.Len() == 0 {
		return "", nil
	}

	versions := make([]*version.Version, 0, len(machines))
	for _, m := range machines {
		v, err := version.NewVersion(m.Spec.Version)
		if err != nil {
			return "", fmt.Errorf("failed to parse version %s: %w", m.Spec.Version, err)
		}

		versions = append(versions, v)
	}

	sort.Sort(version.Collection(versions))

	return versions[0].String(), nil
}

// removeArgsGeneratedInControllerConfigReconcile removes '--config /etc/k0s.yaml' arg generated by the bootstrap controller
// that should not be included when comparing if the k0s configuration has changed because control plane k0s config will not
// include it.
func removeArgsGeneratedInControllerConfigReconcile(bootstrapConfig *bootstrapv2.K0sControllerConfig) {
	argsWithoutConfig := []string{}
	for _, arg := range bootstrapConfig.Spec.K0sConfigSpec.Args {
		if arg != "--config" && arg != bootstrapConfig.Spec.GetK0sConfigPath() {
			argsWithoutConfig = append(argsWithoutConfig, arg)
		}
	}
	bootstrapConfig.Spec.K0sConfigSpec.Args = argsWithoutConfig
}

func uniqueArgs(args []string) []string {
	uniqueArgsSlice := []string{}
	uniqueArgsMap := make(map[string]struct{})
	for _, arg := range args {
		if _, exists := uniqueArgsMap[arg]; !exists {
			uniqueArgsSlice = append(uniqueArgsSlice, arg)
			uniqueArgsMap[arg] = struct{}{}
		}
	}

	return uniqueArgsSlice
}
