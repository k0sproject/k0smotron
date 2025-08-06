# Using Remote Machine with Okta ASA

Okta Advanced Server Access (ASA) is a solution that enables secure access to SSH servers. This guide will walk you through the process of preparing your management cluster to use the Remote Machine provider with Okta ASA.

## Prerequisites

- Setup and integrate Okta Advanced Server Access App to your Okta account
- [Install the Okta ASA agent](https://help.okta.com/asa/en-us/content/topics/adv_server_access/docs/install-agent.htm?cshid=csh-asa-install-server) on your target SSH server.
- Create a [service user](https://help.okta.com/asa/en-us/content/topics/adv_server_access/docs/service-users.htm) and an API key.

## Creating an image

Here is an example of a Dockerfile that installs the Okta ASA server and client agents that will user for the Remote Machine provider:

```Dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
      curl \
      gpg \
      ssh

# Obtain the Okta ASA agent key and add the repository
RUN curl -fsSL https://dist.scaleft.com/GPG-KEY-OktaPAM-2023 | gpg --dearmor | tee /usr/share/keyrings/oktapam-2023-archive-keyring.gpg > /dev/null \
    && echo "deb [signed-by=/usr/share/keyrings/oktapam-2023-archive-keyring.gpg] https://dist.scaleft.com/repos/deb jammy okta" | tee /etc/apt/sources.list.d/oktapam-stable.list \

# Install the Okta ASA agents
RUN apt-get update && apt-get install -y \
      scaleft-client-tools \
      # Mute the error about missing systemd from the post-install script
      scaleft-server-tools | true \

# Configure the ssh client to use the proxy
RUN mkdir -p ~/.ssh/ && sft ssh-config >> ~/.ssh/config

# Add the entrypoint script.
# The script starts the Okta ASA server agent, waits a bit, and then runs the command passed to the container.
RUN echo '#!/bin/bash \n\
sftd 2>1 & \n\
sleep 10 \n\
exec $@ ' >> /entrypoint.sh && chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
```

## Server enrollment

Okta ASA agent needs to be enrolled to the Okta ASA service. To do that, first we need to run `sftd` once with the enrollment token.
The token can be [obtained](https://help.okta.com/asa/en-us/content/topics/adv_server_access/docs/server-enroll-token.htm) from the Okta ASA console.
In this example we put the token to the Kubernetes secret:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: okta-asa-enrollment-token
data:
  enrollment.token: <REDACTED>
```

## Create sft and sftd configs

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: okta-asa-config
  namespace: default
data:
  sftd.yaml: |
    CanonicalName: "k0smotron-job-demo-runner"
  sft.conf: |
    # Allow authentication as a Service User
    section "service_auth" {
      enable = true
    }
```

## Create a PersistentVolumeClaim

We will use a PVC to store the Okta ASA agent state. This is required for the agent to work properly.

```yaml
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: okta-asa-demo-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 200Mi
```

## Create a pod to enroll the runner

Run the pod with the enrollment token and the PVC attached. The pod will enroll the runner to the Okta ASA service and then sleep forever.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: okta-asa-demo-pod
  namespace: default
spec:
  containers:
    - name: okta-asa
      image: makhov/okta-asa-demo:latest
      args:
        - sleep
        - infinity
      env:
      volumeMounts:
        - name: config
          mountPath: /etc/sft/sftd.yaml
          subPath: sftd.yaml
        - name: sftd-lib
          mountPath: /var/lib/sftd
        - name: enrollment-token
          mountPath: /var/lib/sftd/enrollment.token
          subPath: enrollment.token
  volumes:
    - name: config
      configMap:
        name: okta-asa-config
    - name: enrollment-token
      secret:
        secretName: okta-asa-enrollment-token
    - name: sftd-lib
      persistentVolumeClaim:
        claimName: okta-asa-demo-pvc
```

Once the `k0smotron-job-demo-runner` appears in the Okta ASA console, we can remove the pod, enrollment token and start using the runner.

```shell
$ kubectl delete pod okta-asa-demo-pod
$ kubectl delete secret okta-asa-enrollment-token
```

The enrolment process should be done only once. The runner will be available in the Okta ASA console and can be used for the RemoteMachine provider.

## Create a RemoteMachine

Now we can create a RemoteMachine that will run the pod to setup the machine.

```yaml
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: remote-test-0
  namespace: default
spec:
  address: <server-name>
  useSudo: true
  provisionJob:
    sshCommand: "ssh"
    scpCommand: "scp"
    jobSpecTemplate:
      spec:
        containers:
          - name: okta-asa
            image: makhov/okta-asa-demo:latest
            imagePullPolicy: Always
            volumeMounts:
              - name: config
                mountPath: /etc/sft/sftd.yaml
                subPath: sftd.yaml
              - name: config
                mountPath: /root/.config/ScaleFT/sft.conf
                subPath: sft.conf
              - name: sftd-lib
                mountPath: /var/lib/sftd
        volumes:
          - name: config
            configMap:
              name: okta-asa-config
          - name: sftd-lib
            persistentVolumeClaim:
              claimName: okta-asa-demo-pvc
        restartPolicy: Never
```
