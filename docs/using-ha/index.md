# High Availability Best Practices for Postgres Clusters

The advice on this page provides best practices for configuring and maintaining highly available Postgres clusters using the pgEdge Control Plane.

High availability (HA) ensures your database remains accessible even when individual components fail. A well-designed HA Postgres cluster minimizes downtime, prevents data loss, and maintains consistent performance across failure scenarios.

Building a highly available Postgres cluster requires careful planning, proper configuration, and ongoing maintenance. Control Plane simplifies this process.

**Use an Odd Number of Nodes**

For clusters using quorum-based replication or consensus, always deploy an odd number of nodes (3, 5, or 7) to ensure proper quorum and avoid split-brain scenarios.  For example:

- **3 nodes**: Suitable for most production workloads, tolerates 1 node failure.
- **5 nodes**: For critical workloads, tolerates 2 node failures.
- **7+ nodes**: Rarely needed, adds complexity with diminishing returns.

Avoid 2-node or 4-node clusters as they cannot maintain quorum during single or dual node failures respectively.

**Geographic Distribution**

Spread your nodes across multiple availability zones or regions to protect against localized failures:

- Distribute cluster nodes across at least three different availability zones.
- For disaster recovery, place nodes in different geographic regions.
- Balance latency requirements against fault tolerance needs.
- Consider network bandwidth and latency between regions when configuring synchronous replication.

**Ensure Reliable Network Connectivity**

- Use private networks between database nodes when possible.
- Implement redundant network paths to prevent single points of failure.
- Monitor network latency and packet loss between nodes.
- Configure appropriate timeouts for replication connections.

**Port and Firewall Configuration**

- Restrict open ports to only those necessary.
- Use security groups or firewall rules to limit exposure.
- Ensure replication ports are accessible between all cluster nodes.
- Monitor and logconnection attempts for security auditing.

**Set Up Monitoring and Alerting**

- Monitor replication lag on all standby nodes continuously
- Alert when lag exceeds acceptable thresholds (e.g., 10 seconds)
- Track lag trends to identify performance degradation
- Use metrics like `pg_stat_replication.replay_lag` to measure delay

**Configure Automatic Failover for High Availability**

Automatic failover ensures minimal downtime during primary node failures:

- Use a proven failover solution.
- Set conservative failover timers to avoid false positives (30-60 seconds).
- Test failover procedures in a non-production environment.
- Document and automate failover verification steps.

**Implement a Comprehensive Backup Strategy**

A robust backup strategy is essential even with replication:

- **Full backups**: Weekly or daily depending on data volume.
- **Incremental backups**: Continuous WAL archiving for point-in-time recovery.
- **Backup verification**: Regularly test restores to ensure backups are valid.
- **Retention policy**: Keep backups for at least 30 days, longer for compliance.

**Backup Storage**

- Store backups in a location separate from production data.
- Use object storage (S3, GCS, Azure Blob) for durability.
- Encrypt backups at rest and in transit.
- Replicate backups across multiple regions for disaster recovery.

**Enable WAL Archiving for PITR**

Configure continuous WAL archiving to enable recovery to any point in time:

```ini
wal_level = replica
archive_mode = on
archive_command = 'pg_receivewal -D /backup/wal_archive'
archive_timeout = 300
```

**Implement Connection Pooling**

Use connection pooling to manage database connections efficiently:

- Deploy pgBouncer or a similar pooler between applications and databases.
- Configure pool sizes based on database resources (typically max_connections / 4).
- Use transaction pooling for better connection reuse.
- Monitor pool utilization and adjust as needed.

**Optimizing the postgresql.conf File for HA**

```ini
# Replication settings
wal_level = replica
max_wal_senders = 10
max_replication_slots = 10
hot_standby = on
hot_standby_feedback = on

# Performance tuning
shared_buffers = 25% of RAM
effective_cache_size = 50-75% of RAM
maintenance_work_mem = 2GB
checkpoint_completion_target = 0.9
wal_compression = on

# Monitoring and logging
log_destination = 'csvlog'
logging_collector = on
log_replication_commands = on
log_min_duration_statement = 1000
track_io_timing = on
```

**Establish a Regular Maintenance Schedule**

- **Vacuum**: Run VACUUM ANALYZE regularly (or enable autovacuum).
- **Reindex**: Rebuild indexes periodically to prevent bloat.
- **Statistics update**: Ensure table statistics are current for query planning.
- **Log rotation**: Archive and rotate logs to prevent disk space issues.


