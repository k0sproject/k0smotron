#!/usr/bin/env bash
# Render CRDs into the Helm chart as templates.
#
# The kubebuilder helm/v1-alpha plugin only copies the raw CRD bases
# (config/crd/bases via controller-gen), which lack the conversion-webhook and
# cert-manager CA-injection wiring that lives in kustomize patches. k0smotron
# serves v1beta1 and v1beta2 with a conversion webhook, so those CRDs would be
# broken if shipped raw.
#
# Instead we render the fully-patched CRDs from `config/clusterapi/all` (the same
# source as install.yaml), repoint their cert references at the chart's
# cert-manager Certificate (serving-cert) in the release namespace, gate each
# behind .Values.crd.enable, and write them to dist/chart/templates/crd/.
#
# Run this AFTER `kubebuilder edit --plugins=helm/v1-alpha`.
set -euo pipefail

KUSTOMIZE=${KUSTOMIZE:-bin/kustomize}
YQ=${YQ:-yq}
SRC=${SRC:-config/clusterapi/all}
CRD_DIR=${CRD_DIR:-dist/chart/templates/crd}

# The chart's webhook Service is named k0smotron-webhook-service in both the
# plugin output and the kustomize output, so only the namespace and the
# cert-manager Certificate reference need to be templated to the release.
TRANSFORM='select(.kind == "CustomResourceDefinition")
  | (.spec.conversion.webhook.clientConfig.service.namespace | select(. != null)) = "{{ .Release.Namespace }}"
  | (.metadata.annotations."cert-manager.io/inject-ca-from" | select(. != null)) = "{{ .Release.Namespace }}/serving-cert"'

rm -rf "$CRD_DIR"
mkdir -p "$CRD_DIR"

# Render + transform, then split one file per CRD into a temp dir. CRD names
# contain dots (e.g. clusters.k0smotron.io), which yq mistakes for a file
# extension, so we wrap whatever it writes rather than assuming a suffix.
# Keep the temp dir inside the chart tree so we never depend on a writable
# system TMPDIR (restricted in some CI/sandbox environments).
tmp="$CRD_DIR/.split"
mkdir -p "$tmp"
trap 'rm -rf "$tmp"' EXIT

"$KUSTOMIZE" build "$SRC" --load-restrictor LoadRestrictionsNone \
  | "$YQ" eval "$TRANSFORM" - \
  | "$YQ" eval --no-doc --split-exp "\"$tmp/\" + .metadata.name" -

# Wrap each CRD in the chart's enable toggle.
count=0
for f in "$tmp"/*; do
  name=$(basename "$f")
  {
    echo '{{- if .Values.crd.enable }}'
    cat "$f"
    echo '{{- end }}'
  } > "$CRD_DIR/$name.yaml"
  count=$((count + 1))
done

echo "helm-crds: wrote $count CRD templates to $CRD_DIR"
