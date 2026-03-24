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
//nolint:revive
package util

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// joinEncode compresses and base64 encodes a join token
func joinEncode(in io.Reader) (string, error) {
	var outBuf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&outBuf, gzip.BestCompression)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(gz, in)
	gzErr := gz.Close()
	if err != nil {
		return "", err
	}
	if gzErr != nil {
		return "", gzErr
	}

	return base64.StdEncoding.EncodeToString(outBuf.Bytes()), nil
}

// CreateK0sJoinToken creates a join token for k0s using the provided CA certificate,
// token, join URL, and username.
func CreateK0sJoinToken(caCert []byte, token string, joinURL string, userName string) (string, error) {
	const k0sContextName = "k0s"
	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{k0sContextName: {
			Server:                   joinURL,
			CertificateAuthorityData: caCert,
		}},
		Contexts: map[string]*clientcmdapi.Context{k0sContextName: {
			Cluster:  k0sContextName,
			AuthInfo: userName,
		}},
		CurrentContext: k0sContextName,
		AuthInfos: map[string]*clientcmdapi.AuthInfo{userName: {
			Token: token,
		}},
	})
	if err != nil {
		return "", err
	}
	return joinEncode(bytes.NewReader(kubeconfig))
}
