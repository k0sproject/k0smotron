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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder is a utility for managing port forwarding to a Kubernetes pod.
type PortForwarder struct {
	stopChan  chan struct{}
	ReadyChan chan struct{}
	fw        *portforward.PortForwarder
}

// ErrorHandler is a function type for handling errors,
// typically used in the Start method of PortForwarder.
type ErrorHandler func(err error, msgAndArgs ...any)

// GetPortForwarder creates a new PortForwarder for the specified pod and port.
func GetPortForwarder(cfg *rest.Config, name string, namespace string, port int) (*PortForwarder, error) {
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}

	host := getHost(cfg.Host)

	url := &url.URL{
		Scheme: "https",
		Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, name),
		Host:   host,
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d", port)}, stopChan, readyChan, io.Discard, os.Stderr)
	if err != nil {
		return nil, err
	}

	pf := &PortForwarder{
		stopChan:  stopChan,
		ReadyChan: readyChan,
		fw:        fw,
	}
	return pf, nil
}

// Start runs ForwardPorts. The errorHandler is expected to be
// s.Require().NoError
func (pf *PortForwarder) Start(errorHandler ErrorHandler) {
	errorHandler(pf.fw.ForwardPorts())
}

// Close closes the forwarder
func (pf *PortForwarder) Close() {
	close(pf.stopChan)
	pf.fw.Close()
}

// LocalPort returns the local port that is being forwarded to the pod.
func (pf *PortForwarder) LocalPort() (int, error) {
	ports, err := pf.fw.GetPorts()
	if err != nil {
		return 0, err
	}
	return int(ports[0].Local), nil
}

func getHost(input string) string {
	u, err := url.Parse(input)
	if err != nil {
		// Assume the input is a plain host:port
		// The url.Parse is bit ambiguous when it would actually spit out an error -\_('-')_/-
		return input
	}
	if u.Host == "" {
		return input
	}

	return u.Host
}
