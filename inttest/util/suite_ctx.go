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

//nolint:revive
package util

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// NewSuiteContext creates a context for the test suite that is aware of OS signals and test deadlines.
func NewSuiteContext(t *testing.T) (context.Context, context.CancelCauseFunc) {
	signalCtx, cancel := signalAwareCtx(context.Background())

	// We need to reserve some time to conduct a proper teardown of the suite before the test timeout kicks in.
	deadline, hasDeadline := t.Deadline()
	if !hasDeadline {
		return signalCtx, cancel
	}

	remainingTestDuration := time.Until(deadline)
	//  Let's reserve 10% ...
	reservedTeardownDuration := time.Duration(float64(remainingTestDuration.Milliseconds())*0.10) * time.Millisecond
	// ... but at least 20 seconds.
	reservedTeardownDuration = time.Duration(math.Max(float64(20*time.Second), float64(reservedTeardownDuration)))
	// Then construct the context accordingly.
	deadlineCtx, subCancel := context.WithDeadline(signalCtx, deadline.Add(-reservedTeardownDuration))
	_ = subCancel // Silence linter: the deadlined context is implicitly canceled when canceling the signal context

	return deadlineCtx, cancel
}

func signalAwareCtx(parent context.Context) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(parent)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigs)
		select {
		case <-ctx.Done():
		case sig := <-sigs:
			cancel(fmt.Errorf("signal received: %s", sig))
		}
	}()

	return ctx, cancel
}
