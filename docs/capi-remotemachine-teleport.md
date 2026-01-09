# Using Remote Machine with Teleport

This guide will walk you through the process of preparing your management cluster to use the Remote Machine provider with [Teleport](https://goteleport.com/).

## Prerequisites

To use the Remote Machine provider with Teleport, you need to have a Teleport cluster up and running. You can follow the [official guide](https://goteleport.com/docs/quickstart/) to get started.
Also, you need to have `tctl` installed on your desktop.

## Creating Service Account

First, you need to create a service account for Teleport user. It requires an admin access to the secrets since it creates and regularly updates a secret with the Teleport credentials.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: k0smotron-teleport-bot
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: secrets-admin
  namespace: default
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: k0smotron-teleport-bot-secrets-admin
  namespace: default
subjects:
  - kind: ServiceAccount
    name: teleport-bot
roleRef:
  kind: Role
  name:  secrets-admin
  apiGroup: rbac.authorization.k8s.io
```

## Creating Teleport Bot User

Next, you need to create a Teleport Bot user for the service account. Since we use a `kubernetes` auth method for Teleport, first we need to figure out the cluster's JWKS.
It will be used by the Teleport Cluster to verify the JWT tokens.

```bash
$ kubectl proxy -p 8080
$ curl http://localhost:8080/openid/v1/jwks
{"keys":[<REDACTED>]}
```

Create `teleport-role.yaml`. Don't forget to specify correct permissions for the role, see [Teleport documentation](https://goteleport.com/docs/access-controls/guides/role-templates/).

```yaml
---
kind: role
version: v5
metadata:
  name: k0smotron-bot
spec:
  allow: {}
  deny: {}
  options: {}
```

```bash
$ tctl create -f teleport-role.yaml
```

Now, we can create a Teleport Bot user. Create `k0smotron-bot-token.yaml`

```yaml
kind: token
version: v2
metadata:
  # name will be specified in the `tbot` to use this token
  name: k0smotron-bot
spec:
  roles: [Bot]
  # bot_name will match the name of the bot created later in this guide.
  bot_name: k0smotron
  join_method: kubernetes
  kubernetes:
    type: static_jwks
    static_jwks:
      jwks: |
        {"keys":[<REDACTED>]}
    allow:
    - service_account: "default:k0smotron-teleport-bot"
```

```bash
$ tctl create -f k0smotron-bot-token.yaml
```

Finally, create a bot user:

```bash
$ tctl bots add k0smotron --token k0smotron-bot --roles k0smotron-bot
```

## Creating a deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: k0smotron-tbot-config
  namespace: default
data:
  tbot.yaml: |
    version: v2
    onboarding:
      join_method: kubernetes
      token: k0smotron-bot # name of the join token
    storage:
      type: memory
    # ensure this is configured to the address of your Teleport Proxy or
    # Auth Server. Prefer the address of the Teleport Proxy.
    auth_server: teleport.example.com:443
    # in outputs we specify the destination of the identity. In our case we will put it into the kubernetes secret.
    outputs:
    - type: identity
      destination:
        type: kubernetes_secret
        name: k0smotron-bot-identity
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k0smotron-tbot
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: k0smotron-tbot
  template:
    metadata:
      labels:
        app.kubernetes.io/name: k0smotron-tbot
    spec:
      containers:
        - name: tbot
          image: public.ecr.aws/gravitational/teleport:14.3.3
          command:
            - tbot
          args:
            - start
            - -c
            - /config/tbot.yaml
          env:
            # POD_NAMESPACE is required for the kubernetes_secret` destination type to work correctly.
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            # KUBERNETES_TOKEN_PATH specifies the path to the service account JWT to use for joining.
            - name: KUBERNETES_TOKEN_PATH
              value: /var/run/secrets/tokens/join-token
          volumeMounts:
            - mountPath: /config
              name: config
            - mountPath: /var/run/secrets/tokens
              name: join-sa-token
      serviceAccountName: tbot
      volumes:
        - name: config
          configMap:
            name: k0smotron-tbot-config
        - name: join-sa-token
          projected:
            sources:
              - serviceAccountToken:
                  path: join-token
                  expirationSeconds: 600
                  # `teleport.example.com` must be replaced with the _name_ of your Teleport cluster.
                  audience: teleport.example.com
```

More information about Teleport's Machine ID and how to set it up with Kubernetes can be found [here](https://goteleport.com/docs/machine-id/deployment/kubernetes/).
Also, you can use [this example](https://github.com/k0sproject/k0smotron/blob/{{{ extra.k0smotron_version }}}/config/samples/capi/remotemachine/remotemachine-teleport-access.yaml) as a reference.

## Create a RemoteMachine

Now we can create a RemoteMachine that will run the pod to setup the machine.

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: remote-test-0
  namespace: default
spec:
  address: <server-name>
  useSudo: false
  provisionJob:
    sshCommand: "tsh -i /identity-output/identity --proxy teleport.example.com:443 ssh"
    scpCommand: "tsh -i /identity-output/identity --proxy teleport.example.com:443 scp"
    jobSpecTemplate:
      spec:
        containers:
          - name: teleport
            image: public.ecr.aws/gravitational/teleport:14.3.3
            volumeMounts:
             - name: identity-output
               mountPath: /identity-output
        volumes:
          - name: identity-output
            secret:
              secretName: identity-output
        restartPolicy: Never
```
