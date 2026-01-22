# Control Plane Automated Testing Infrastructure

## End-to-End Deployment & Testing Pipeline

---

## Slide 1: Overview - The Automation Pipeline

### What We've Built

```
┌─────────────────────────────────────────────────────────────────────┐
│                    AUTOMATED TESTING PIPELINE                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐     │
│   │  Launch  │───▶│  Setup   │───▶│  Deploy  │───▶│   Run    │     │
│   │ AWS VMs  │    │  Docker  │    │ Control  │    │  Tests   │     │
│   │          │    │  Swarm   │    │  Plane   │    │          │     │
│   └──────────┘    └──────────┘    └──────────┘    └──────────┘     │
│                                                                      │
│   Ansible Role:   Ansible Role:   Ansible Role:   Go Test Suite     │
│   aws             docker_swarm    deploy_cp       http-apitest      │
│                                   save_node_ips                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Benefits
- **One Command Deployment**: `ansible-playbook site.yaml`
- **Reproducible**: Same infrastructure every time
- **Scalable**: Change `ec2_instance_count` to add more nodes
- **Integrated Testing**: Auto-configures test environment

---

## Slide 2: Infrastructure Provisioning (Ansible)

### AWS Infrastructure Setup

```yaml
# site.yaml - Main Playbook
- name: Launch AWS infrastructure
  roles:
    - aws              # Create EC2 instances, SSH keys

- name: Setup Docker Swarm cluster
  roles:
    - docker_swarm     # Install Docker, init Swarm

- name: Deploy Control Plane
  roles:
    - deploy_control_plane  # Pull & run containers

- name: Save node IPs for testing
  roles:
    - save_node_ips    # Write nodes_config.json
```

### Infrastructure Components

| Component | Configuration |
|-----------|--------------|
| **Cloud** | AWS EC2 (ap-south-1) |
| **Instance Type** | t2.large |
| **OS** | Rocky Linux |
| **Container Runtime** | Docker CE + Swarm |
| **Nodes** | Configurable (1-N) |

### Ansible Roles Created
1. **docker_swarm** - Installs Docker, initializes Swarm manager, joins worker nodes
2. **deploy_control_plane** - Pulls latest image from `ghcr.io/pgedge/control-plane`
3. **save_node_ips** - Exports VM IPs to `nodes_config.json` for test framework

---

## Slide 3: Docker Swarm Cluster Architecture

### Multi-Node Cluster Setup

```
                    ┌─────────────────────────────────────┐
                    │         DOCKER SWARM CLUSTER        │
                    └─────────────────────────────────────┘
                                      │
           ┌──────────────────────────┼──────────────────────────┐
           │                          │                          │
           ▼                          ▼                          ▼
    ┌─────────────┐           ┌─────────────┐           ┌─────────────┐
    │   Node 1    │           │   Node 2    │           │   Node 3    │
    │  (Manager)  │           │  (Worker)   │           │  (Worker)   │
    ├─────────────┤           ├─────────────┤           ├─────────────┤
    │ Control     │           │ Control     │           │ Control     │
    │ Plane       │           │ Plane       │           │ Plane       │
    │ :3000       │           │ :3000       │           │ :3000       │
    └─────────────┘           └─────────────┘           └─────────────┘
          │                         │                         │
          └─────────────────────────┼─────────────────────────┘
                                    │
                                    ▼
                          ┌─────────────────┐
                          │ nodes_config.json│
                          │ ["ip1","ip2",..]│
                          └─────────────────┘
```

### Docker Swarm Setup Process
1. Install Docker CE on all nodes
2. Initialize Swarm on first node (becomes manager)
3. Get worker join token
4. Join remaining nodes as workers
5. Deploy Control Plane container on each node

---

## Slide 4: HTTP API Test Framework

### Test Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│                      HTTP API TEST FRAMEWORK                        │
├────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────┐         ┌─────────────────────────────────┐  │
│  │ nodes_config.json│────────▶│        Test Runner              │  │
│  │                  │         │   go test -tags=http_apitest    │  │
│  │ {                │         ├─────────────────────────────────┤  │
│  │   "nodes": [     │         │                                 │  │
│  │     "IP1",       │         │  1. GET /v1/cluster/init        │  │
│  │     "IP2",       │         │     └─▶ Returns token, URL      │  │
│  │     "IP3"        │         │                                 │  │
│  │   ]              │         │  2. POST /v1/cluster/join       │  │
│  │ }                │         │     └─▶ Join nodes to cluster   │  │
│  └─────────────────┘         │                                 │  │
│                               │  3. Verify cluster formation    │  │
│                               │                                 │  │
│                               └─────────────────────���───────────┘  │
│                                                                     │
└────────────────────────────────────────────────────────────────────┘
```

### Test Configuration Priority
1. **Environment Variable**: `CP_NODE_IPS=ip1,ip2,ip3`
2. **Config File**: `nodes_config.json`
3. **Default**: localhost (for local development)

### Running Tests
```bash
# After Ansible playbook completes:
cd /path/to/control-plane
go test -v -tags=http_apitest ./http-apitest/tests/...
```

### Test Flow
1. Initialize cluster on Node 0 → Get join token
2. Join remaining nodes (1 to N) using token
3. Verify all nodes successfully joined

---

## Slide 5: Complete Workflow

### End-to-End Execution

```bash
# Step 1: Configure (optional - defaults work)
vim roles/aws/vars/main.yaml
# Set: ec2_instance_count: 3

# Step 2: Run Ansible Playbook
cd /path/to/ansible/launch_aws_setup
ansible-playbook site.yaml

# Step 3: Run API Tests
cd /path/to/control-plane
go test -v -tags=http_apitest ./http-apitest/tests/...
```

### What Happens Behind the Scenes

| Step | Action | Output |
|------|--------|--------|
| 1 | Launch EC2 instances | 3 VMs with public IPs |
| 2 | Configure SSH | Passwordless SSH between nodes |
| 3 | Install Docker | Docker CE + plugins on all nodes |
| 4 | Init Swarm | Manager on node 1, workers join |
| 5 | Deploy Control Plane | Container running on port 3000 |
| 6 | Save IPs | `nodes_config.json` updated |
| 7 | Run Tests | Cluster init/join verification |

### Key Files

| File | Purpose |
|------|---------|
| `site.yaml` | Main Ansible playbook |
| `roles/aws/vars/main.yaml` | AWS configuration (instance count, type) |
| `roles/docker_swarm/tasks/main.yaml` | Docker & Swarm setup |
| `roles/deploy_control_plane/tasks/main.yaml` | Container deployment |
| `http-apitest/tests/nodes_config.json` | Test node configuration |
| `http-apitest/tests/cluster_test.go` | API test cases |

---

## Summary

### What We Achieved

✅ **Infrastructure as Code** - Reproducible AWS environment
✅ **Container Orchestration** - Docker Swarm for multi-node deployment
✅ **Automated Deployment** - Control Plane from official releases
✅ **Integrated Testing** - Seamless handoff to test framework
✅ **Single Command** - Full pipeline with `ansible-playbook site.yaml`

### Future Enhancements
- Add CI/CD integration (GitHub Actions / CircleCI)
- Implement teardown playbook for cleanup
- Add more comprehensive API tests
- Support for different cloud providers (GCP, Azure)
