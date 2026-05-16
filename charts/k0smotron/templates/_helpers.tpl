{{/*
Expand the name of the chart.
*/}}
{{- define "k0smotron.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "k0smotron.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart label value.
*/}}
{{- define "k0smotron.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "k0smotron.labels" -}}
helm.sh/chart: {{ include "k0smotron.chart" . }}
{{ include "k0smotron.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: k0smotron
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels used in Deployment and Services.
*/}}
{{- define "k0smotron.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k0smotron.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
ServiceAccount name for the controller manager.
*/}}
{{- define "k0smotron.serviceAccountName" -}}
{{- printf "%s-controller-manager" (include "k0smotron.fullname" .) }}
{{- end }}

{{/*
Full image reference: repository:tag
*/}}
{{- define "k0smotron.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/*
Webhook service name.
*/}}
{{- define "k0smotron.webhookServiceName" -}}
{{- printf "%s-webhook-service" (include "k0smotron.fullname" .) }}
{{- end }}

{{/*
cert-manager Certificate name.
*/}}
{{- define "k0smotron.certName" -}}
{{- printf "%s-serving-cert" (include "k0smotron.fullname" .) }}
{{- end }}

{{/*
cert-manager CA injection annotation value: <namespace>/<cert-name>
*/}}
{{- define "k0smotron.caInjectAnnotation" -}}
{{- printf "%s/%s" .Release.Namespace (include "k0smotron.certName" .) }}
{{- end }}

{{/*
Webhook server TLS secret name.
*/}}
{{- define "k0smotron.webhookSecretName" -}}
{{- printf "%s-webhook-server-cert" (include "k0smotron.fullname" .) }}
{{- end }}
