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

package util

import ctrl "sigs.k8s.io/controller-runtime"

// RequeueWithBackoff asks for the request to be requeued through the
// controller's rate limiter, so retries back off. RequeueAfter would fix the
// delay instead, and controller-runtime has no replacement for this yet.
func RequeueWithBackoff() ctrl.Result {
	return ctrl.Result{Requeue: true} //nolint:staticcheck // See above.
}
