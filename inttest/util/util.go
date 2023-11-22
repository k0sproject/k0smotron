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

package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/k0sproject/k0smotron/internal/exec"
)

func InstallK0smotronOperator(ctx context.Context, kc *kubernetes.Clientset, rc *rest.Config) error {
	return CreateFromYAML(ctx, kc, rc, os.Getenv("K0SMOTRON_INSTALL_YAML"))
}

func CreateFromYAML(ctx context.Context, kc *kubernetes.Clientset, rc *rest.Config, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	resources, err := ParseManifests(data)
	if err != nil {
		return err
	}

	dc, err := GetDynamicClient(rc)
	if err != nil {
		return err
	}

	return CreateResources(ctx, resources, kc, dc)
}

func ParseManifests(data []byte) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var resource map[string]interface{}
	for {
		err := decoder.Decode(&resource)
		if err == io.EOF {
			return resources, nil
		} else if err != nil {
			return nil, err
		}

		item := &unstructured.Unstructured{
			Object: resource,
		}
		if item.GetAPIVersion() != "" && item.GetKind() != "" {
			resources = append(resources, item)
			resource = nil
		}
	}
}

func GetDynamicClient(rc *rest.Config) (*dynamic.DynamicClient, error) {
	client, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func getMapper(kc *kubernetes.Clientset) *restmapper.DeferredDiscoveryRESTMapper {
	disc := memory.NewMemCacheClient(discovery.NewDiscoveryClient(kc.RESTClient()))
	return restmapper.NewDeferredDiscoveryRESTMapper(disc)
}

func CreateResources(ctx context.Context, resources []*unstructured.Unstructured, kc *kubernetes.Clientset, client *dynamic.DynamicClient) error {
	mapper := getMapper(kc)
	for _, res := range resources {

		mapping, err := mapper.RESTMapping(
			res.GroupVersionKind().GroupKind(),
			res.GroupVersionKind().Version)

		if err != nil {
			return fmt.Errorf("getting mapping error: %w", err)
		}

		var drClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			drClient = client.Resource(mapping.Resource).Namespace(res.GetNamespace())
		} else {
			drClient = client.Resource(mapping.Resource)
		}

		_, err = drClient.Create(ctx, res, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating %s/%s objects error: %w", res.GroupVersionKind(), res.GetName(), err)
		}
	}
	return nil
}

func GetJoinToken(kc *kubernetes.Clientset, rc *rest.Config, name string, namespace string) (string, error) {
	output, err := exec.PodExecCmdOutput(context.TODO(), kc, rc, name, namespace, "k0s token create --role=worker")
	if err != nil {
		return "", fmt.Errorf("failed to get join token: %s", err)
	}

	return output, nil
}

// GetKMCClientSet returns a kubernetes clientset for the cluster given
// the name and the namespace of the cluster.k0smotron.io
func GetKMCClientSet(ctx context.Context, kc *kubernetes.Clientset, name string, namespace string, port int) (*kubernetes.Clientset, error) {
	secretName := fmt.Sprintf("%s-kubeconfig", name)
	// Wait first to see the secret exists
	if err := WaitForSecret(ctx, kc, secretName, namespace); err != nil {
		return nil, err
	}
	kubeConf, err := kc.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	kmcCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConf.Data["value"]))
	if err != nil {
		return nil, err
	}

	// Override the host to point to the port forwarded API server
	kmcCfg.Host = fmt.Sprintf("localhost:%d", port)

	return kubernetes.NewForConfig(kmcCfg)
}

func GetNodeAddress(ctx context.Context, kc *kubernetes.Clientset, node string) (string, error) {
	n, err := kc.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, addr := range n.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address, nil
		}
	}

	return "", fmt.Errorf("Node doesn't have an Address of type InternalIP")
}

func WaitForSecret(ctx context.Context, kc *kubernetes.Clientset, name string, namespace string) error {
	// Use apimachinery wait directly as the k0s common polls bit too much and sometimes it results into client side throttling
	// Since it's marked deprecated in a wrong way, there's no replacement for it yet, we'll disable the linter for now
	// nolint:staticcheck
	return wait.PollImmediateUntilWithContext(ctx, 1*time.Second, func(ctx context.Context) (bool, error) {
		secret, err := kc.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})

		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if err != nil && apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if secret.Data["value"] != nil && len(secret.Data["value"]) > 0 {
			return true, nil
		}
		return false, nil
	})
}
