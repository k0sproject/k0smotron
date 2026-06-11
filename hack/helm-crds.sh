#!/usr/bin/env bash
# Render CRDs and RBAC into the Helm chart from the kustomize install.
#
# The kubebuilder helm/v1-alpha plugin scaffolds from the standard kubebuilder
# layout, which k0smotron doesn't follow:
#   - CRDs: the plugin copies the raw config/crd/bases, missing the conversion-
#     webhook and cert-manager CA-injection patches. k0smotron serves v1beta1 and
#     v1beta2 with a conversion webhook, so raw CRDs would be broken.
#   - RBAC: the plugin only finds config/rbac/role.yaml (a partial standalone
#     role), missing the full manager ClusterRole, the ServiceAccount, the
#     leader-election Role, and both bindings - so the manager can't even start.
#
# Both come out correct and mutually consistent from `config/clusterapi/all`
# (the same source as install.yaml). We render them, re-point namespace/cert
# references at the release, gate them behind their chart toggles, and write them
# over the plugin's output.
#
# Run this AFTER `kubebuilder edit --plugins=helm/v1-alpha`.
set -euo pipefail

KUSTOMIZE=${KUSTOMIZE:-bin/kustomize}
YQ=${YQ:-yq}
SRC=${SRC:-config/clusterapi/all}
TPL=${TPL:-dist/chart/templates}

# Build the full install once and reuse it for every extraction. Keep scratch
# files inside the chart tree so we never depend on a writable system TMPDIR
# (restricted in some CI/sandbox environments).
mkdir -p "$TPL"
rendered="$TPL/.rendered.yaml"
"$KUSTOMIZE" build "$SRC" --load-restrictor LoadRestrictionsNone > "$rendered"
trap 'rm -f "$rendered"' EXIT

# split_wrap <input.yaml> <out_dir> <guard>
# Splits a multi-doc stream one file per resource (yq mistakes the dots in names
# like clusters.k0smotron.io for an extension, so we wrap whatever it writes
# rather than assume a suffix) and wraps each in a Helm enable toggle.
split_wrap() {
  local input="$1" out="$2" guard="$3" tmp="$2/.split" name count=0
  rm -rf "$out"; mkdir -p "$tmp"
  "$YQ" eval --no-doc --split-exp "\"$tmp/\" + .metadata.name" "$input"
  for f in "$tmp"/*; do
    # yq appends .yml only when the name has no dot (e.g. RBAC names); CRD names
    # like clusters.k0smotron.io keep none. Strip it so we don't get .yml.yaml.
    name=$(basename "$f"); name="${name%.yml}"
    { echo "{{- if $guard }}"; cat "$f"; echo '{{- end }}'; } > "$out/$name.yaml"
    count=$((count + 1))
  done
  rm -rf "$tmp"
  echo "$count"
}

# --- CRDs --------------------------------------------------------------------
# Strip schema descriptions: Helm stores the whole chart (raw templates) plus the
# rendered manifest in one release Secret capped at 1 MiB; the embedded
# PodSpec/JobSpec descriptions (~5 MiB of CRDs) blow that. Descriptions are docs
# only - no effect on validation/conversion - and install.yaml keeps the full
# CRDs for `kubectl explain`.
# Use with(select(...); ...) not a guarded assignment: referencing a deep path on
# the left of `=` makes yq materialise the intermediate nodes even when a trailing
# select filters out the write, injecting a half-built conversion block (no
# strategy) into CRDs that have none - the apiserver rejects that.
CRD_TRANSFORM='select(.kind == "CustomResourceDefinition")
  | del(.. | .description?)
  | with(select(.spec.conversion.strategy == "Webhook"); .spec.conversion.webhook.clientConfig.service.namespace = "{{ .Release.Namespace }}")
  | with(select(.metadata.annotations."cert-manager.io/inject-ca-from" != null); .metadata.annotations."cert-manager.io/inject-ca-from" = "{{ .Release.Namespace }}/serving-cert")'
"$YQ" eval "$CRD_TRANSFORM" "$rendered" > "$TPL/.crds.yaml"
crd_count=$(split_wrap "$TPL/.crds.yaml" "$TPL/crd" ".Values.crd.enable")
rm -f "$TPL/.crds.yaml"

# --- RBAC + ServiceAccount ---------------------------------------------------
# The manager Deployment (from the plugin) references
# .Values.controllerManager.serviceAccountName; kustomize names the SA, roles and
# bindings k0smotron-* to match. Namespaced objects and binding subjects are
# pinned to k0smotron by kustomize, so re-point them at the release namespace.
RBAC_TRANSFORM='select(.kind == "ServiceAccount" or .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or .kind == "Role" or .kind == "RoleBinding")
  | with(select(.metadata.namespace != null); .metadata.namespace = "{{ .Release.Namespace }}")
  | with(select(.subjects != null); .subjects[].namespace = "{{ .Release.Namespace }}")'
"$YQ" eval "$RBAC_TRANSFORM" "$rendered" > "$TPL/.rbac.yaml"
rbac_count=$(split_wrap "$TPL/.rbac.yaml" "$TPL/rbac" ".Values.rbac.enable")
rm -f "$TPL/.rbac.yaml"

echo "helm-crds: wrote $crd_count CRD and $rbac_count RBAC templates to $TPL"
