//go:build e2e

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

package mothership

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/k0smotron/k0smotron/e2e/util"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

// InitAndWatchControllerLogs initializes a management using clusterctl and setup watches for controller logs.
// Important: Considering we want to support test suites using existing clusters, clusterctl init is executed only in case
// there are no provider controllers in the cluster; but controller logs watchers are created regardless of the pre-existing providers.
func InitAndWatchControllerLogs(ctx context.Context, input clusterctl.InitManagementClusterAndWatchControllerLogsInput, interval util.Interval) error {
	if input.CoreProvider == "" {
		input.CoreProvider = config.ClusterAPIProviderName
	}
	if len(input.BootstrapProviders) == 0 {
		input.BootstrapProviders = []string{config.KubeadmBootstrapProviderName}
	}
	if len(input.ControlPlaneProviders) == 0 {
		input.ControlPlaneProviders = []string{config.KubeadmControlPlaneProviderName}
	}

	client := input.ClusterProxy.GetClient()
	controllersDeployments := framework.GetControllerDeployments(ctx, framework.GetControllerDeploymentsInput{
		Lister: client,
	})
	if len(controllersDeployments) == 0 {
		initInput := clusterctl.InitInput{
			// pass reference to the management cluster hosting this test
			KubeconfigPath: input.ClusterProxy.GetKubeconfigPath(),
			// pass the clusterctl config file that points to the local provider repository created for this test
			ClusterctlConfigPath: input.ClusterctlConfigPath,
			// setup the desired list of providers for a single-tenant management cluster
			CoreProvider:              input.CoreProvider,
			BootstrapProviders:        input.BootstrapProviders,
			ControlPlaneProviders:     input.ControlPlaneProviders,
			InfrastructureProviders:   input.InfrastructureProviders,
			IPAMProviders:             input.IPAMProviders,
			RuntimeExtensionProviders: input.RuntimeExtensionProviders,
			AddonProviders:            input.AddonProviders,
			// setup clusterctl logs folder
			LogFolder: input.LogFolder,
		}

		clusterctl.Init(ctx, initInput)
	}

	fmt.Println("Waiting for provider controllers to be running")
	controllersDeployments = framework.GetControllerDeployments(ctx, framework.GetControllerDeploymentsInput{
		Lister: client,
	})
	if len(controllersDeployments) == 0 {
		return errors.New("the list of controller deployments should not be empty")
	}

	for _, deployment := range controllersDeployments {
		err := util.WaitForDeploymentsAvailable(ctx, framework.WaitForDeploymentsAvailableInput{
			Getter:     client,
			Deployment: deployment,
		}, interval)
		if err != nil {
			return err
		}

		// Start streaming logs from all controller providers
		framework.WatchDeploymentLogsByName(ctx, framework.WatchDeploymentLogsByNameInput{
			GetLister:  client,
			Cache:      input.ClusterProxy.GetCache(ctx),
			ClientSet:  input.ClusterProxy.GetClientSet(),
			Deployment: deployment,
			LogPath:    filepath.Join(input.LogFolder, "logs", deployment.GetNamespace()),
		})

		if !input.DisableMetricsCollection {
			framework.WatchPodMetrics(ctx, framework.WatchPodMetricsInput{
				GetLister:   client,
				ClientSet:   input.ClusterProxy.GetClientSet(),
				Deployment:  deployment,
				MetricsPath: filepath.Join(input.LogFolder, "metrics", deployment.GetNamespace()),
			})
		}
	}

	return nil
}
