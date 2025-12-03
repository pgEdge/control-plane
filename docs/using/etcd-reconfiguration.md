# Etcd Mode Reconfiguration


This guide explains how to change a Control Plane host's etcd mode after cluster initialization.


## Overview


The Control Plane supports two etcd modes:


- **Server mode**: Runs an embedded etcd server and participates as a voting member
- **Client mode**: Connects to the etcd cluster as a client only


**Recommended topology:**
- 1-3 hosts: All should be etcd servers
- 4-7 hosts: 3 etcd servers, rest as clients
- 8+ hosts: 5 etcd servers, rest as clients


!!! warning "Maintain Odd Numbers"
   Etcd requires an **odd number** of servers (3 or 5) for proper quorum.


## How It Works


Etcd mode reconfiguration is **fully automatic**:


1. Stop the container
2. Update `PGEDGE_ETCD_MODE` environment variable
3. Restart the container
4. The system automatically handles all cluster operations


**What happens automatically:**
- **Client→Server**: Discovers cluster, obtains credentials, joins as voting member
- **Server→Client**: Removes itself from membership, transitions to client mode


No manual API calls or configuration needed!


## Procedures


### Promoting a Client to Server (Example - host-4)


```bash
# 1. Stop the container
docker stop control-plane-host-4


# 2. Update docker-compose.yaml environment:
PGEDGE_ETCD_MODE: server  # was: client


# 3. Restart
docker-compose up -d host-4


# 4. Verify (check logs)
docker logs control-plane-host-4
```


### Demoting a Server to Client (Example - host-4)


!!! warning "Quorum Check"
   Ensure at least 2 other healthy servers remain before demotion.


```bash
# 1. Stop the container
docker stop control-plane-host-4


# 2. Update docker-compose.yaml environment:
PGEDGE_ETCD_MODE: client  # was: server


# 3. Restart
docker-compose up -d host-4


# 4. Verify (check logs)
docker logs control-plane-host-4
```


## Troubleshooting


### Promotion Issues


**Problem**: Host fails to join cluster
**Solution**: Check logs for connection errors. Verify network connectivity and that other hosts are healthy.


**Problem**: "Permission denied" errors
**Solution**: System automatically obtains new credentials. If issue persists, check RBAC is enabled on cluster.


### Demotion Issues


**Problem**: Host fails to remove itself from membership
**Solution**: Check remaining servers have quorum. System continues transition even if removal fails.


**Problem**: Old data directory persists
**Solution**: System automatically cleans up etcd directory. If persists, manually remove after verifying host transitioned.


### General Troubleshooting


Check cluster health:


```bash
docker exec control-plane-host-1 etcdctl member list
```


All members should show `STATUS=started`.


## Best Practices


- **Change one host at a time** - Wait for completion before reconfiguring another
- **Monitor cluster health** - Verify all servers healthy before/after changes
- **Maintain odd numbers** - Always keep 3 or 5 etcd servers, never 2 or 4

## Summary


Etcd mode reconfiguration is fully automatic - just update the environment variable and restart. The Control Plane handles all cluster operations including credential provisioning, membership changes, and configuration updates without manual intervention.

