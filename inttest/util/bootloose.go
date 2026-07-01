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
	"context"
	"fmt"

	"github.com/k0sproject/k0s/inttest/common"
)

// ImportK0smotronImages imports the k0smotron image bundle on all worker nodes.
// The bundle must be mounted at K0sExtraImageBundleMountPoints[0].
func ImportK0smotronImages(ctx context.Context, s *common.BootlooseSuite) error {
	if len(s.K0sExtraImageBundleMountPoints) == 0 {
		return nil
	}
	bundlePath := s.K0sExtraImageBundleMountPoints[0]
	for i := range s.WorkerCount {
		workerNode := s.WorkerNode(i)
		s.T().Logf("Importing images in %s", workerNode)
		sshWorker, err := s.SSH(ctx, workerNode)
		if err != nil {
			return err
		}
		defer sshWorker.Disconnect()
		_, err = sshWorker.ExecWithOutput(ctx, fmt.Sprintf("k0s ctr images import %s", bundlePath))
		if err != nil {
			return fmt.Errorf("failed to import k0smotron images: %w", err)
		}
	}
	return nil
}
