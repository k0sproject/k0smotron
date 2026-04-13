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

package capimultiinstancewatchselector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/inttest/util"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	operatorNamespace       = "k0smotron"
	secondaryDeploymentName = "controller-manager-secondary"
	testResourceNamespace   = "default"
	testClusterName         = "watch-selector-capi-test"
	testControlPlaneName    = "watch-selector-capi-test-cp"
	testInfrastructureName  = "watch-selector-capi-test"
	testResourceLabelKey    = "instance"
	primaryInstanceLabel    = "primary"
	secondaryInstanceLabel  = "secondary"
	controlPlaneEnableArg   = "--enable-controller=control-plane"
	controlPlaneMetricLabel = `controller="k0smotroncontrolplane"`
	reconcileMetricName     = "controller_runtime_reconcile_total"
)

type CAPIMultiInstanceWatchSelectorSuite struct {
	suite.Suite
	client           *kubernetes.Clientset
	restConfig       *rest.Config
	ctx              context.Context
	clusterYAMLPath  string
	sourceDeployment string
}

func TestCAPIMultiInstanceWatchSelectorSuite(t *testing.T) {
	suite.Run(t, &CAPIMultiInstanceWatchSelectorSuite{})
}

func (s *CAPIMultiInstanceWatchSelectorSuite) SetupSuite() {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	s.Require().NotEmpty(kubeConfigPath, "KUBECONFIG env var must be set and point to kind cluster")

	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	s.Require().NoError(err)
	s.restConfig = restCfg

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	s.Require().NoError(err)
	s.client = kubeClient

	s.ctx, _ = util.NewSuiteContext(s.T())

	tmpDir := s.T().TempDir()
	s.clusterYAMLPath = tmpDir + "/watch-selector-cluster.yaml"
	s.Require().NoError(os.WriteFile(s.clusterYAMLPath, []byte(clusterYAML), 0o644))

	s.sourceDeployment, err = s.findControlPlaneDeploymentName(s.ctx)
	s.Require().NoError(err)
}

func (s *CAPIMultiInstanceWatchSelectorSuite) TestWatchLabelSelectorIsolatesMultipleCAPIInstances() {
	s.applyClusterObjects()
	defer s.cleanupResources()

	s.T().Log("deploying a second CAPI control-plane controller instance in the same namespace without a watch selector")
	s.Require().NoError(s.applySecondaryControllerDeployment(s.ctx, nil))
	s.Require().NoError(util.WaitForDeployment(s.ctx, s.client, secondaryDeploymentName, operatorNamespace))

	shadowPodName, err := s.getDeploymentPodName(s.ctx, secondaryDeploymentName, operatorNamespace)
	s.Require().NoError(err)

	initialCounter, err := s.getK0smotronControlPlaneReconcileCounter(s.ctx, operatorNamespace, shadowPodName)
	s.Require().NoError(err)

	s.T().Log("triggering a reconcile on the primary-labeled K0smotronControlPlane")
	s.Require().NoError(s.annotateControlPlane("reconcile-probe", "without-selector"))

	var counterAfterUnscopedPatch float64
	err = wait.PollUntilContextTimeout(s.ctx, 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		counterAfterUnscopedPatch, err = s.getK0smotronControlPlaneReconcileCounter(ctx, operatorNamespace, shadowPodName)
		if err != nil {
			return false, err
		}
		return counterAfterUnscopedPatch > initialCounter, nil
	})
	s.Require().NoError(err, "the secondary control-plane controller should reconcile the primary-labeled object without --watch-label-selector")

	s.T().Log("rolling the second controller with --watch-label-selector=instance=secondary")
	s.Require().NoError(s.applySecondaryControllerDeployment(s.ctx, ptrTo("--watch-label-selector="+testResourceLabelKey+"="+secondaryInstanceLabel)))
	s.Require().NoError(util.WaitForRolloutCompleted(s.ctx, s.client, secondaryDeploymentName, operatorNamespace))

	shadowPodName, err = s.getDeploymentPodName(s.ctx, secondaryDeploymentName, operatorNamespace)
	s.Require().NoError(err)

	counterBeforeScopedPatch, err := s.getK0smotronControlPlaneReconcileCounter(s.ctx, operatorNamespace, shadowPodName)
	s.Require().NoError(err)

	s.T().Log("triggering the same reconcile after scoping the secondary controller away from the primary-labeled object")
	s.Require().NoError(s.annotateControlPlane("reconcile-probe", "with-selector"))

	err = wait.PollUntilContextTimeout(s.ctx, 2*time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		counterAfterScopedPatch, err := s.getK0smotronControlPlaneReconcileCounter(ctx, operatorNamespace, shadowPodName)
		if err != nil {
			return false, err
		}
		if counterAfterScopedPatch != counterBeforeScopedPatch {
			return false, fmt.Errorf("secondary controller reconcile counter changed from %.0f to %.0f after enabling watch selector", counterBeforeScopedPatch, counterAfterScopedPatch)
		}
		return false, nil
	})
	s.ErrorContains(err, "context deadline exceeded", "the secondary controller should stay idle for primary-labeled resources once scoped")
}

func (s *CAPIMultiInstanceWatchSelectorSuite) findControlPlaneDeploymentName(ctx context.Context) (string, error) {
	deployments, err := s.client.AppsV1().Deployments(operatorNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	if len(deployments.Items) == 0 {
		return "", fmt.Errorf("no deployments found in operator namespace %q", operatorNamespace)
	}

	for _, deployment := range deployments.Items {
		if slices.Contains(deployment.Spec.Template.Spec.Containers[0].Args, controlPlaneEnableArg) {
			return deployment.Name, nil
		}
	}

	return deployments.Items[0].Name, nil
}

func (s *CAPIMultiInstanceWatchSelectorSuite) applyClusterObjects() {
	out, err := exec.Command("kubectl", "apply", "-f", s.clusterYAMLPath).CombinedOutput()
	s.Require().NoError(err, "failed to apply cluster objects: %s", string(out))
}

func (s *CAPIMultiInstanceWatchSelectorSuite) cleanupResources() {
	keep := os.Getenv("KEEP_AFTER_TEST")
	if keep == "true" {
		return
	}
	if keep == "on-failure" && s.T().Failed() {
		return
	}

	_, _ = exec.Command("kubectl", "delete", "-f", s.clusterYAMLPath).CombinedOutput()
	_, _ = exec.Command("kubectl", "delete", "deployment", secondaryDeploymentName, "-n", operatorNamespace).CombinedOutput()
}

func (s *CAPIMultiInstanceWatchSelectorSuite) applySecondaryControllerDeployment(ctx context.Context, watchSelectorArg *string) error {
	original, err := s.client.AppsV1().Deployments(operatorNamespace).Get(ctx, s.sourceDeployment, metav1.GetOptions{})
	if err != nil {
		return err
	}

	shadow := original.DeepCopy()
	shadow.Name = secondaryDeploymentName
	shadow.Namespace = operatorNamespace
	shadow.ResourceVersion = ""
	shadow.UID = ""
	shadow.CreationTimestamp = metav1.Time{}
	shadow.Generation = 0
	shadow.ManagedFields = nil
	shadow.OwnerReferences = nil
	shadow.Status = appsv1.DeploymentStatus{}
	shadow.Spec.Selector.MatchLabels = mergeStringMaps(shadow.Spec.Selector.MatchLabels, map[string]string{
		"watch-selector-instance": secondaryInstanceLabel,
	})
	shadow.Spec.Template.Labels = mergeStringMaps(shadow.Spec.Template.Labels, map[string]string{
		"watch-selector-instance": secondaryInstanceLabel,
	})
	shadow.Spec.Template.Annotations = mergeStringMaps(shadow.Spec.Template.Annotations, map[string]string{
		"k0smotron.io/restarted-at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	shadow.Spec.Template.Spec.Containers[0].Args = rewriteControllerArgs(shadow.Spec.Template.Spec.Containers[0].Args, watchSelectorArg)

	if _, err := s.client.AppsV1().Deployments(operatorNamespace).Get(ctx, secondaryDeploymentName, metav1.GetOptions{}); err == nil {
		current, err := s.client.AppsV1().Deployments(operatorNamespace).Get(ctx, secondaryDeploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current.Spec.Selector.MatchLabels = shadow.Spec.Selector.MatchLabels
		current.Spec.Template.Labels = shadow.Spec.Template.Labels
		current.Spec.Template.Annotations = shadow.Spec.Template.Annotations
		current.Spec.Template.Spec.Containers[0].Args = shadow.Spec.Template.Spec.Containers[0].Args
		_, err = s.client.AppsV1().Deployments(operatorNamespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	_, err = s.client.AppsV1().Deployments(operatorNamespace).Create(ctx, shadow, metav1.CreateOptions{})
	return err
}

func rewriteControllerArgs(args []string, watchSelectorArg *string) []string {
	updated := make([]string, 0, len(args)+2)
	hasInsecureDiagnostics := false
	for _, arg := range args {
		if arg == "--leader-elect" {
			continue
		}
		if strings.HasPrefix(arg, "--watch-label-selector=") {
			continue
		}
		if arg == "--insecure-diagnostics" {
			hasInsecureDiagnostics = true
		}
		updated = append(updated, arg)
	}
	if !hasInsecureDiagnostics {
		updated = append(updated, "--insecure-diagnostics")
	}
	if watchSelectorArg != nil {
		updated = append(updated, *watchSelectorArg)
	}
	return updated
}

func (s *CAPIMultiInstanceWatchSelectorSuite) getDeploymentPodName(ctx context.Context, deploymentName, namespace string) (string, error) {
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	var podName string
	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					podName = pod.Name
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}

	return podName, nil
}

func (s *CAPIMultiInstanceWatchSelectorSuite) annotateControlPlane(key, value string) error {
	out, err := exec.Command("kubectl", "annotate", "--overwrite", "-n", testResourceNamespace, "k0smotroncontrolplane/"+testControlPlaneName, key+"="+value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("annotate K0smotronControlPlane: %s", string(out))
	}
	return nil
}

func (s *CAPIMultiInstanceWatchSelectorSuite) getK0smotronControlPlaneReconcileCounter(ctx context.Context, namespace, podName string) (float64, error) {
	fw, err := util.GetPortForwarderWithPorts(s.restConfig, podName, namespace, 0, 8080)
	if err != nil {
		return 0, fmt.Errorf("get port forwarder: %w", err)
	}
	go fw.Start(s.Require().NoError)
	defer fw.Close()

	<-fw.ReadyChan

	localPort, err := fw.LocalPort()
	if err != nil {
		return 0, fmt.Errorf("get local port: %w", err)
	}

	var body io.ReadCloser
	err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(_ context.Context) (bool, error) {
		body, err = fetchMetricsBody(localPort)
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return 0, fmt.Errorf("fetch metrics: %w", err)
	}
	defer body.Close()

	return parseReconcileCounter(body, controlPlaneMetricLabel)
}

func getFreeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func fetchMetricsBody(localPort int) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/metrics", localPort), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected metrics status code %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func parseReconcileCounter(r io.Reader, controllerMetricLabel string) (float64, error) {
	var total float64

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, reconcileMetricName+"{") {
			continue
		}
		if !strings.Contains(line, controllerMetricLabel) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0, fmt.Errorf("unexpected metrics line %q", line)
		}
		value := 0.0
		_, err := fmt.Sscanf(fields[1], "%f", &value)
		if err != nil {
			return 0, fmt.Errorf("parse metrics line %q: %w", line, err)
		}
		total += value
	}

	return total, scanner.Err()
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, base)
	maps.Copy(out, overlay)
	return out
}

func ptrTo[T any](value T) *T {
	return &value
}

const clusterYAML = `apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: watch-selector-capi-test
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    serviceDomain: cluster.local
    services:
      cidrBlocks:
      - 10.128.0.0/12
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane
    name: watch-selector-capi-test-cp
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerCluster
    name: watch-selector-capi-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerCluster
metadata:
  name: watch-selector-capi-test
  namespace: default
spec: {}
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane
metadata:
  name: watch-selector-capi-test-cp
  namespace: default
  labels:
    instance: primary
spec:
  version: v1.32.6+k0s.0
  persistence:
    type: emptyDir
  service:
    type: NodePort
`
