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

package util

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForDeploymentsAvailable waits until the Deployment has status.Available = True, that signals that
// all the desired replicas are in place.
func WaitForDeploymentsAvailable(ctx context.Context, input framework.WaitForDeploymentsAvailableInput, interval Interval) error {
	fmt.Printf("Waiting for deployment %s to be available\n", klog.KObj(input.Deployment))
	deployment := &appsv1.Deployment{}
	err := wait.PollUntilContextTimeout(ctx, interval.tick, interval.timeout, true, func(ctx context.Context) (done bool, err error) {
		key := client.ObjectKey{
			Namespace: input.Deployment.GetNamespace(),
			Name:      input.Deployment.GetName(),
		}
		if err := input.Getter.Get(ctx, key, deployment); err != nil {
			return false, err
		}
		for _, c := range deployment.Status.Conditions {
			if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return errors.New(framework.DescribeFailedDeployment(input, deployment))
	}

	return nil
}
