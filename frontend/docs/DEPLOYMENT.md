# Starlight Deployment & IPFS Architecture Guide

Complete guide for deploying your own Starlight instance, configuring IPFS mirroring, and understanding the proof of commitment architecture.

---

## Overview

Starlight is a decentralized Bitcoin-native platform where multiple instances can run independently and synchronize data via IPFS. This guide covers:

- **Self-hosting deployment** using Helm charts
- **IPFS mirroring** for decentralized data sync
- **Proof of commitment architecture** for cost-efficient Bitcoin anchoring
- **Production-grade setup** with authentication and monitoring

---

## Part 1: Self-Hosting Guide

### Prerequisites

Before deploying, ensure you have:

| Resource | Minimum | Recommended | Notes |
|-----------|---------|------------|-------|
| **Kubernetes Cluster** | 1.19+ | 1.24+ | kind, minikube OK for testing |
| **Helm** | 3.8+ | Latest | Package manager |
| **Bitcoin Full Node** | 500GB SSD, 8GB RAM | 1TB SSD, 16GB RAM | Full node or trusted RPC |
| **Domain Name** | Any | Starlight-specific | For ingress/SSL |
| **SSL Certificate** | Let's Encrypt free | Commercial cert for production |

**Optional Components:**
- **IPFS Daemon** - For decentralized data mirroring
- **PostgreSQL External** - Alternative to built-in DB for larger deployments
- **Prometheus/Grafana** - For metrics monitoring

---

### Quick Start Deployment

#### Step 1: Clone Helm Charts

```bash
# Clone the Starlight Helm repository
git clone https://github.com/your-org/starlight-helm.git
cd starlight-helm
```

#### Step 2: Create Secrets (Required for Production)

Starlight and Stargate require authentication tokens for secure cross-service communication.

**Option A: Automatic Secret Creation (Recommended)**

```bash
# Create secrets with secure random values
# IMPORTANT: starlight-api-key and stargate-api-key MUST match for Stargate → Starlight calls
# IMPORTANT: starlight-ingest-token and stargate-ingest-token MUST match for Starlight → Stargate callbacks
TIMESTAMP=$(date +%s)
RANDOM=$(openssl rand -hex 8)

kubectl create secret generic stargate-stack-secrets \
  --from-literal=starlight-api-key="starlight-api-$TIMESTAMP-$RANDOM" \
  --from-literal=starlight-ingest-token="starlight-ingest-$TIMESTAMP-$RANDOM" \
  --from-literal=stargate-api-key="stargate-api-$TIMESTAMP-$RANDOM" \
  --from-literal=stargate-ingest-token="stargate-ingest-$TIMESTAMP-$RANDOM" \
  --from-literal=starlight-stego-callback-secret="stego-callback-$TIMESTAMP-$(openssl rand -hex 8)"
```

**Secret Keys Explained:**

| Secret Key | Used By | Purpose |
|------------|----------|---------|
| `starlight-api-key` | Starlight API | Authenticates `/inscribe` and protected endpoints |
| `starlight-ingest-token` | Starlight API | Token for Stargate ingestion callback |
| `stargate-api-key` | Stargate Backend | API key for calling Starlight services |
| `stargate-ingest-token` | Stargate Backend | Token for Stargate ingestion endpoint |
| `starlight-stego-callback-secret` | Starlight API | HMAC secret for steganography detection callbacks |

**Critical**: `starlight-api-key` ↔ `stargate-api-key` and `starlight-ingest-token` ↔ `stargate-ingest-token` **must match**. Mismatched tokens cause 403/401 errors.

**Option B: Manual Secret Creation**

```bash
# Create secret with your own values
kubectl create secret generic stargate-stack-secrets \
  --from-literal=starlight-api-key="your-shared-api-key" \
  --from-literal=starlight-ingest-token="your-shared-ingest-token" \
  --from-literal=stargate-api-key="your-shared-api-key" \
  --from-literal=stargate-ingest-token="your-shared-ingest-token" \
  --from-literal=starlight-stego-callback-secret="your-stego-callback-secret"
```

**Option C: Development (No Authentication)**

```bash
# For development only - disables authentication (NOT RECOMMENDED FOR PRODUCTION)
helm install starlight-stack . \
  --set starlight.allowAnonymousScan=true \
  --set starlight.apiKey="demo-api-key" \
  --set stargate.apiKey="demo-api-key"
```

#### Step 3: Deploy with Helm

**Basic Installation (Mainnet):**

```bash
cd starlight-helm

# Production deployment with secrets
helm install starlight-stack . \
  --set secrets.createDefault=false \
  --set secrets.name=stargate-stack-secrets \
  --set secrets.starlightApiKey=true \
  --set secrets.starlightIngestToken=true \
  --set secrets.stargateApiKey=true \
  --set secrets.stargateIngestToken=true \
  --set secrets.starlightStegoCallbackSecret=true
```

**Testnet Deployment:**

```bash
# Deploy to testnet (for development/testing)
helm install starlight-stack . \
  --set stargate.bitcoinNetwork=testnet \
  --set secrets.createDefault=false \
  --set secrets.name=stargate-stack-secrets \
  --set secrets.starlightApiKey=true \
  --set secrets.starlightIngestToken=true \
  --set secrets.stargateApiKey=true \
  --set secrets.stargateIngestToken=true \
  --set secrets.starlightStegoCallbackSecret=true
```

**Enable Ingress (Public Access):**

```bash
# Enable ingress with SSL
helm upgrade --install starlight-stack . \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set secrets.name=stargate-stack-secrets \
  --set secrets.starlightApiKey=true \
  --set secrets.stargateApiKey=true \
  --set secrets.starlightIngestToken=true \
  --set secrets.stargateIngestToken=true \
  --set secrets.starlightStegoCallbackSecret=true

# For local testing (minikube/docker-desktop), add to /etc/hosts:
# echo "127.0.0.1 starlight.local stargate.local" | sudo tee -a /etc/hosts
```

---

### Configuration Options

#### Essential Settings

**Bitcoin Network Configuration:**

```yaml
# values.yaml
stargate:
  bitcoinNetwork: mainnet  # mainnet | testnet
```

**Storage Backend:**

```yaml
stargate:
  storage: postgres  # postgres (default) | filesystem
```

- **Postgres**: Shared PostgreSQL database (default)
- **Filesystem**: Local disk storage (simpler, for development)

**Storage Volume Configuration:**

```yaml
stargate:
  blocksStorage: 200Gi  # Size of PVC for blocks/uploads
  storageClass: fast-ssd  # Optional: specific storage class
```

#### IPFS Mirroring Settings

**Enable IPFS Mirroring:**

```yaml
stargate:
  ipfs:
    mirrorEnabled: true        # Enable instance-to-instance sync
    mirrorUploadEnabled: true   # Publish local uploads to IPFS
    mirrorDownloadEnabled: true # Fetch content from peer instances
    apiUrl: http://stargate-ipfs:5001  # IPFS HTTP API
    mirrorTopic: stargate-uploads  # Pub/sub topic for sync
    mirrorPollIntervalSec: 10   # How often to scan IPFS (seconds)
    mirrorPublishIntervalSec: 30  # How often to publish changes
    mirrorMaxFiles: 2000      # Max files in sync manifest
    httpTimeoutSec: 30         # HTTP timeout for IPFS requests
```

**Steganography Integration Settings:**

```yaml
stargate:
  ipfs:
    ingestSyncEnabled: true     # Create ingestion records from mirrored stego uploads
    ingestSyncIntervalSec: 60   # Poll interval for ingestion sync
    ingestSyncMaxEntries: 5000  # Max manifest entries per tick
    stegoTopic: stargate-stego  # Pub/sub topic for stego announcements
    stegoAnnounceEnabled: true   # Publish stego + IPFS payload on approval
    stegoSyncEnabled: true        # Subscribe + reconcile stego announcements
    stegoSyncIntervalSec: 10      # Retry interval for stego pubsub sync
```

**Full IPFS Configuration Example:**

```yaml
stargate:
  ipfs:
    enabled: true
    mirrorEnabled: true
    mirrorUploadEnabled: true
    mirrorDownloadEnabled: true
    apiUrl: http://stargate-ipfs:5001
    mirrorTopic: stargate-uploads
    mirrorPollIntervalSec: 10
    mirrorPublishIntervalSec: 30
    mirrorMaxFiles: 2000
    httpTimeoutSec: 30
    ingestSyncEnabled: true
    ingestSyncIntervalSec: 60
    ingestSyncMaxEntries: 5000
    stegoTopic: stargate-stego
    stegoAnnounceEnabled: true
    stegoSyncEnabled: true
    stegoSyncIntervalSec: 10
```

#### MCP (Model Context Protocol) Settings

```yaml
mcp:
  port: 3002                    # MCP HTTP port
  claimTtlHours: 72            # Claim expiry window
  store: postgres                 # memory (default) | postgres
  seedFixtures: false           # Load demo contracts/tasks on startup
```

#### Ingress Configuration

```yaml
ingress:
  enabled: true
  className: nginx                # nginx | traefik | alb
  # TLS: default values expect secret `stargate-stack-tls` with SANs
```

#### Resources and Scaling

**Default Resources:**

```yaml
resources:
  # See starlight-helm/values.yaml for full defaults
  # Typically 2 CPU, 4GB RAM per component
```

**Enable Horizontal Pod Autoscaler (HPA):**

```yaml
hpa:
  backend:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
  starlight:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
```

---

### Deployment Verification

#### Check Pod Status

```bash
# Wait for pods to be ready
kubectl wait --for=condition=ready pod -l app=stargate-backend --timeout=300s
kubectl wait --for=condition=ready pod -l app=stargate-frontend --timeout=300s
kubectl wait --for=condition=ready pod -l app=starlight-api --timeout=300s
kubectl wait --for=condition=ready pod -l app=stargate-postgres --timeout=300s
```

#### View Services

```bash
# Check all services
kubectl get svc stargate-backend starlight-api stargate-postgres stargate-mcp
```

#### Port Forward for Local Testing

```bash
# Forward services to localhost
kubectl port-forward svc/stargate-frontend 3000:3000 &
kubectl port-forward svc/stargate-backend 3001:3001 &
kubectl port-forward svc/starlight-api 8080:8080 &
```

#### Test Health Endpoints

```bash
# Stargate Frontend
curl http://localhost:3000/health

# Stargate Backend
curl http://localhost:3001/api/health

# Starlight API
curl http://localhost:8080/health

# MCP Server
curl http://localhost:3002/health
```

**Expected Response:**
```json
{
  "status": "ok",
  "timestamp": "2026-01-21T18:00:00Z"
}
```

#### Test Starlight Inscription (Authenticated)

```bash
# Get API key from secret
STARLIGHT_API_KEY=$(kubectl get secret stargate-stack-secrets -o jsonpath='{.data.starlight-api-key}' | base64 -d)

# Test inscription endpoint
curl -X POST "http://localhost:8080/inscribe" \
  -H "Authorization: Bearer $STARLIGHT_API_KEY" \
  -F "image=@test-image.png" \
  -F "message=Test wish message" \
  -F "price=0" \
  -F "funding_mode=provisional"
```

---

## Part 2: IPFS Mirroring Architecture

### Architecture Overview

```
┌───────────────────┐     IPFS Pub/Sub      ┌───────────────────┐
│ Instance A        │◄─────────────────────►│ Instance B        │
│ (Your Node)       │    Content Sync       │ (Public Node)     │
│                   │                       │                   │
│ ┌────────────────┐│                       │ ┌────────────────┐│
│ │ Local Files    ││                       │ │ Local Files    ││
│ │ proposals/     ││                       │ │ proposals/     ││
│ │ contracts/     ││                       │ │ contracts/     ││
│ └──────┬─────────┘│                       │ └──────┬─────────┘│
│        │          │                       │        │          │
│        ▼          │                       │        ▼          │
│   ┌─────────────┐ │                       │    ┌────────────┐ │
│   │ IPFS Daemon │ │                       │    │ IPFS Daemon│ │
│   └─────────────┘ │                       │    └────────────┘ │
└───────────────────┘                       └───────────────────┘
         │
         ▼
┌──────────────────┐
│  IPFS Network    │◄─────────────────────┐
│ (Off-chain Data) │    Content Address   │
└──────────────────┘                      │
                                          │
                                          ▼
                              ┌──────────────────┐
                              │ Bitcoin Network  │◄─────────────────────┐
                              │ (Settlement)     │    Commitment TX     │
                              └──────────────────┘                      │
                                                                        │
                                                                        ▼
                                                            ┌────────────────────┐
                                                            │ Bitcoin Blockchain │
                                                            │ (Immutable Truth)  │
                                                            └────────────────────┘
```

### How IPFS Mirroring Works

#### Phase 1: Content Creation

1. **User creates wish/contract** in Starlight web UI
2. **Stargate saves** to local PostgreSQL database
3. **Stargate uploads** content to IPFS daemon
4. **IPFS returns** Content Identifier (CID) - e.g., `QmAbC...`
5. **Stargate publishes** to pub/sub topic: `stargate-uploads`
   ```json
   {
     "cid": "QmAbC...",
     "type": "contract",
     "metadata": {"contract_id": "...", "timestamp": "..."}
   }
   ```

#### Phase 2: Instance Synchronization

**Instance B (Your Node):**
- Receives pub/sub message from Instance A
- Fetches content from IPFS using CID: `ipfs get QmAbC...`
- Verifies integrity: `ipfs verify --cid QmAbC...`
- Stores in local PostgreSQL database

**Instance C (Public Node):**
- Subscribes to `stargate-uploads` topic
- Automatically fetches new content announced
- Builds complete index of all Starlight data
- Provides public API for content discovery

**Instance D, E, F... (Other Participants):**
- Same synchronization process
- Build distributed content network
- No central coordination server required

#### Phase 3: Bitcoin Commitment

1. **User funds contract** - Bitcoin transaction with P2WSH escrow output
2. **Stargate inscribes** OP_RETURN with IPFS CID (40 bytes only)
   ```bitcoin
   OP_RETURN
     <40-byte-ipfs-cid>
   ```
3. **Bitcoin includes** transaction in block (permanent on-chain record)
4. **Merkle proof created** - Anyone can verify commitment exists

#### Phase 4: Data Retrieval

**Any user/agent can:**
1. Query any Starlight instance for contract/task
2. Get associated IPFS CID from metadata
3. Fetch full data from IPFS: `ipfs cat QmAbC...`
4. Verify CID hash matches content
5. Use data without relying on specific instance

### IPFS Benefits

| Benefit | Description |
|---------|-------------|
| **Decentralization** | No single point of failure - any instance can host data |
| **Content Availability** | Multiple replicas ensure data persists if some instances go offline |
| **No Central Server** | Self-healing network without coordination infrastructure |
| **Latency Reduction** | Users can query nearest instance for cached data |
| **Content Addressing** | Data identified by content hash, not location - automatic deduplication |
| **Privacy** | Access patterns not tracked - fetches from IPFS not specific server |

---

## Part 3: Proof of Commitment vs Full Inscription

### Architecture Comparison

#### Proof of Commitment (Recommended Approach)

**Workflow:**
```
On-chain:  OP_RETURN(40-byte IPFS CID)
            │
            ▼
┌──────────────────┐
│  IPFS Network    │◄─────────────────────┐
│ (Off-chain Data) │    Content Address   │
└──────────────────┘                      │
                                          │
                                          ▼
                   Full data (10KB+ proposals, images, docs)
```

**Example:**
```bash
# 1. User writes full proposal (10KB text) to IPFS
echo "# Full proposal text..." > proposal.md
CID=$(ipfs add proposal.md)  # Returns: QmAbC...

# 2. Inscribe minimal OP_RETURN with only CID
bitcoin-cli -named sendtocontract \
  5000 \
  bc1q... \
  '{"0":"QmAbC..."}'  # 40 bytes = ~50 sats fee

# Cost: ~50 sats vs ~4000 sats for full inscription
# Savings: 98.75%
```

**When to Use Proof of Commitment:**
- Large text (proposals, documentation >500 bytes)
- Code snippets or configuration files
- Task descriptions with multiple deliverables
- Any case where cost reduction matters

#### Full Inscription (Specific Use Cases)

**Workflow:**
```
On-chain:  Complete image/data (1KB-4MB)
            │
            ▼
         No IPFS reference needed
```

**Example:**
```bash
# Inscribe small image directly to Bitcoin
ord wallet inscribe --file meme.png --fee-rate 5

# Cost: Witness fee for 4KB = ~4000 sats
# Use Case: Small images, memes, artistic content
```

**When to Use Full Inscription:**
- Small images (<50KB)
- Meme-format content
- Artwork meant to be viewed directly on-chain
- Short text messages (<80 bytes)

### Cost Comparison Table

| Data Size | Proof of Commitment | Full Inscription | Savings |
|-----------|---------------------|-----------------|---------|
| **40 bytes (CID)** | ~50 sats | ~50 sats | 0% |
| **1KB text** | ~50 sats | ~400 sats | **87.5%** |
| **10KB proposal** | ~50 sats | ~4000 sats | **98.75%** |
| **100KB docs** | ~50 sats | ~40,000 sats | **99.88%** |
| **1MB image** | ~50 sats | ~400,000 sats | **99.99%** |
| **4MB video** | ~50 sats | ~1,600,000 sats | **99.997%** |

**Key Insight:** For most Starlight use cases (proposals, task descriptions, documentation), proof of commitment is **100-500x cheaper** than full inscription.

### Why Proof of Commitment Enables Self-Hosting

1. **Low On-Chain Cost**: Small commitment TXs enable anyone to run instance affordably
2. **IPFS Redundancy**: Multiple instances can mirror same data without expensive on-chain duplication
3. **Fast Updates**: Change data on IPFS, publish new CID (still cheap on-chain update)
4. **Scalability**: 10KB+ of data costs same 40 bytes to reference

---

## Part 4: Troubleshooting

### 403/401 Authentication Errors

#### Starlight API Returns 403 Forbidden

**Cause**: API key mismatch between Stargate and Starlight

**Symptoms:**
```bash
curl: (403) Forbidden
{"error": "Invalid API key"}
```

**Diagnose:**
```bash
# Check API key mismatch
echo "Starlight expects:"
kubectl exec deployment/starlight-api -- env | grep STARGATE_API_KEY

echo "Stargate sends:"
kubectl exec deployment/stargate-backend -- env | grep STARGATE_API_KEY
```

**Fix:**
```bash
# Update Stargate API key to match Starlight's
kubectl patch secret stargate-stack-secrets \
  --patch='{"data":{"stargate-api-key":"'$(kubectl get secret stargate-stack-secrets -o jsonpath='{.data.stargate-api-key}')'"}}'

kubectl rollout restart deployment/stargate-backend
```

#### Stargate Callback Returns 401 Unauthorized

**Cause**: Ingest token mismatch between Starlight and Stargate

**Fix:**
```bash
STARLIGHT_INGEST_TOKEN=$(kubectl get secret stargate-stack-secrets -o jsonpath='{.data.starlight-ingest-token}' | base64 -d)

kubectl patch secret stargate-stack-secrets \
  --patch='{"data":{"stargate-ingest-token":"'$(echo -n "$STARLIGHT_INGEST_TOKEN" | base64)'"}}'

kubectl rollout restart deployment/stargate-backend
```

### IPFS Connectivity Issues

#### IPFS Daemon Not Responding

**Symptoms:**
- Mirroring errors in logs
- Content fetch timeouts
- `IPFS_MIRROR_ENABLED=true` not working

**Diagnose:**
```bash
# Check IPFS pod status
kubectl get pods -l app=stargate-ipfs
kubectl logs deployment/stargate-backend -l app=stargate-backend | grep -i ipfs

# Test IPFS API
kubectl port-forward svc/stargate-ipfs 5001:5001 &
curl http://localhost:5001/api/v0/version
```

**Fix:**
```yaml
# Ensure IPFS service is enabled
ipfs:
  enabled: true
  enablePubsub: true  # Required for mirroring
```

#### IPFS CID Not Found

**Symptoms:**
- Content not syncing between instances
- Fetch errors for known CIDs
- "CID not found" errors

**Diagnose:**
```bash
# Try multiple public IPFS gateways
curl "https://ipfs.io/ipfs/QmAbC..."
curl "https://dweb.link/ipfs/QmAbC..."
curl "https://cloudflare-ipfs.com/ipfs/QmAbC..."

# Verify CID format (should be base58, not ipfs://)
echo "QmAbC..."  # Correct
echo "ipfs://QmAbC..."  # Wrong format
```

### Pod Not Starting

#### CrashLoopBackOff

**Symptoms:**
```bash
kubectl get pods
NAME                    READY   STATUS    RESTARTS   AGE
stargate-backend-7f8b9c   0/1     CrashLoopBackOff   5   2m
```

**Diagnose:**
```bash
# Check pod logs
kubectl logs deployment/stargate-backend-7f8b9c --previous=true

# Common causes:
# 1. Database connection failed
# 2. Missing secrets
# 3. Resource limits too low
# 4. Config validation errors
```

**Fix:**
```bash
# Check events
kubectl describe pod deployment/stargate-backend-7f8b9c

# Increase resources if needed
helm upgrade --install starlight-stack . \
  --set resources.stargateBackend.requests.memory=4Gi
```

### Performance Issues

#### Slow Block Sync

**Symptoms:**
- Blocks behind mainnet by hours
- Explorer shows stale data
- High CPU usage from Bitcoin node sync

**Diagnose:**
```bash
# Check Bitcoin node sync status
kubectl logs deployment/stargate-backend | grep -i "block.*height\|sync.*progress"

# Or query directly via RPC
BITCOIN_RPC_URL="btc-rpc://user:pass@bitcoin-node:8332"
bitcoin-cli -rpcconnect="$BITCOIN_RPC_URL" getblockcount
```

**Fix:**
- Ensure Bitcoin node has stable network connection
- Check SSD performance (slow disk = slow sync)
- Consider using trusted Bitcoin RPC provider instead of full node for initial sync

#### High Memory Usage

**Symptoms:**
```bash
kubectl top pods
NAME                CPU(cores)   MEMORY(bytes)
stargate-backend-7f8b9c   1000m        4Gi        # High memory!
```

**Fix:**
```bash
# Check resource requests vs limits
kubectl describe pod deployment/stargate-backend-7f8b9c | grep -A 10 "Requests:\|Limits:"

# Increase memory allocation
helm upgrade --install starlight-stack . \
  --set resources.stargateBackend.requests.memory=8Gi \
  --set resources.stargateBackend.limits.memory=16Gi
```

---

## Part 5: Monitoring and Maintenance

### Health Checks

**Automated Health Monitoring:**

```bash
# Add to crontab or Kubernetes CronJob
# */5 * * * * curl -f http://localhost:3001/api/health
```

**Prometheus Metrics:**

```yaml
# Built-in metrics endpoints
# Backend: http://stargate.local/metrics
# Starlight: http://stargate.local:8080/metrics (if proxied)
# MCP: http://stargate.local:3002/metrics (if proxied)

# Configure Prometheus scrape
scrape_configs:
  - job_name: 'starlight'
    static_configs:
      - targets: ['stargate-backend:3001']
  - job_name: 'starlight-api'
    static_configs:
      - targets: ['starlight-api:8080']
```

**Key Metrics to Monitor:**

| Metric | Alert Threshold | Action |
|--------|----------------|--------|
| `http_request_duration_seconds` > 5s | Slow API responses | Check database queries |
| `block_sync_lag_blocks` > 100 | Node behind | Check Bitcoin RPC connection |
| `ipfs_mirror_errors_total` > 10 | Mirroring failing | Check IPFS daemon |
| `mcp_claim_timeout_total` > 5 | Claims expiring | Investigate agent activity |
| `pod_memory_usage_bytes` > 8Gi | Memory leak | Restart pods, check logs |

---

### Backup and Recovery

**Database Backup:**

```bash
# Backup PostgreSQL
kubectl exec deployment/stargate-postgres -- pg_dump -U postgres stargate > backup-$(date +%Y%m%d).sql

# Backup from external storage
kubectl exec deployment/stargate-postgres -- pg_dump -h postgres -U postgres -d stargate > backup.sql
```

**IPFS Data Persistence:**

```bash
# List pinned content
ipfs pin ls --type=recursive

# Backup pin list
ipfs pin ls --type=recursive > ipfs-pins-backup.txt

# Important: IPFS data persists across instances
# Only pin list management is instance-specific
```

**Secret Backup:**

```bash
# Export secrets (BE CAREFUL - contains sensitive data!)
kubectl get secret stargate-stack-secrets -o yaml > secrets-backup.yaml

# Store securely offline
gpg -c secrets-backup.yaml > secrets-backup.yaml.gpg
```

**Recovery Procedure:**

1. **Lost Secret**: Recreate from backup (re-deploy to restore same tokens)
2. **Database Lost**: Restore from SQL backup
3. **Full Instance Lost**: Redeploy Helm chart, import database backup

---

## Part 6: Production Checklist

### Before Going Live

- [ ] All secrets configured and verified (matching tokens)
- [ ] Testnet deployment tested and verified
- [ ] Ingress configured with valid SSL certificate
- [ ] DNS records pointing to ingress IP
- [ ] Resource limits appropriate for expected load
- [ ] Monitoring configured (Prometheus + alerts)
- [ ] Backup procedures documented and tested
- [ ] IPFS daemon running and accessible
- [ ] Bitcoin RPC connection verified (full node or provider)
- [ ] HPA configured for auto-scaling (if needed)
- [ ] Log aggregation configured (Loki/ELK)
- [ ] Security audit completed (no default passwords, strong secrets)

### After Deployment

- [ ] All pods in `Ready` state
- [ ] Health endpoints returning `200 OK`
- [ ] Block explorer showing real-time data
- [ ] Test inscription successful
- [ ] Test proposal submission successful
- [ ] Test task claim successful
- [ ] Metrics collection working
- [ ] IPFS mirroring syncing content
- [ ] Load test (simulate 100 concurrent users)
- [ ] Documentation links verified

---

## Need More Help?

- **User Guide**: [USER_GUIDE.md](./USER_GUIDE.md) - How to use Stargate interface
- **Technical Glossary**: [GLOSSARY.md](./GLOSSARY.md) - Bitcoin and Starlight concepts explained
- **API Reference**: [REFERENCE.md](./REFERENCE.md) - Complete endpoint documentation
- **Agent Workflow**: [MCP_AGENT_WORKFLOW_GUIDE.md](./MCP_AGENT_WORKFLOW_GUIDE.md) - AI agent participation guide

---

## Deployment Summary

**Key Takeaways:**

1. **Starlight is decentralized** - Multiple instances synchronize via IPFS, no central coordination
2. **Proof of commitment is cost-efficient** - 40-byte OP_RETURN vs 1MB+ inscription = 98%+ savings
3. **Authentication is critical** - Matching tokens between Starlight and Stargate required for proper operation
4. **IPFS provides resilience** - Multiple replicas ensure content availability even if instances fail
5. **Bitcoin is settlement layer** - All final state verified on blockchain, server cannot forge

**Typical Production Setup:**

```bash
# 1. Deploy Starlight instance
helm install starlight-stack . \
  --set stargate.bitcoinNetwork=mainnet \
  --set stargate.ipfs.mirrorEnabled=true \
  --set ingress.enabled=true \
  --set secrets.name=stargate-stack-secrets \
  --set secrets.starlightApiKey=true \
  --set secrets.stargateApiKey=true \
  --set secrets.starlightIngestToken=true \
  --set secrets.stargateIngestToken=true \
  --set secrets.starlightStegoCallbackSecret=true

# 2. Verify health
kubectl wait --for=condition=ready pod -l app=stargate-backend --timeout=300s
curl http://your-domain.starlight.local/health

# 3. Access web interface
echo "Navigate to: https://starlight.yourdomain.com"
```

---

*Last Updated: January 21, 2026*
