# kindest/node already ships everything kubeadm needs (containerd, kubeadm,
# kubelet, kubectl, systemd, correct cgroup driver, preloaded images) and is
# the same image Cluster API's own docker provider (and kind itself) run in
# production CI every day. We only add what a pre-provisioned bare-metal host
# would already have: an SSH daemon so k0smotron's RemoteMachine controller
# can reach it.
ARG NODE_IMAGE=kindest/node:v1.31.6

FROM $NODE_IMAGE

RUN apt-get update && apt-get install -y --no-install-recommends openssh-server \
  && apt-get clean && rm -rf /var/lib/apt/lists/* \
  && ssh-keygen -A \
  && mkdir -p /root/.ssh && chmod 700 /root/.ssh \
  # unlock the root account (locked by default) and allow key-based root login,
  # same idea as footloose-alpine's shadow tweak, just for a systemd/PAM base
  && sed -i 's/^root:[^:]*:/root::/' /etc/shadow \
  && sed -i -e 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
