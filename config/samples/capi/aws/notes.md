# AWS Notes

These are working notes, eventually we need to get this into proper docs page...

## Pre-requisites

https://cluster-api.sigs.k8s.io/user/quick-start.html#initialization-for-common-providers

You need to use `IAM_config_access` profile. I've already created the needed IAM profiles etc on eu-north-1 on our testing account. So it might fail creating those.

## Challenges

AWSCluster.controlPlaneEndpoint is immutable. Hence k0smotron cannot patch it when it knows the k0smotron CP address. :sad:

I've opened up discussion with CAPI folks: https://kubernetes.slack.com/archives/CD6U2V71N/p1686828804383629

## Self managed infra

As `AWSCluster` creates many unnecessary resources for our use case, we can make it self managed via annotation:
```
cluster.x-k8s.io/managed-by: external
```

That though has many pre-requisites: https://cluster-api-aws.sigs.k8s.io/topics/bring-your-own-aws-infrastructure.html#prerequisites

My time ran out with subnets:
```
"failed to create AWSMachine instance: failed to run machine \"k0s-aws-test-0\" with public IP, no public subnets available"
```

*Note:* When using self managed infra, you must manually patch the `AWSCluster` status to ready:
```
kubectl patch AWSCluster k0s-aws-test --type=merge --subresource status --patch 'status: {ready: true}'
```

I believe when we have proper subnets etc. (maybe via Terraform) this should work.

