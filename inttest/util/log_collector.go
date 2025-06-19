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
	"os"
	"path/filepath"
	"time"
)

// WaitForLogFile waits for k0s log files to be created
func WaitForLogFile(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Check if any k0s log files exist
			matches, err := filepath.Glob("/tmp/k0s-*.log")
			if err == nil && len(matches) > 0 {
				return nil
			}

			time.Sleep(1 * time.Second)
		}
	}

	return fmt.Errorf("timeout waiting for k0s log files")
}

// SafeCollectLogs attempts to collect logs but doesn't fail if they don't exist
func SafeCollectLogs(_ string) (string, error) {
	matches, err := filepath.Glob("/tmp/k0s-*.log")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no k0s log files found (this is expected if k0s hasn't started yet)")
	}

	// Collect logs from the first matching file
	content, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	return string(content), nil
}

// WaitForK0sToStart waits for k0s process to start by checking for log files
func WaitForK0sToStart(ctx context.Context, nodeName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Check if k0s process is running by looking for log files
			matches, err := filepath.Glob("/tmp/k0s-*.log")
			if err == nil && len(matches) > 0 {
				// Also check if the log file has some content
				info, err := os.Stat(matches[0])
				if err == nil && info.Size() > 0 {
					return nil
				}
			}

			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("timeout waiting for k0s to start on node %s", nodeName)
}
