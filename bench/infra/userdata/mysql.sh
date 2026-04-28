#!/bin/bash
# MySQL 8 node setup
# Template variables (injected by Terraform templatefile()):
#   ${mysql_password} — bench user password
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
BENCH_DB="bench"
BENCH_USER="bench"
BENCH_PASSWORD="${mysql_password}"

###############################################################################
# 0. System prerequisites
###############################################################################
apt-get update -y
apt-get install -y curl gnupg

###############################################################################
# 1. Wait for the io2 data disk to attach
###############################################################################
for i in $(seq 1 20); do
  [ -b /dev/sdb ] || [ -b /dev/nvme1n1 ] && break
  sleep 3
done

DATA_DEVICE="/dev/sdb"
[ -b /dev/nvme1n1 ] && DATA_DEVICE="/dev/nvme1n1"

###############################################################################
# 2. Install MySQL 8
###############################################################################
apt-get install -y mysql-server

###############################################################################
# 3. Configure MySQL
###############################################################################
systemctl stop mysql

###############################################################################
# 4. Create / update MySQL config with benchmark tuning
###############################################################################
# Prefix with zzz so these settings override the packaged mysqld.cnf defaults.
cat > /etc/mysql/mysql.conf.d/zzz-bench.cnf << EOF
[mysqld]
# Network
bind-address           = 0.0.0.0
port                   = 3306
max_connections        = 500
max_allowed_packet     = 256M

# InnoDB tuning for benchmark workload
innodb_buffer_pool_size        = 20G
innodb_buffer_pool_instances   = 4
innodb_log_file_size           = 1G
innodb_log_buffer_size         = 64M
innodb_flush_log_at_trx_commit = 1
innodb_flush_method            = O_DIRECT
innodb_io_capacity             = 5000   # matches io2 IOPS provisioning
innodb_io_capacity_max         = 5000
innodb_read_io_threads         = 8
innodb_write_io_threads        = 8

# Query cache is deprecated in MySQL 8, skip it
# General
character-set-server  = utf8mb4
collation-server      = utf8mb4_unicode_ci
default_storage_engine = InnoDB
slow_query_log        = 1
long_query_time       = 1
EOF

###############################################################################
# 5. Start MySQL and create bench database + user
###############################################################################
systemctl restart mysql

# Wait for MySQL to be ready
for i in $(seq 1 60); do
  mysqladmin ping -h 127.0.0.1 --silent && break
  if [ "$i" -eq 60 ]; then
    systemctl status mysql --no-pager -l || true
    journalctl -u mysql --no-pager -n 100 || true
    exit 1
  fi
  sleep 2
done

PRIVATE_IP=$(hostname -I | awk '{print $1}')
for i in $(seq 1 30); do
  timeout 1 bash -c "</dev/tcp/$PRIVATE_IP/3306" >/dev/null 2>&1 && break
  if [ "$i" -eq 30 ]; then
    ss -ltnp || true
    mysqld --verbose --help 2>/dev/null | grep -E '^(bind-address|port)[[:space:]]' || true
    exit 1
  fi
  sleep 2
done

mysql -u root << SQL
CREATE DATABASE IF NOT EXISTS \`$BENCH_DB\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '$BENCH_USER'@'10.0.0.0/255.0.0.0' IDENTIFIED BY '$BENCH_PASSWORD';
CREATE USER IF NOT EXISTS '$BENCH_USER'@'%' IDENTIFIED BY '$BENCH_PASSWORD';
GRANT ALL PRIVILEGES ON \`$BENCH_DB\`.* TO '$BENCH_USER'@'10.0.0.0/255.0.0.0';
GRANT ALL PRIVILEGES ON \`$BENCH_DB\`.* TO '$BENCH_USER'@'%';
FLUSH PRIVILEGES;
SQL

echo "MySQL 8 setup complete."
