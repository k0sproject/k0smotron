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

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

// GetKubeClient returns a Kubernetes clientset for the given cluster REST config.
func GetKubeClient(restConfig *rest.Config) (*kubernetes.Clientset, error) {
	tCfg, err := restConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating transport config: %w", err)
	}
	tlsCfg, err := transport.TLSConfigFor(tCfg)
	if err != nil {
		return nil, fmt.Errorf("error generating tls config: %w", err)
	}

	// Disable keep-alive to avoid hanging connections
	cl := http.DefaultClient
	cl.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig:       tlsCfg,
	}

	return kubernetes.NewForConfigAndClient(restConfig, cl)
}
