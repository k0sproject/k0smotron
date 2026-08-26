#!/usr/bin/env python3
#
# Copyright 2026.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Render the jinja examples in the docs the way cloud init would.

A template can look fine and still emit invalid yaml, so parse the result.
"""

import pathlib
import re
import sys

import jinja2
import yaml

# cloud init renders user data with trim_blocks enabled and lstrip_blocks off.
# See cloudinit/handlers/jinja_template.py upstream.
ENV = jinja2.Environment(trim_blocks=True, undefined=jinja2.StrictUndefined)

BLOCK_RE = re.compile(r"^```jinja\n(.*?)^```$", re.MULTILINE | re.DOTALL)

# Names come from the VarK0s constants in internal/provisioner/cloudinit.go.
COMMAND_VARS = {
    "k0smotron_k0sDownloadCommands": "curl -sSf https://get.k0s.sh | sh",
    "k0smotron_k0sInstallCommand": "k0s install worker --token-file /etc/k0s.token",
    "k0smotron_k0sStartCommand": "k0s start",
}

# Contents that have broken a documented template before, or plausibly could.
CONTENTS = {
    "plain": "hello\n",
    "multiline": "line1\nline2\n",
    "leading spaces": "  extraArgs:\n    foo: bar\n",
    "leading tab": "\thi\n",
    "leading newline": "\nfoo\n",
    "quotes and backslash": 'a"b\\c\n',
    "blank lines": "a\n\n\nb\n",
    "non ascii": "café München\n",
    "no trailing newline": "Zm9vYmFy",
    "yaml lookalike": "- item\nkey: value\n",
}


def files_var(content):
    """Mirror what writeFilesVars emits, with every key always present."""
    return [
        {
            "path": "/etc/k0smotron-example.conf",
            "permissions": "0640",
            "owner": "test:test",
            "content": content,
        }
    ]


def check_block(source, doc, index):
    failures = []
    uses_files = "k0smotron_files" in source

    for label, content in CONTENTS.items() if uses_files else {"none": ""}.items():
        variables = dict(COMMAND_VARS, k0smotron_files=files_var(content))

        try:
            rendered = ENV.from_string(source).render(**variables)
        except jinja2.TemplateError as err:
            failures.append(f"{label}: template error {err}")
            continue

        try:
            parsed = yaml.safe_load(rendered)
        except yaml.YAMLError as err:
            first = str(err).split("\n")[0]
            failures.append(f"{label}: rendered to invalid yaml, {first}")
            continue

        if not uses_files:
            continue

        written = [f for f in parsed.get("write_files", []) if f.get("path", "").startswith("/etc/k0smotron")]
        if len(written) != 1:
            failures.append(f"{label}: expected one generated file, found {len(written)}")
            continue

        # Content must survive exactly. An example is free to forward only
        # some of the other fields, but must not corrupt the ones it does.
        got = written[0]
        if got.get("content") != content:
            failures.append(f"{label}: content changed, {got.get('content')!r} != {content!r}")

        expected = {"owner": "test:test", "permissions": "0640"}
        for key, want in expected.items():
            if key in got and got[key] != want:
                failures.append(f"{label}: {key} changed, {got[key]!r} != {want!r}")

    for failure in failures:
        print(f"{doc} block {index}: {failure}", file=sys.stderr)

    return not failures


def main():
    root = pathlib.Path(__file__).resolve().parent.parent
    docs = sorted((root / "docs").rglob("*.md"))

    blocks = 0
    ok = True
    for doc in docs:
        text = doc.read_text(encoding="utf-8")
        for index, match in enumerate(BLOCK_RE.finditer(text), start=1):
            blocks += 1
            if not check_block(match.group(1), doc.relative_to(root), index):
                ok = False

    if not ok:
        print(f"checked {blocks} jinja examples, some are broken", file=sys.stderr)
        return 1

    print(f"checked {blocks} jinja examples, all render to valid yaml")
    return 0


if __name__ == "__main__":
    sys.exit(main())
