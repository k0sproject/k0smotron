#!/usr/bin/env bash
set -euo pipefail

MODE="${1:---fix}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
BOILERPLATE_FILE="${REPO_ROOT}/hack/boilerplate.go.txt"
CURRENT_YEAR="$(date +%Y)"

if [[ ! -f "${BOILERPLATE_FILE}" ]]; then
  echo "boilerplate file not found: ${BOILERPLATE_FILE}" >&2
  exit 1
fi

if [[ "${MODE}" != "--fix" && "${MODE}" != "--check" ]]; then
  echo "usage: $0 [--fix|--check]" >&2
  exit 1
fi

has_apache_header() {
  local file="$1"
  # Hopefully we don't have any files with more than 40 lines before the license header :)
  head -n 40 "${file}" | grep -q "Licensed under the Apache License, Version 2.0"
}

prepend_boilerplate() {
  local file="$1"
  local tmp
  local prefix_end=0
  local line_no=0
  local line
  local seen_tags=false
  tmp="$(mktemp)"

  # Find where header should be inserted:
  # after a top build-tag block, otherwise at file start.
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line_no=$((line_no + 1))

    if [[ "${line}" =~ ^[[:space:]]*$ ]]; then
      if [[ "${seen_tags}" == true ]]; then
        prefix_end="${line_no}"
      fi
      continue
    fi

    if [[ "${line}" =~ ^//go:build[[:space:]] || "${line}" =~ ^//\ \+build[[:space:]] ]]; then
      seen_tags=true
      prefix_end="${line_no}"
      continue
    fi

    break
  done < "${file}"

  if [[ "${seen_tags}" == true ]]; then
    {
      head -n "${prefix_end}" "${file}"
      sed -E "s/^Copyright [0-9]{4}\.$/Copyright ${CURRENT_YEAR}./" "${BOILERPLATE_FILE}"
      printf "\n\n"
      tail -n "+$((prefix_end + 1))" "${file}"
    } > "${tmp}"
  else
    {
      sed -E "s/^Copyright [0-9]{4}\.$/Copyright ${CURRENT_YEAR}./" "${BOILERPLATE_FILE}"
      printf "\n\n"
      cat "${file}"
    } > "${tmp}"
  fi

  mv "${tmp}" "${file}"
}

missing=0
updated=0

while IFS= read -r file; do
  if has_apache_header "${file}"; then
    continue
  fi

  missing=$((missing + 1))
  if [[ "${MODE}" == "--fix" ]]; then
    prepend_boilerplate "${file}"
    updated=$((updated + 1))
    echo "added header: ${file}"
  else
    echo "missing header: ${file}"
  fi
done < <(cd "${REPO_ROOT}" && git ls-files '*.go')

if [[ "${MODE}" == "--check" ]]; then
  if ((missing > 0)); then
    echo "found ${missing} go files without Apache header" >&2
    exit 1
  fi
  echo "all go files have Apache headers"
else
  echo "updated ${updated} files"
fi
