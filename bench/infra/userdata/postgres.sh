#!/bin/bash
# PostgreSQL 16 node setup
# Template variables (injected by Terraform templatefile()):
#   ${postgres_password} — bench user password
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
BENCH_DB="bench"
BENCH_USER="bench"
BENCH_PASSWORD="${postgres_password}"

###############################################################################
# 0. System prerequisites
###############################################################################
apt-get update -y
apt-get install -y curl gnupg lsb-release

###############################################################################
# 1. Mount io2 data disk at PostgreSQL's default data root
###############################################################################
for i in $(seq 1 20); do
  [ -b /dev/sdb ] || [ -b /dev/nvme1n1 ] && break
  sleep 3
done

DATA_DEVICE="/dev/sdb"
[ -b /dev/nvme1n1 ] && DATA_DEVICE="/dev/nvme1n1"

if ! blkid "$DATA_DEVICE" > /dev/null 2>&1; then
  mkfs.ext4 -F "$DATA_DEVICE"
fi

mkdir -p /var/lib/postgresql
DATA_UUID=$(blkid -s UUID -o value "$DATA_DEVICE")
echo "UUID=$DATA_UUID /var/lib/postgresql ext4 defaults,noatime 0 2" >> /etc/fstab
mount -a

###############################################################################
# 2. Install PostgreSQL 16
###############################################################################
curl -sSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor \
  -o /usr/share/keyrings/postgresql-archive-keyring.gpg

echo "deb [signed-by=/usr/share/keyrings/postgresql-archive-keyring.gpg] \
  https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
  > /etc/apt/sources.list.d/pgdg.list

apt-get update -y
apt-get install -y postgresql-16
systemctl stop postgresql
chown -R postgres:postgres /var/lib/postgresql

###############################################################################
# 3. Configure PostgreSQL
###############################################################################
PG_CONF="/etc/postgresql/16/main/postgresql.conf"

###############################################################################
# 4. Tune postgresql.conf for benchmark workload
###############################################################################
cat >> "$PG_CONF" << 'EOF'

# k0smotron benchmark tuning
max_connections            = 500
shared_buffers             = 8GB
effective_cache_size       = 24GB
maintenance_work_mem       = 512MB
work_mem                   = 16MB
wal_buffers                = 64MB
checkpoint_completion_target = 0.9
default_statistics_target  = 100
random_page_cost           = 1.1  # io2 — treat as near-SSD random cost
effective_io_concurrency   = 200
max_wal_size               = 4GB
min_wal_size               = 1GB
synchronous_commit         = on
log_min_duration_statement = 1000
EOF

###############################################################################
# 5. Allow connections from the benchmark VPC (10.0.0.0/8)
###############################################################################
PG_HBA="/etc/postgresql/16/main/pg_hba.conf"
echo "host    all    all    10.0.0.0/8    scram-sha-256" >> "$PG_HBA"

# Listen on all interfaces
sed -i "s/#listen_addresses = 'localhost'/listen_addresses = '*'/" "$PG_CONF"

###############################################################################
# 6. Start PostgreSQL and create bench database + user
###############################################################################
systemctl start postgresql

# Wait for PostgreSQL to accept connections
until pg_isready -h 127.0.0.1 -p 5432; do sleep 2; done

sudo -u postgres psql -c "CREATE USER $BENCH_USER WITH PASSWORD '$BENCH_PASSWORD';"
sudo -u postgres psql -c "CREATE DATABASE $BENCH_DB OWNER $BENCH_USER;"
sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE $BENCH_DB TO $BENCH_USER;"

echo "PostgreSQL 16 setup complete."
