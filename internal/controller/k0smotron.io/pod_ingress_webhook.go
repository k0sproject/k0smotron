package k0smotronio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const PodIngressMutatorPath = "/mutate-child-cluster-pods"

// PodIngressMutatorHandler is an http.Handler that injects KUBERNETES_SERVICE_HOST
// and KUBERNETES_SERVICE_PORT env vars into child cluster pods so they can reach
// the API server through the ingress before the local HAProxy DaemonSet is running.
// It reads the target host/port from the `apiHost` and `apiPort` query parameters.
type PodIngressMutatorHandler struct{}

func (h *PodIngressMutatorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, "failed to read request body")
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	apiHost := r.URL.Query().Get("apiHost")
	apiPort := r.URL.Query().Get("apiPort")
	if apiHost == "" || apiPort == "" {
		http.Error(w, "apiHost and apiPort query params are required", http.StatusBadRequest)
		return
	}

	var admReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admReview); err != nil {
		logger.Error(err, "failed to decode admission review")
		http.Error(w, fmt.Sprintf("failed to decode admission review: %v", err), http.StatusBadRequest)
		return
	}
	if admReview.Request == nil {
		http.Error(w, "admission review request is nil", http.StatusBadRequest)
		return
	}

	pod := &corev1.Pod{}
	if err := json.Unmarshal(admReview.Request.Object.Raw, pod); err != nil {
		logger.Error(err, "failed to decode pod")
		http.Error(w, fmt.Sprintf("failed to decode pod: %v", err), http.StatusBadRequest)
		return
	}

	patches := buildEnvVarPatches(pod.Spec.Containers, "/spec/containers", apiHost, apiPort)
	patches = append(patches, buildEnvVarPatches(pod.Spec.InitContainers, "/spec/initContainers", apiHost, apiPort)...)

	resp := &admissionv1.AdmissionReview{
		TypeMeta: admReview.TypeMeta,
		Response: &admissionv1.AdmissionResponse{
			UID:     admReview.Request.UID,
			Allowed: true,
		},
	}

	if len(patches) > 0 {
		patchBytes, err := json.Marshal(patches)
		if err != nil {
			logger.Error(err, "failed to marshal patches")
			http.Error(w, "failed to marshal patches", http.StatusInternalServerError)
			return
		}
		patchType := admissionv1.PatchTypeJSONPatch
		resp.Response.Patch = patchBytes
		resp.Response.PatchType = &patchType
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		logger.Error(err, "failed to marshal response")
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error(err, "failed to write response")
	}
}

type jsonPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// buildEnvVarPatches builds JSON patch operations to inject KUBERNETES_SERVICE_HOST
// and KUBERNETES_SERVICE_PORT into containers that don't already have them set.
// It handles the case where the env array is nil (requires creating the array first).
func buildEnvVarPatches(containers []corev1.Container, basePath, apiHost, apiPort string) []jsonPatch {
	var patches []jsonPatch
	for i, c := range containers {
		hasHost, hasPort := false, false
		for _, env := range c.Env {
			if env.Name == "KUBERNETES_SERVICE_HOST" {
				hasHost = true
			}
			if env.Name == "KUBERNETES_SERVICE_PORT" {
				hasPort = true
			}
		}
		if hasHost && hasPort {
			continue
		}

		containerPath := fmt.Sprintf("%s/%d", basePath, i)
		// effectiveEnvLen tracks how many env vars the container effectively has
		// after accounting for patches we've already added for it.
		effectiveEnvLen := len(c.Env)

		if !hasHost {
			patches = append(patches, makeEnvPatch(containerPath, effectiveEnvLen, "KUBERNETES_SERVICE_HOST", apiHost))
			effectiveEnvLen++
		}
		if !hasPort {
			patches = append(patches, makeEnvPatch(containerPath, effectiveEnvLen, "KUBERNETES_SERVICE_PORT", apiPort))
		}
	}
	return patches
}

func makeEnvPatch(containerPath string, currentEnvLen int, name, value string) jsonPatch {
	envVar := corev1.EnvVar{Name: name, Value: value}
	if currentEnvLen == 0 {
		// Array doesn't exist yet; create it with the new var as the first element.
		return jsonPatch{Op: "add", Path: containerPath + "/env", Value: []corev1.EnvVar{envVar}}
	}
	return jsonPatch{Op: "add", Path: containerPath + "/env/-", Value: envVar}
}
