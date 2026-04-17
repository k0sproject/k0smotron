###############################################################################
# Outputs
###############################################################################

output "control_plane_ips" {
  description = "Public IP addresses of the k0s control-plane node"
  value       = concat([aws_instance.cp_primary.public_ip])
}

output "worker_ips" {
  description = "Public IP addresses of the 3 k0s worker nodes"
  value       = aws_instance.worker[*].public_ip
}

output "observer_ip" {
  description = "Public IP address of the benchmark/observer node"
  value       = aws_instance.observer.public_ip
}

output "postgres_ip" {
  description = "Private IP of the PostgreSQL node (accessible only within the VPC)"
  value       = aws_instance.postgres.private_ip
}

output "mysql_ip" {
  description = "Private IP of the MySQL node (accessible only within the VPC)"
  value       = aws_instance.mysql.private_ip
}

output "bench_env" {
  description = "Export these environment variables on the observer node before running benchmarks"
  sensitive   = true
  value       = <<-EOT
    # Run these on the observer node (or wherever you execute the benchmark binary)

    export KUBECONFIG=/home/ubuntu/.kube/config   # populated by observer.sh

    export BENCH_POSTGRES_URL="postgres://bench:${var.postgres_password}@${aws_instance.postgres.private_ip}:5432/bench"
    export BENCH_MYSQL_URL="mysql://bench:${var.mysql_password}@tcp(${aws_instance.mysql.private_ip}:3306)/bench"
    export BENCH_WORKER_EXTERNAL_ADDRESSES="${join(",", aws_instance.worker[*].public_ip)}"
  EOT
}

output "ssh_commands" {
  description = "SSH convenience commands"
  value = {
    observer = "ssh ubuntu@${aws_instance.observer.public_ip}"
    cp0      = "ssh ubuntu@${aws_instance.cp_primary.public_ip}"
  }
}

output "destroy_reminder" {
  description = "IMPORTANT — cost/cleanup reminder"
  value       = <<-EOT
    All EBS volumes (root + data disks) are configured with delete_on_termination = true.
    Running 'terraform destroy' will permanently delete:
      - 3 c5.2xlarge control-plane instances  (+ root gp3 + etcd io2 volumes each)
      - 3 m5.4xlarge worker instances          (+ root gp3 volumes each)
      - 1 t3.medium  observer instance         (+ root gp3 volume)
      - 1 r6i.xlarge PostgreSQL instance       (+ root gp3 + data io2 volumes)
      - 1 r6i.xlarge MySQL instance            (+ root gp3 + data io2 volumes)
    Make sure to retrieve any benchmark results BEFORE running terraform destroy.
  EOT
}
