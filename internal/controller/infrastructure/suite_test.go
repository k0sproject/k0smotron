//go:build envtest

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

package infrastructure

import (
	"os"
	"testing"

	"github.com/k0sproject/k0smotron/v2/internal/test/envtest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	testEnv *envtest.Environment
	ctx     = ctrl.SetupSignalHandler()
)

func TestMain(m *testing.M) {
	// Nothing here reads secrets, so the cached secret client the other suites
	// build is not needed.
	testEnv = envtest.Build(ctx, func(manager.Manager) error { return nil })
	code := m.Run()
	testEnv.Teardown()
	os.Exit(code)
}
