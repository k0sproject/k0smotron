variable "aws_region" {
  description = "AWS region to deploy all resources"
  type        = string
  default     = "eu-north-1"
}

variable "az" {
  description = "Availability zone for all resources — single AZ is critical for benchmark consistency (eliminates cross-AZ latency variance)"
  type        = string
  default     = "eu-north-1a"
}

variable "key_name" {
  description = "EC2 key pair name for SSH access"
  type        = string
}

variable "allowed_cidr" {
  description = "CIDR block allowed SSH (22) and kubectl (6443) access from the outside"
  type        = string
  default     = "0.0.0.0/0"
}

variable "k0s_version" {
  description = "k0s version to install on management cluster nodes"
  type        = string
  default     = "v1.31.2+k0s.0"
}

variable "postgres_password" {
  description = "Password for the PostgreSQL bench user"
  type        = string
  default     = "bench_secret_change_me"
  sensitive   = true
}

variable "mysql_password" {
  description = "Password for the MySQL bench user"
  type        = string
  default     = "bench_secret_change_me"
  sensitive   = true
}

variable "cp_instance_type" {
  description = "EC2 instance type for the k0s control-plane"
  type        = string
  default     = "c5.4xlarge"
}

variable "cp_etcd_iops" {
  description = "io2 IOPS for the control-plane etcd data disk"
  type        = number
  default     = 6000
}

variable "cp_etcd_size" {
  description = "Size in GB of the control-plane etcd data disk"
  type        = number
  default     = 100
}

variable "worker_instance_type" {
  description = "EC2 instance type for worker nodes"
  type        = string
  default     = "m5.4xlarge"
}

variable "tags" {
  description = "Tags applied to all resources"
  type        = map(string)
  default = {
    Project = "k0smotron-bench"
  }
}
