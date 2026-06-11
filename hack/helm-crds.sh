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
# CRDs go in the k0smotron-crds subchart's crds/ directory: crds/ is installed
# but never rendered into the manifest, so the full-fat CRDs (descriptions kept)
# are stored only once and fit Helm's 1 MiB release-Secret limit. crds/ is not
# templated, so the conversion-webhook and cert-manager namespaces are baked to
# $CRD_NAMESPACE; the parent NOTES guard enforces installing into that namespace.
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

# --- CRDs (crds subchart, full descriptions, baked namespace) ----------------
# One file per CRD: with full descriptions the combined set is >5 MiB, Helm's
# per-file limit. Not templated, so bake $CRD_NAMESPACE. Use with(select(...);...)
# not a guarded assignment: a deep path on the left of `=` makes yq materialise
# the intermediate nodes, injecting a half-built conversion block (no strategy)
# into CRDs that have none - the apiserver rejects that. yq's --split-exp keeps
# the dotted CRD name verbatim (no extension), so just append .yaml.
rm -rf "$CHART/templates/crd" "$SUB/crds"; mkdir -p "$SUB/crds/.split"
"$YQ" eval '
  select(.kind == "CustomResourceDefinition")
  | with(select(.spec.conversion.strategy == "Webhook"); .spec.conversion.webhook.clientConfig.service.namespace = "'"$CRD_NAMESPACE"'")
  | with(select(.metadata.annotations."cert-manager.io/inject-ca-from" != null); .metadata.annotations."cert-manager.io/inject-ca-from" = "'"$CRD_NAMESPACE"'/serving-cert")
' "$rendered" \
  | "$YQ" eval --no-doc --split-exp "\"$SUB/crds/.split/\" + .metadata.name" -
for f in "$SUB/crds/.split"/*; do
  mv "$f" "$SUB/crds/$(basename "$f").yaml"
done
rmdir "$SUB/crds/.split"

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
