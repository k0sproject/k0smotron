//go:build extension

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

package inplaceversionupdate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	autopilot "github.com/k0sproject/k0smotron/v2/internal/autopilot"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createAutopilotPlanForMachine(ctx context.Context, c client.Client, clientset *kubernetes.Clientset, desiredMachine *clusterv1.Machine, isControlPlane bool) error {
	downloadURL, err := getDownloadURLFromBootstrapConfig(ctx, c, desiredMachine.Spec.Bootstrap.ConfigRef, isControlPlane, desiredMachine.Namespace)
	if err != nil {
		return fmt.Errorf("error getting download URL from bootstrap config: %w", err)
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	target := autopilot.ControllersTarget
	if !isControlPlane {
		target = autopilot.WorkersTarget
	}

	planParams := &autopilot.PlanParameters{
		// compose autopilot plan id by using machine name, timestamp and desired version to ensure uniqueness
		// in case of multiple updates on the same machine
		ID:          fmt.Sprintf("id-%s-%s-%s", desiredMachine.Name, desiredMachine.Spec.Version, timestamp),
		Timestamp:   timestamp,
		Version:     desiredMachine.Spec.Version,
		DownloadURL: downloadURL,
		Target:      target,
		Nodes:       []string{desiredMachine.Name},
	}

	err = autopilot.CreatePlan(ctx, clientset, planParams)
	if err != nil {
		return fmt.Errorf("error creating autopilot plan: %w", err)
	}

	return nil
}

func getWorkloadClusterClientset(ctx context.Context, client client.Client, cluster client.ObjectKey) (*kubernetes.Clientset, error) {
	data, err := kubeconfig.FromSecret(ctx, client, cluster)
	if err != nil {
		return nil, fmt.Errorf("error fetching %s kubeconfig from secret: %w", cluster.Name, err)
	}
	config, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("error generating %s clientconfig: %w", cluster.Name, err)
	}
	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s restconfig:  %w", cluster.Name, err)
	}

	tCfg, err := restConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s transport config: %w", cluster.Name, err)
	}
	tlsCfg, err := transport.TLSConfigFor(tCfg)
	if err != nil {
		return nil, fmt.Errorf("error generating %s tls config: %w", cluster.Name, err)
	}

	// Disable keep-alive to avoid hanging connections
	cl := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: -1,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       5 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       tlsCfg,
		},
	}

	return kubernetes.NewForConfigAndClient(restConfig, cl)
}

func getDownloadURLFromBootstrapConfig(ctx context.Context, client client.Client, configRef clusterv1.ContractVersionedObjectReference, isControlPlane bool, namespace string) (string, error) {
	downloadURL := ""

	uBootstrapConfig, err := external.GetObjectFromContractVersionedRef(ctx, client, configRef, namespace)
	if err != nil {
		return "", fmt.Errorf("error fetching bootstrap config: %w", err)
	}

	if isControlPlane {
		k0sControllerConfig := &bootstrapv1.K0sControllerConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uBootstrapConfig.Object, k0sControllerConfig)
		if err != nil {
			return "", fmt.Errorf("failed to convert controller bootstrap config for machine object: %w", err)
		}
		downloadURL = k0sControllerConfig.Spec.K0sConfigSpec.DownloadURL
	} else {
		k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uBootstrapConfig.Object, k0sWorkerConfig)
		if err != nil {
			return "", fmt.Errorf("failed to convert worker bootstrap config for machine object: %w", err)
		}
		downloadURL = k0sWorkerConfig.Spec.DownloadURL
	}

	return downloadURL, nil
}
