# Disaster Recovery

The Control Plane provides disaster recovery procedures for different failure scenarios. Choose the appropriate guide based on your situation:

* **[Partial Failure Recovery (Quorum Intact)](partial-recovery.md)** - Use when quorum remains intact but one or more hosts are lost. The Control Plane API remains accessible throughout recovery.

* **[Complete Failure Recovery (No Quorum)](full-recovery.md)** - Use when etcd quorum, Docker Swarm quorum, or both are lost. Covers etcd snapshot restore, Docker Swarm re-initialization, and combined etcd + Swarm recovery scenarios.

!!! warning

    Before attempting any recovery procedure, ensure you have recent backups of your Control Plane data volume.
