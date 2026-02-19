# Cloud-init customization

## Using custom user-data

k0smotron’s CAPI bootstrap providers for both controllers and workers allow you to append your own cloud-init content to the generated user data via the `spec.customUserDataRef` field.

This is useful when you need to:

- Run additional provisioning logic (install agents, tweak OS, configure networking).
- Ship extra cloud-init modules beyond what k0smotron emits.
- Use templated variables to reuse the k0smotron-generated commands in your own snippets.

#### How it works

- k0smotron generates the base cloud-init for the node (files, commands, etc.).
- If `spec.customUserDataRef` is set on the bootstrap object, its content is appended to the user data.
- On bootstrap, cloud-init [merges](https://cloudinit.readthedocs.io/en/latest/reference/merging.html) user-data.

Example (Controller):

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: cp-custom-userdata
stringData:
  customUserData: |
    #cloud-config
    packages:
      - htop
    runcmd:
      - echo "hello from custom controller cloud-init"
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: my-cp
spec:
  replicas: 1
  k0sConfigSpec:
    customUserDataRef:
      secretRef:
        name: cp-custom-userdata
        key: customUserData
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerMachineTemplate
      name: my-cp-tpl
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: my-cp-tpl
spec:
  template:
    spec: {}
```

Example (WorkerConfigTemplate):

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: worker-custom-userdata
data:
  customUserData: |
    #cloud-config
    write_files:
      - path: /etc/motd
        permissions: "0644"
        content: |
          Welcome from custom worker cloud-init
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: my-worker-template
spec:
  template:
    spec:
      customUserDataRef:
        configMapRef:
          name: worker-custom-userdata
          key: customUserData
```

Your custom content should be valid cloud-init. Include a `#cloud-config` header at the top of the snippet.

## Using jinja templating

k0smotron supports Jinja templating in your customUserData. You can use Jinja syntax to include variables and control structures.

!!! note 
    Read more about instance data templating in the [cloud-init documentation](https://cloudinit.readthedocs.io/en/latest/explanation/instancedata.html).

## Using CloudInitVars feature gate

!!! warning "Important"
    Use `CloudInitVars` feature gate in case you really need to and if you absolutely understand the implications. It is not recommended for general use.
    Most likely, you don't need it and should stick to the regular customUserData with pre/postStartCommands approach.

When the `CloudInitVars` feature gate is enabled, k0smotron exposes generated commands and files as Jinja variables instead of putting them into `runcmd` and `files` sections. You can embed into your customUserData. 
The variables include:

- `{{ k0smotron_k0sDownloadCommands }}` — shell line with all download steps chained by &&.
- `{{ k0smotron_k0sInstallCommand }}` — the k0s install command (controller/worker specific).
- `{{ k0smotron_k0sStartCommand }}` — the command that starts k0s.
- `{{ k0smotron_files }}` — a list of file objects with fields: path, content, permissions.

Example customUserData using variables:

```jinja
runcmd:
  - {{ k0smotron_k0sDownloadCommands }}
  - echo "About to install" && {{ k0smotron_k0sInstallCommand }}
  - {{ k0smotron_k0sStartCommand }}
  
write_files:
  {% for f in k0smotron_files %}
  - path: {{ f.path }}
    permissions: "{{ f.permissions }}"
    content: |
      {{ f.content | indent(6) }}
  {% endfor %}
  - path: /my/custom/file.txt
    permissions: "0644"
    content: |
      Welcome from custom cloud-init with variables
```

Another approach is to use the variables in a completely custom script:

```jinja
runcmd:
  - echo -n "custom" > /root/custom
  - /root/cloud-init.sh
  
write_files:
  - path: /root/cloud-init.sh
    content: |
      #!/usr/bin/bash
      set -euo pipefail
  
      {{ k0smotron_k0sDownloadCommands }}
      {{ k0smotron_k0sInstallCommand }}
      {{ k0smotron_k0sStartCommand }}
    permissions: "0755"
  {% for f in k0smotron_files %}
  - path: {{ f.path }}
    content: |
      {{ f.content | indent(6) }}
    permissions: "{{ f.permissions }}"
  {% endfor %}
  - path: /root/my-extra-file
    content: test
    permissions: "0600"
```

You don't need to start the document with `## template: jinja` or `#cloud-init`, k0smotron does it.

#### Enabling the feature gate

You can enable the `CloudInitVars` feature gate by:

- Adding the `--feature-gates=CloudInitVars=true` flag to the k0smotron bootstrap provider args.
- Setting the `K0SMOTRON_FEATURE_GATES` environment variable to k0smotron controller manager deployment. For example, if you are using the k0smotron Helm chart, you can set it like this:

  ```yaml
  env:
  - name: K0SMOTRON_FEATURE_GATES
    value: "CloudInitVars=true"
  ```
  
- Setting the `K0SMOTRON_FEATURE_GATES` environment variable for `clusterctl`. For example:

  ```bash
  export K0SMOTRON_FEATURE_GATES="CloudInitVars=true"
  clusterctl init --bootstrap k0sproject-k0smotron
  ```
