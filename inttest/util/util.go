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

	"github.com/k0sproject/k0s/inttest/common"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

func InstallK0smotronOperator(ctx context.Context, kc *kubernetes.Clientset, rc *rest.Config) error {
	data, err := os.ReadFile(os.Getenv("K0SMOTRON_INSTALL_YAML"))
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
			return err
		}

		var drClient dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			drClient = client.Resource(mapping.Resource).Namespace(res.GetNamespace())
		} else {
			drClient = client.Resource(mapping.Resource)
		}

		_, err = drClient.Create(ctx, res, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func GetJoinToken(kc *kubernetes.Clientset, rc *rest.Config, name string, namespace string) (string, error) {
	output, err := common.PodExecCmdOutput(kc, rc, name, namespace, "k0s token create --role=worker")
	if err != nil {
		return "", fmt.Errorf("failed to get join token: %s", err)
	}

	return output, nil
}
