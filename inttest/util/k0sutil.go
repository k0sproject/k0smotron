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
	"errors"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/inttest/util/k0scontext"
	"github.com/k0sproject/k0smotron/inttest/util/watch"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// Poll tries a condition func until it returns true, an error or the specified
// context is canceled or expired.
func Poll(ctx context.Context, condition wait.ConditionWithContextFunc) error {
	return wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, condition)
}

// LogfFn will be used whenever something needs to be logged.
type LogfFn func(format string, args ...any)

// Retrieves the LogfFn stored in context, falling back to use testing.T's Logf
// if the context has a *testing.T or logrus's Infof as a last resort.
func logfFrom(ctx context.Context) LogfFn {
	if logf := k0scontext.Value[LogfFn](ctx); logf != nil {
		return logf
	}
	if t := k0scontext.Value[*testing.T](ctx); t != nil {
		return t.Logf
	}
	return logrus.Infof
}

// LineWriter is an io.Writer that buffers data until it encounters a newline character,
type LineWriter struct {
	WriteLine func([]byte)
	buf       []byte
}

// Write implements [io.Writer].
func (s *LineWriter) Write(in []byte) (int, error) {
	s.buf = append(s.buf, in...)
	s.logLines()
	return len(in), nil
}

// Logs each complete line and discards the used data.
func (s *LineWriter) logLines() {
	var off int
	for {
		n := bytes.IndexByte(s.buf[off:], '\n')
		if n < 0 {
			break
		}

		s.WriteLine(s.buf[off : off+n])
		off += n + 1
	}

	// Move the unprocessed data to the beginning of the buffer and reset the length.
	if off > 0 {
		l := copy(s.buf, s.buf[off:])
		s.buf = s.buf[:l]
	}
}

// Flush should be called before stopping the writer to ensure all data is logged.
// Logs any remaining data in the buffer that doesn't end with a newline.
func (s *LineWriter) Flush() {
	if len(s.buf) > 0 {
		s.WriteLine(s.buf)
		// Reset the length and keep the underlying array.
		s.buf = s.buf[:0]
	}
}

// WaitForNodeReadyStatus waits until the node with the given name has the specified Ready condition status.
func WaitForNodeReadyStatus(ctx context.Context, clients kubernetes.Interface, nodeName string, status corev1.ConditionStatus) error {
	return watch.Nodes(clients.CoreV1().Nodes()).
		WithObjectName(nodeName).
		WithErrorCallback(RetryWatchErrors(logfFrom(ctx))).
		Until(ctx, func(node *corev1.Node) (done bool, err error) {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					if cond.Status == status {
						return true, nil
					}

					break
				}
			}

			return false, nil
		})
}

// RetryWatchErrors returns a watch.ErrorCallback that retries on transient errors and logs them
// using the provided LogfFn.
func RetryWatchErrors(logf LogfFn) watch.ErrorCallback {
	return func(err error) (time.Duration, error) {
		if retryDelay, e := watch.IsRetryable(err); e == nil {
			logf("Encountered transient watch error, retrying in %s: %v", retryDelay, err)
			return retryDelay, nil
		}

		retryDelay := 1 * time.Second

		switch {
		case errors.Is(err, syscall.ECONNRESET):
			logf("Encountered connection reset while watching, retrying in %s: %v", retryDelay, err)
			return retryDelay, nil

		case errors.Is(err, syscall.ECONNREFUSED):
			logf("Encountered connection refused while watching, retrying in %s: %v", retryDelay, err)
			return retryDelay, nil

		case errors.Is(err, io.EOF):
			logf("Encountered EOF while watching, retrying in %s: %v", retryDelay, err)
			return retryDelay, nil
		}

		return 0, err
	}
}

// WaitForPod waits until the pod with the given name and namespace has the Ready condition set to True.
func WaitForPod(ctx context.Context, kc *kubernetes.Clientset, name, namespace string) error {
	return watch.Pods(kc.CoreV1().Pods(namespace)).
		WithObjectName(name).
		WithErrorCallback(RetryWatchErrors(logfFrom(ctx))).
		Until(ctx, func(pod *corev1.Pod) (bool, error) {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady {
					if cond.Status == corev1.ConditionTrue {
						return true, nil
					}

					break
				}
			}

			return false, nil
		})
}

// WaitForDeployment waits for the Deployment with the given name to become
// available as long as the given context isn't canceled.
func WaitForDeployment(ctx context.Context, kc *kubernetes.Clientset, name, namespace string) error {
	return watch.Deployments(kc.AppsV1().Deployments(namespace)).
		WithObjectName(name).
		WithErrorCallback(RetryWatchErrors(logrus.Infof)).
		Until(ctx, func(deployment *appsv1.Deployment) (bool, error) {
			for _, c := range deployment.Status.Conditions {
				if c.Type == appsv1.DeploymentAvailable {
					if c.Status == corev1.ConditionTrue {
						return true, nil
					}

					break
				}
			}

			return false, nil
		})
}

// WaitForStatefulSet waits for the StatefulSet with the given name to have
// as many ready replicas as defined in the spec.
func WaitForStatefulSet(ctx context.Context, kc *kubernetes.Clientset, name, namespace string) error {
	return watch.StatefulSets(kc.AppsV1().StatefulSets(namespace)).
		WithObjectName(name).
		WithErrorCallback(RetryWatchErrors(logrus.Infof)).
		Until(ctx, func(s *appsv1.StatefulSet) (bool, error) {
			return s.Status.ReadyReplicas == *s.Spec.Replicas, nil
		})
}
