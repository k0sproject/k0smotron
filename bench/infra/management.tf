###############################################################################
# Management cluster — control-plane nodes
#
# 3x c5.2xlarge (8 vCPU, 16 GiB)
#   - Root:      gp3 20 GB
#   - etcd data: io2 50 GB, 3000 IOPS  (predictable, no burst)
###############################################################################

locals {
  cp_common = {
    ami                         = data.aws_ami.ubuntu.id
    instance_type               = var.cp_instance_type
    subnet_id                   = aws_subnet.bench.id
    availability_zone           = var.az
    key_name                    = var.key_name
    associate_public_ip_address = true
    security_groups = [
      aws_security_group.internal.id,
      aws_security_group.external.id,
    ]
  }
}

# Primary controller (node 0) — bootstraps the cluster and serves tokens on :8888
resource "aws_instance" "cp_primary" {
  ami                         = local.cp_common.ami
  instance_type               = local.cp_common.instance_type
  subnet_id                   = local.cp_common.subnet_id
  availability_zone           = local.cp_common.availability_zone
  key_name                    = local.cp_common.key_name
  associate_public_ip_address = local.cp_common.associate_public_ip_address
  vpc_security_group_ids      = local.cp_common.security_groups

  user_data = base64encode(templatefile("${path.module}/userdata/control-plane.sh", {
    node_index     = 0
    k0s_version    = var.k0s_version
    cp0_private_ip = ""
  }))

  root_block_device {
    volume_type           = "gp3"
    volume_size           = 20
    delete_on_termination = true
    encrypted             = true
    tags                  = merge(var.tags, { Name = "k0smotron-bench-cp-0-root" })
  }

  ebs_block_device {
    device_name           = "/dev/sdb"
    volume_type           = "io2"
    volume_size           = var.cp_etcd_size
    iops                  = var.cp_etcd_iops
    delete_on_termination = true
    encrypted             = true
    tags                  = merge(var.tags, { Name = "k0smotron-bench-cp-0-etcd" })
  }

  tags = merge(var.tags, {
    Name  = "k0smotron-bench-cp-0"
    Role  = "control-plane"
    Index = "0"
  })

  lifecycle {
    ignore_changes = [ami]
  }
}

###############################################################################
# Management cluster — worker nodes
#
# 3x m5.4xlarge (16 vCPU, 64 GiB)
#   - Root: gp3 50 GB
#   HCP StatefulSet pods land on these nodes.
###############################################################################

resource "aws_instance" "worker" {
  count = 3

  ami           = data.aws_ami.ubuntu.id
  instance_type = var.worker_instance_type

  subnet_id                   = aws_subnet.bench.id
  availability_zone           = var.az
  key_name                    = var.key_name
  associate_public_ip_address = true

  vpc_security_group_ids = [
    aws_security_group.internal.id,
    aws_security_group.external.id,
  ]

  user_data = base64encode(templatefile("${path.module}/userdata/worker.sh", {
    k0s_version    = var.k0s_version
    cp0_private_ip = aws_instance.cp_primary.private_ip
  }))
  user_data_replace_on_change = true

  root_block_device {
    volume_type           = "gp3"
    volume_size           = 50
    delete_on_termination = true
    encrypted             = true
    tags                  = merge(var.tags, { Name = "k0smotron-bench-worker-${count.index}-root" })
  }

  tags = merge(var.tags, {
    Name  = "k0smotron-bench-worker-${count.index}"
    Role  = "worker"
    Index = tostring(count.index)
  })

  lifecycle {
    ignore_changes = [ami]
  }
}

###############################################################################
# Benchmark / observer node
#
# 1x t3.medium (2 vCPU, 4 GiB) — not being measured, burstable is fine
#   - Root: gp3 20 GB
#   Runs the Go benchmark binary and collects results.
###############################################################################

resource "aws_instance" "observer" {
  ami           = data.aws_ami.ubuntu.id
  instance_type = "t3.medium"

  subnet_id                   = aws_subnet.bench.id
  availability_zone           = var.az
  key_name                    = var.key_name
  associate_public_ip_address = true

  vpc_security_group_ids = [
    aws_security_group.internal.id,
    aws_security_group.external.id,
  ]

  user_data = base64encode(templatefile("${path.module}/userdata/observer.sh", {
    cp0_private_ip = aws_instance.cp_primary.private_ip
  }))

  root_block_device {
    volume_type           = "gp3"
    volume_size           = 20
    delete_on_termination = true
    encrypted             = true
    tags                  = merge(var.tags, { Name = "k0smotron-bench-observer-root" })
  }

  tags = merge(var.tags, {
    Name = "k0smotron-bench-observer"
    Role = "observer"
  })

  lifecycle {
    ignore_changes = [ami]
  }
}
