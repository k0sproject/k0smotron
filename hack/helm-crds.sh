#!/usr/bin/env bash
# Render RBAC into the parent chart and CRDs into a bundled crds subchart, both
# from the kustomize install (config/clusterapi/all, the install.yaml source).
#
# Why this exists - the kubebuilder helm/v1-alpha plugin scaffolds from the
# standard kubebuilder layout, which k0smotron doesn't follow, so its output is
# incomplete:
#   - RBAC: only config/rbac/role.yaml (a partial standalone ClusterRole) is
#     found - no ServiceAccount, no leader-election Role, no bindings, so the
#     manager can't start.
#   - CRDs: the raw config/crd/bases are copied without the conversion-webhook or
#     cert-manager CA wiring, so v1beta1<->v1beta2 conversion is broken.
#
# RBAC is small and namespace-dependent, so it goes in the parent templates/ and
# is templated to .Release.Namespace.
#
# CRDs go in the k0smotron-crds subchart's templates/ directory so they can be
# gated per controller via .Values.global.controllers. The full-fat CRDs
# (descriptions kept) are ~5 MiB raw but gzip to well under Helm's 1 MiB
# release-Secret limit. The conversion-webhook and cert-manager namespaces are
# baked to $CRD_NAMESPACE (not templated to .Release.Namespace); the parent NOTES
# guard enforces installing into that namespace.
#
# Run this AFTER `kubebuilder edit --plugins=helm/v1-alpha`.
set -euo pipefail

KUSTOMIZE=${KUSTOMIZE:-bin/kustomize}
YQ=${YQ:-yq}
SRC=${SRC:-config/clusterapi/all}
CHART=${CHART:-dist/chart}
CHART_VERSION=${CHART_VERSION:-0.0.0-dev}
CRD_NAMESPACE=${CRD_NAMESPACE:-k0smotron-system}

TPL="$CHART/templates"
SUB="$CHART/charts/k0smotron-crds"

mkdir -p "$TPL"
rendered="$TPL/.rendered.yaml"
"$KUSTOMIZE" build "$SRC" --load-restrictor LoadRestrictionsNone > "$rendered"
trap 'rm -f "$rendered"' EXIT

# --- RBAC + ServiceAccount (parent templates/, namespace-templated) ----------
# The manager Deployment references .Values.controllerManager.serviceAccountName;
# kustomize names the SA/roles/bindings k0smotron-* to match. Namespaced objects
# and binding subjects are pinned to k0smotron by kustomize - re-point at release.
# One file, gated by a single conditional wrapping all docs.
rm -rf "$TPL/rbac"; mkdir -p "$TPL/rbac"
{
  echo '{{- if .Values.rbac.enable }}'
  "$YQ" eval '
    select(.kind == "ServiceAccount" or .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or .kind == "Role" or .kind == "RoleBinding")
    | with(select(.metadata.namespace != null); .metadata.namespace = "{{ .Release.Namespace }}")
    | with(select(.subjects != null); .subjects[].namespace = "{{ .Release.Namespace }}")
  ' "$rendered"
  echo '{{- end }}'
} > "$TPL/rbac/rbac.yaml"

# --- CRDs (crds subchart, templated for per-controller selection) ------------
# CRDs live in the subchart's templates/ (not crds/) so that
# .Values.global.controller can gate which CRDs install: each CRD is wrapped in
# a controller conditional keyed off the CRD's API group. The full set is ~5 MiB
# raw but gzips to well under Helm's ~1 MiB release-Secret limit. The
# conversion-webhook / cert-manager CA namespaces are still baked to
# $CRD_NAMESPACE (the parent NOTES guard enforces installing there). When
# .Values.global.crdKeep is set, each CRD is annotated helm.sh/resource-policy:
# keep so it survives `helm uninstall`. Use with(select(...);...) not a guarded
# assignment: a deep path on the left of `=` makes yq materialise the
# intermediate nodes, injecting a half-built conversion block (no strategy) into
# CRDs that have none - the apiserver rejects that. yq's --split-exp keeps the
# dotted CRD name verbatim (no extension), so just append .yaml.
rm -rf "$CHART/templates/crd" "$SUB/crds" "$SUB/templates"; mkdir -p "$SUB/.split"
"$YQ" eval '
  select(.kind == "CustomResourceDefinition")
  | with(select(.spec.conversion.strategy == "Webhook"); .spec.conversion.webhook.clientConfig.service.namespace = "'"$CRD_NAMESPACE"'")
  | with(select(.metadata.annotations."cert-manager.io/inject-ca-from" != null); .metadata.annotations."cert-manager.io/inject-ca-from" = "'"$CRD_NAMESPACE"'/serving-cert")
' "$rendered" \
  | "$YQ" eval --no-doc --split-exp "\"$SUB/.split/\" + .metadata.name" -
mkdir -p "$SUB/templates"
# Injected under each CRD's metadata.annotations (which yq emits at line 4).
keep='    {{- if (.Values.global | default dict).crdKeep }}\n    "helm.sh/resource-policy": keep\n    {{- end }}'
for f in "$SUB/.split"/*; do
  base=$(basename "$f")
  # Map the CRD's API group to the controller that owns it. The k0smotron.io
  # (standalone) CRDs ship with control-plane: enabling that controller also
  # runs the standalone controllers, which reconcile them.
  case "$base" in
    *.bootstrap.cluster.x-k8s.io)      name='bootstrap';;
    *.controlplane.cluster.x-k8s.io)   name='control-plane';;
    *.infrastructure.cluster.x-k8s.io) name='infrastructure';;
    *.k0smotron.io)                    name='control-plane';;
    *) echo "helm-crds: unclassified CRD $base" >&2; exit 1;;
  esac
  {
    echo '{{- $ctrl := (.Values.global | default dict).controller | default "all" -}}'
    echo "{{- if has \$ctrl (list \"all\" \"$name\") }}"
    awk -v keep="$keep" 'NR==4 && $0=="  annotations:"{print; print keep; next} {print}' "$f"
    echo '{{- end }}'
  } > "$SUB/templates/$base.yaml"
done
rm -rf "$SUB/.split"

# Subchart metadata.
cat > "$SUB/Chart.yaml" <<EOF
apiVersion: v2
name: k0smotron-crds
description: k0smotron CustomResourceDefinitions
type: application
version: $CHART_VERSION
appVersion: "$CHART_VERSION"
EOF

# Wire the subchart into the parent: dependency (for the condition toggle) + values.
"$YQ" -i '.dependencies = [{"name": "k0smotron-crds", "version": "'"$CHART_VERSION"'", "condition": "crds.enabled", "repository": "file://charts/k0smotron-crds"}]' "$CHART/Chart.yaml"
"$YQ" -i '.crds.enabled = true | .crds.namespace = "'"$CRD_NAMESPACE"'"' "$CHART/values.yaml"
# global.* is shared with the CRD subchart: which controller to run (drives both
# the manager --enable-controller flag and CRD selection) and whether CRDs get the
# resource-policy: keep annotation. Only seed defaults if absent, to preserve any
# hand-written values/comments.
"$YQ" -i '.global.controller = (.global.controller // "all") | .global.crdKeep = (.global.crdKeep // true)' "$CHART/values.yaml"

# The CRDs' conversion webhook is baked to .Values.crds.namespace (crds/ can't be
# templated). Abort the install up front if the release namespace differs.
cat > "$TPL/validate-namespace.yaml" <<'EOF'
{{- if .Values.crds.enabled }}
{{- if ne .Release.Namespace .Values.crds.namespace }}
{{- fail (printf "\nk0smotron CRDs are baked to namespace %q (the conversion webhook references it) but you are installing into %q.\nInstall into %q (helm install ... --namespace %s), or regenerate the chart with `make helm-chart CRD_NAMESPACE=%s`." .Values.crds.namespace .Release.Namespace .Values.crds.namespace .Values.crds.namespace .Release.Namespace) }}
{{- end }}
{{- end }}
EOF

echo "helm-crds: wrote templates/rbac/rbac.yaml and a CRD subchart (namespace=$CRD_NAMESPACE)"
