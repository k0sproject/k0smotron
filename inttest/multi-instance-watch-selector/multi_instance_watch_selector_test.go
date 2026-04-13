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

package multiinstancewatchselector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0s/inttest/common"
	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/k0smotron/inttest/util"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	operatorNamespace          = "k0smotron"
	secondaryOperatorNamespace = "k0smotron-secondary"
	operatorDeploymentName     = "k0smotron-controller-manager"
	operatorServiceAccountName = "controller-manager"
	webhookCertSecretName      = "k0smotron-webhook-server-cert"
	testClusterNamespace       = "kmc-watch-selector-test"
	testClusterName            = "kmc-watch-selector-test"
	testResourceLabelKey       = "instance"
	primaryInstanceLabel       = "primary"
	secondaryInstanceLabel     = "secondary"
	reconcileMetricName        = "controller_runtime_reconcile_total"
	clusterControllerMetric    = `controller="cluster"`
)

type MultiInstanceWatchSelectorSuite struct {
	common.FootlooseSuite
}

func TestMultiInstanceWatchSelectorSuite(t *testing.T) {
	s := MultiInstanceWatchSelectorSuite{
		FootlooseSuite: common.FootlooseSuite{
			ControllerCount:                 1,
			WorkerCount:                     1,
			K0smotronWorkerCount:            0,
			K0smotronImageBundleMountPoints: []string{"/dist/bundle.tar"},
		},
	}
	suite.Run(t, &s)
}

func (s *MultiInstanceWatchSelectorSuite) TestWatchLabelSelectorIsolatesMultipleStandaloneInstances() {
	s.T().Log("starting k0s")
	s.Require().NoError(s.InitController(0, "--disable-components=metrics-server"))
	s.Require().NoError(s.RunWorkers())

	kc, err := s.KubeClient(s.ControllerNode(0))
	s.Require().NoError(err)
	rc, err := s.GetKubeConfig(s.ControllerNode(0))
	s.Require().NoError(err)

	s.Require().NoError(s.WaitForNodeReady(s.WorkerNode(0), kc))
	s.Require().NoError(s.ImportK0smotronImages(s.Context()))

	s.T().Log("deploying k0smotron operator")
	s.Require().NoError(util.InstallK0smotronOperator(s.Context(), kc, rc))
	s.Require().NoError(common.WaitForDeployment(s.Context(), kc, operatorDeploymentName, operatorNamespace))

	s.T().Log("creating a labeled k0smotron cluster")
	s.createLabeledK0smotronCluster(s.Context(), kc)
	s.Require().NoError(common.WaitForStatefulSet(s.Context(), kc, "kmc-"+testClusterName, testClusterNamespace))

	s.T().Log("deploying a second controller instance in a different namespace without a watch selector")
	s.Require().NoError(s.applySecondaryControllerDeployment(s.Context(), kc, nil))
	s.Require().NoError(util.WaitForDeployment(s.Context(), kc, operatorDeploymentName, secondaryOperatorNamespace))

	shadowPodName, err := s.getDeploymentPodName(s.Context(), kc, operatorDeploymentName, secondaryOperatorNamespace)
	s.Require().NoError(err)

	initialCounter, err := s.getClusterReconcileCounter(s.Context(), rc, secondaryOperatorNamespace, shadowPodName)
	s.Require().NoError(err)

	s.T().Log("triggering a reconcile on the primary-labeled cluster")
	s.Require().NoError(s.patchClusterAnnotation(s.Context(), kc, "reconcile-probe", "without-selector"))

	var counterAfterUnscopedPatch float64
	err = wait.PollImmediateUntilWithContext(s.Context(), 2*time.Second, func(ctx context.Context) (bool, error) {
		counterAfterUnscopedPatch, err = s.getClusterReconcileCounter(ctx, rc, secondaryOperatorNamespace, shadowPodName)
		if err != nil {
			return false, err
		}
		return counterAfterUnscopedPatch > initialCounter, nil
	})
	s.Require().NoError(err, "the shadow controller should reconcile the primary-labeled cluster without --watch-label-selector")

	s.T().Log("rolling the second namespace controller with --watch-label-selector=instance=secondary")
	s.Require().NoError(s.applySecondaryControllerDeployment(s.Context(), kc, ptrTo("--watch-label-selector="+testResourceLabelKey+"="+secondaryInstanceLabel)))
	s.Require().NoError(util.WaitForRolloutCompleted(s.Context(), kc, operatorDeploymentName, secondaryOperatorNamespace))

	shadowPodName, err = s.getDeploymentPodName(s.Context(), kc, operatorDeploymentName, secondaryOperatorNamespace)
	s.Require().NoError(err)

	counterBeforeScopedPatch, err := s.getClusterReconcileCounter(s.Context(), rc, secondaryOperatorNamespace, shadowPodName)
	s.Require().NoError(err)

	s.T().Log("triggering the same reconcile after scoping the shadow controller away from the primary cluster")
	s.Require().NoError(s.patchClusterAnnotation(s.Context(), kc, "reconcile-probe", "with-selector"))

	err = wait.PollImmediateUntilWithContext(s.Context(), 2*time.Second, func(ctx context.Context) (bool, error) {
		counterAfterScopedPatch, err := s.getClusterReconcileCounter(ctx, rc, secondaryOperatorNamespace, shadowPodName)
		if err != nil {
			return false, err
		}
		if counterAfterScopedPatch != counterBeforeScopedPatch {
			return false, fmt.Errorf("shadow controller reconcile counter changed from %.0f to %.0f after enabling watch selector", counterBeforeScopedPatch, counterAfterScopedPatch)
		}
		return false, nil
	})
	s.ErrorContains(err, "context deadline exceeded", "the shadow controller should stay idle for primary-labeled resources once scoped with --watch-label-selector")
}

func (s *MultiInstanceWatchSelectorSuite) createLabeledK0smotronCluster(ctx context.Context, kc *kubernetes.Clientset) {
	_, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testClusterNamespace,
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)

	cluster := []byte(fmt.Sprintf(`
{
  "apiVersion": "k0smotron.io/v1beta2",
  "kind": "Cluster",
  "metadata": {
    "name": %q,
    "namespace": %q,
    "labels": {
      %q: %q
    }
  },
  "spec": {
    "replicas": 1,
    "version": "v1.31.5-k0s.0",
    "service": {
      "type": "NodePort"
    },
    "k0sConfig": {
      "apiVersion": "k0s.k0sproject.io/v1beta1",
      "kind": "ClusterConfig",
      "spec": {
        "telemetry": {
          "enabled": false
        }
      }
    }
  }
}
`, testClusterName, testClusterNamespace, testResourceLabelKey, primaryInstanceLabel))

	res := kc.RESTClient().
		Post().
		AbsPath("/apis/k0smotron.io/v1beta2/namespaces/" + testClusterNamespace + "/clusters").
		Body(cluster).
		Do(ctx)
	s.Require().NoError(res.Error())
}

func (s *MultiInstanceWatchSelectorSuite) applySecondaryControllerDeployment(ctx context.Context, kc *kubernetes.Clientset, watchSelectorArg *string) error {
	if err := s.ensureSecondaryControllerResources(ctx, kc); err != nil {
		return err
	}

	original, err := kc.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	shadow := original.DeepCopy()
	shadow.Namespace = secondaryOperatorNamespace
	shadow.ResourceVersion = ""
	shadow.UID = ""
	shadow.CreationTimestamp = metav1.Time{}
	shadow.Generation = 0
	shadow.ManagedFields = nil
	shadow.OwnerReferences = nil
	shadow.SelfLink = ""
	shadow.Status = appsv1.DeploymentStatus{}
	shadow.Spec.Template.Annotations = mergeStringMaps(shadow.Spec.Template.Annotations, map[string]string{
		"k0smotron.io/restarted-at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	shadow.Spec.Template.Spec.ServiceAccountName = operatorServiceAccountName

	container := &shadow.Spec.Template.Spec.Containers[0]
	container.Args = rewriteManagerArgs(container.Args, watchSelectorArg)

	if _, err := kc.AppsV1().Deployments(secondaryOperatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{}); err == nil {
		current, err := kc.AppsV1().Deployments(secondaryOperatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		current.Spec.Template.Labels = shadow.Spec.Template.Labels
		current.Spec.Template.Annotations = shadow.Spec.Template.Annotations
		current.Spec.Template.Spec.Containers[0].Args = container.Args
		_, err = kc.AppsV1().Deployments(secondaryOperatorNamespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	_, err = kc.AppsV1().Deployments(secondaryOperatorNamespace).Create(ctx, shadow, metav1.CreateOptions{})
	return err
}

func rewriteManagerArgs(args []string, watchSelectorArg *string) []string {
	updated := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if arg == "--leader-elect" {
			continue
		}
		if strings.HasPrefix(arg, "--watch-label-selector=") {
			continue
		}
		updated = append(updated, arg)
	}
	if watchSelectorArg != nil {
		updated = append(updated, *watchSelectorArg)
	}
	return updated
}

func (s *MultiInstanceWatchSelectorSuite) ensureSecondaryControllerResources(ctx context.Context, kc *kubernetes.Clientset) error {
	if _, err := kc.CoreV1().Namespaces().Get(ctx, secondaryOperatorNamespace, metav1.GetOptions{}); err != nil {
		if _, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: secondaryOperatorNamespace,
			},
		}, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	if _, err := kc.CoreV1().ServiceAccounts(secondaryOperatorNamespace).Get(ctx, operatorServiceAccountName, metav1.GetOptions{}); err != nil {
		if _, err := kc.CoreV1().ServiceAccounts(secondaryOperatorNamespace).Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      operatorServiceAccountName,
				Namespace: secondaryOperatorNamespace,
			},
		}, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	if _, err := kc.RbacV1().ClusterRoleBindings().Get(ctx, "k0smotron-secondary-manager-rolebinding", metav1.GetOptions{}); err != nil {
		if _, err := kc.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "k0smotron-secondary-manager-rolebinding",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "k0smotron-manager-role",
			},
			Subjects: []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      operatorServiceAccountName,
				Namespace: secondaryOperatorNamespace,
			}},
		}, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	secret, err := kc.CoreV1().Secrets(operatorNamespace).Get(ctx, webhookCertSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if _, err := kc.CoreV1().Secrets(secondaryOperatorNamespace).Get(ctx, webhookCertSecretName, metav1.GetOptions{}); err != nil {
		secretCopy := secret.DeepCopy()
		secretCopy.Namespace = secondaryOperatorNamespace
		secretCopy.ResourceVersion = ""
		secretCopy.UID = ""
		secretCopy.CreationTimestamp = metav1.Time{}
		secretCopy.ManagedFields = nil
		secretCopy.OwnerReferences = nil
		if _, err := kc.CoreV1().Secrets(secondaryOperatorNamespace).Create(ctx, secretCopy, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (s *MultiInstanceWatchSelectorSuite) getDeploymentPodName(ctx context.Context, kc *kubernetes.Clientset, deploymentName, namespace string) (string, error) {
	deployment, err := kc.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	var podName string
	err = wait.PollImmediateUntilWithContext(ctx, 2*time.Second, func(ctx context.Context) (bool, error) {
		pods, err := kc.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			if pod.Status.Phase != corev1.PodRunning {
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

func (s *MultiInstanceWatchSelectorSuite) patchClusterAnnotation(ctx context.Context, kc *kubernetes.Clientset, key, value string) error {
	patch := fmt.Sprintf(`[{"op":"add","path":"/metadata/annotations","value":{}},{"op":"add","path":"/metadata/annotations/%s","value":%q}]`,
		escapeJSONPointer(key), value)

	cluster := &km.Cluster{}
	err := kc.RESTClient().
		Patch(types.JSONPatchType).
		AbsPath("/apis/k0smotron.io/v1beta2/namespaces/" + testClusterNamespace + "/clusters/" + testClusterName).
		Body([]byte(patch)).
		Do(ctx).
		Into(cluster)
	if err == nil {
		return nil
	}

	mergePatch := fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q}}}`, key, value)
	return kc.RESTClient().
		Patch(types.MergePatchType).
		AbsPath("/apis/k0smotron.io/v1beta2/namespaces/" + testClusterNamespace + "/clusters/" + testClusterName).
		Body([]byte(mergePatch)).
		Do(ctx).
		Error()
}

func (s *MultiInstanceWatchSelectorSuite) getClusterReconcileCounter(ctx context.Context, rc *rest.Config, namespace, podName string) (float64, error) {
	fw, err := util.GetPortForwarder(rc, podName, namespace, 8080)
	if err != nil {
		return 0, err
	}
	defer fw.Close()

	errCh := make(chan error, 1)
	go fw.Start(func(err error, _ ...any) {
		errCh <- err
	})

	select {
	case <-fw.ReadyChan:
	case err := <-errCh:
		return 0, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	localPort, err := fw.LocalPort()
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/metrics", localPort), nil)
	if err != nil {
		return 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return parseClusterReconcileCounter(resp.Body)
}

func parseClusterReconcileCounter(r io.Reader) (float64, error) {
	var total float64

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, reconcileMetricName+"{") {
			continue
		}
		if !strings.Contains(line, clusterControllerMetric) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0, fmt.Errorf("unexpected metrics line: %q", line)
		}

		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse reconcile metric value from %q: %w", line, err)
		}
		total += value
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return total, nil
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, base)
	maps.Copy(out, overlay)
	return out
}

func escapeJSONPointer(input string) string {
	input = strings.ReplaceAll(input, "~", "~0")
	return strings.ReplaceAll(input, "/", "~1")
}

func ptrTo[T any](value T) *T {
	return &value
}
