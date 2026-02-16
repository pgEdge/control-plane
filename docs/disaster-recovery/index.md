# Disaster Recovery

The Control Plane provides disaster recovery procedures for different failure scenarios. Choose the appropriate guide based on your situation:

* **[Quorum Loss Recovery](full-recovery.md)** - Use when etcd quorum is lost (all or majority of server-mode hosts are offline). This is the most critical scenario requiring immediate recovery.

* **[Partial Recovery](partial-recovery.md)** - Use when quorum remains intact but one or more hosts are lost. This scenario is simpler as the Control Plane API remains accessible.

!!! warning

    Before attempting any recovery procedure, ensure you have recent backups of your Control Plane data volume.
