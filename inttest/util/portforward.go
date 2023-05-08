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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PortForwarder struct {
	stopChan  chan struct{}
	ReadyChan chan struct{}
	fw        *portforward.PortForwarder
}
type ErrorHandler func(err error, msgAndArgs ...interface{})

func GetPortForwarder(cfg *rest.Config, name string, namespace string) (*PortForwarder, error) {
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}

	url := &url.URL{
		Scheme: "https",
		Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, name),
		Host:   cfg.Host,
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	fw, err := portforward.New(dialer, []string{"30443"}, stopChan, readyChan, io.Discard, os.Stderr)
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
