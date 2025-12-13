# Bitcoin Smart Contract System API Documentation

## Overview

The Stargate Bitcoin Smart Contract System provides a complete Bitcoin-backed smart contract platform with escrow services, transaction monitoring, and dispute resolution. This system transforms Stargate from a basic task platform into a true Bitcoin smart contract ecosystem.

## Architecture

### Core Components

1. **Script Interpreter** (`script_interpreter.go`)
   - Validates Bitcoin scripts (P2PKH, multisig, timelock, Taproot)
   - Extracts script details and validates contract types
   - Supports OP_CHECKSIG, OP_CHECKMULTISIG, OP_HASH160 operations

2. **Merkle Proof Verifier** (`merkle_verifier.go`)
   - Validates proofs against blockchain data via Blockstream API
   - Recalculates Merkle roots from proof paths
   - Batch verification and proof chain validation
   - Real-time proof refresh and status monitoring

3. **Automated Escort Service** (`escort_service.go`)
   - Manages proof lifecycle (provisional → confirmed)
   - Automated proof validation and monitoring
   - Script validation integration
   - Contract completion and dispute detection

4. **Bitcoin Escrow Manager** (`escrow_manager.go`)
   - 2-of-3 multisig escrow creation and management
   - Timelock and Taproot contract support
   - Funding, claiming, and payout processing
   - Refund handling with proper validation

5. **Transaction Monitor** (`transaction_monitor.go`)
   - Real-time transaction status tracking
   - Event-driven architecture for contract events
   - Configurable confirmation requirements
   - Batch monitoring and status updates

6. **Dispute Resolution** (`dispute_resolution.go`)
   - Multi-arbitrator voting system
   - Evidence submission and validation
   - Weighted decision calculation
   - Appeal mechanism and payout distribution

## Network Configuration

### Mainnet
- **API**: `https://blockstream.info/api`
- **Explorer**: `https://blockstream.info`

### Testnet
- **API**: `https://blockstream.info/testnet/api`
- **Explorer**: `https://blockstream.info/testnet`
- **Faucet**: `https://coinfaucet.eu/en/btc-testnet/`

### Signet
- **API**: `https://mempool.space/signet/api`
- **Explorer**: `https://mempool.space/signet`
- **Faucet**: `https://signetfaucet.com/`

## API Endpoints

### Bitcoin Network

#### GET `/api/health`
Returns system health status including Bitcoin connection and scanner info.

**Response:**
```json
{
  "status": "healthy",
  "scanner": {
    "type": "mock_scanner",
    "initialized": true,
    "healthy": true
  },
  "scanner_manager": {
    "status": "healthy",
    "scanner_type": "mock_scanner",
    "circuit_breaker": "closed"
  },
  "bitcoin": {
    "node_connected": true,
    "node_url": "https://blockstream.info/api",
    "block_height": 825000
  },
  "timestamp": "2024-12-12T12:00:00Z"
}
```

#### GET `/api/info`
Returns API information and capabilities.

#### POST `/api/scan/transaction`
Scans a Bitcoin transaction for steganographic content.

**Request:**
```json
{
  "transaction_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
  "extract_images": true,
  "scan_options": {
    "extract_message": true,
    "confidence_threshold": 0.5,
    "include_metadata": true
  }
}
```

**Response:**
```json
{
  "transaction_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
  "block_height": 170000,
  "timestamp": "1231006505",
  "scan_results": {
    "images_found": 2,
    "images_scanned": 2,
    "stego_detected": true,
    "processing_time_ms": 1500
  },
  "images": [...],
  "request_id": "req_123456789"
}
```

#### POST `/api/scan/block`
Scans all transactions in a Bitcoin block for steganographic content.

#### POST `/api/extract`
Extracts hidden messages from steganographic images.

#### GET `/api/tx/{transaction_id}`
Retrieves transaction details with optional image data.

### Smart Contract System

#### Escrow Management

##### POST `/api/escrow/create`
Creates a new escrow contract.

**Request:**
```json
{
  "contract_id": "escrow-001",
  "type": "multisig_2of3",
  "participants": [
    "03a34b99f22c790c4e36b2b3c2c35a36db06226e41c692fc82b8b56ac1c540c5bd",
    "03b287eaf122eea69030d0e8b9c9b2d4b8345eef3c08c9a8355c1b9259b0c4c5d7",
    "03c4567890123456789012345678901234567890123456789012345678901234d8"
  ],
  "amount_sats": 100000,
  "timelock": 144,
  "description": "Service payment escrow"
}
```

**Response:**
```json
{
  "contract_id": "escrow-001",
  "address": "3FZbgi29cpjq2GjdwV8eyHuJJnkLtktZc5",
  "required_signatures": 2,
  "amount_sats": 100000,
  "timelock": 144,
  "status": "created",
  "created_at": "2024-12-12T12:00:00Z"
}
```

##### POST `/api/escrow/{contract_id}/fund`
Funds an escrow contract.

##### POST `/api/escrow/{contract_id}/claim`
Claims funds from an escrow contract.

##### POST `/api/escrow/{contract_id}/payout`
Processes payouts to contract participants.

##### POST `/api/escrow/{contract_id}/refund`
Processes refund from escrow contract.

##### GET `/api/escrow/{contract_id}/status`
Returns current escrow contract status.

#### Transaction Monitoring

##### POST `/api/monitor/transactions`
Adds transactions to monitoring.

**Request:**
```json
{
  "transactions": [
    {
      "tx_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
      "contract_id": "contract-001",
      "type": "funding",
      "required_confirmations": 6,
      "amount_sats": 100000
    }
  ]
}
```

##### GET `/api/monitor/transactions`
Returns all monitored transactions.

##### GET `/api/monitor/stats`
Returns monitoring service statistics.

#### Merkle Proof Verification

##### POST `/api/proofs/verify`
Verifies a Merkle proof against blockchain data.

**Request:**
```json
{
  "proof": {
    "tx_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
    "block_height": 170000,
    "block_header_merkle_root": "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
    "proof_path": [
      {
        "hash": "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6",
        "direction": "left"
      }
    ],
    "confirmation_status": "provisional"
  }
}
```

##### POST `/api/proofs/batch-verify`
Verifies multiple Merkle proofs.

##### POST `/api/proofs/refresh`
Refreshes proof status from blockchain.

#### Dispute Resolution

##### POST `/api/disputes/create`
Creates a new dispute case.

**Request:**
```json
{
  "contract_id": "contract-001",
  "dispute_type": "quality",
  "initiator": "client",
  "respondent": "provider",
  "evidence": ["evidence1.jpg", "evidence2.pdf"],
  "description": "Work quality does not meet requirements"
}
```

##### POST `/api/disputes/{dispute_id}/evidence`
Submits evidence for a dispute.

##### POST `/api/disputes/{dispute_id}/vote`
Casts a vote in dispute resolution.

##### POST `/api/disputes/{dispute_id}/resolve`
Resolves a dispute with final decision.

##### POST `/api/disputes/{dispute_id}/appeal`
Creates an appeal for dispute decision.

## Data Types

### Contract
```json
{
  "contract_id": "contract-001",
  "title": "Service Contract",
  "total_budget_sats": 100000,
  "goals_count": 3,
  "available_tasks_count": 3,
  "status": "active",
  "skills": ["bitcoin", "escrow", "smart-contracts"]
}
```

### Task
```json
{
  "task_id": "task-001",
  "contract_id": "contract-001",
  "goal_id": "goal-001",
  "title": "Create multisig escrow",
  "description": "Set up secure 2-of-3 multisig",
  "budget_sats": 50000,
  "skills": ["bitcoin", "escrow"],
  "status": "available",
  "difficulty": "medium",
  "estimated_hours": 2,
  "requirements": {
    "min_reputation": 4.5,
    "verification_required": true
  }
}
```

### MerkleProof
```json
{
  "tx_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
  "block_height": 170000,
  "block_header_merkle_root": "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b",
  "proof_path": [
    {
      "hash": "8b8a2e6d3e9c1b2a3f4e5d6c7b8a9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6",
      "direction": "left"
    }
  ],
  "visible_pixel_hash": "abc123def456",
  "funded_amount_sats": 100000,
  "funding_address": "3FZbgi29cpjq2GjdwV8eyHuJJnkLtktZc5",
  "confirmation_status": "confirmed",
  "seen_at": "2024-12-12T12:00:00Z",
  "confirmed_at": "2024-12-12T12:06:00Z"
}
```

### EscrowContract
```json
{
  "contract_id": "escrow-001",
  "type": "multisig_2of3",
  "participants": [
    "03a34b99f22c790c4e36b2b3c2c35a36db06226e41c692fc82b8b56ac1c540c5bd"
  ],
  "required_signatures": 2,
  "amount_sats": 100000,
  "timelock": 144,
  "funding_address": "3FZbgi29cpjq2GjdwV8eyHuJJnkLtktZc5",
  "status": "funded",
  "created_at": "2024-12-12T12:00:00Z",
  "funded_at": "2024-12-12T12:01:00Z"
}
```

## Error Handling

All API endpoints return consistent error responses:

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid JSON request body",
    "details": {
      "field": "transaction_id",
      "issue": "invalid_format"
    },
    "request_id": "req_123456789"
  }
}
```

### Common Error Codes

- `INVALID_REQUEST`: Malformed request
- `TX_NOT_FOUND`: Transaction not found on blockchain
- `PROOF_INVALID`: Merkle proof validation failed
- `ESCROW_NOT_FOUND`: Escrow contract not found
- `INSUFFICIENT_FUNDS`: Not enough funds to complete operation
- `UNAUTHORIZED`: Invalid signature or permission
- `RATE_LIMITED`: Too many requests

## Configuration

### Environment Variables

```bash
# Bitcoin Network
BITCOIN_NETWORK=testnet  # mainnet | testnet | signet

# API Configuration
API_HOST=0.0.0.0
API_PORT=3001
API_TIMEOUT=30s

# Database
DATABASE_URL=postgresql://user:pass@localhost/stargate
DATABASE_POOL_SIZE=10

# Monitoring
MONITOR_CHECK_INTERVAL=2m
MONITOR_REQUIRED_CONFIRMATIONS=6

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
```

## Security Considerations

1. **Private Key Management**: Never log or expose private keys
2. **Input Validation**: All inputs are validated before processing
3. **Rate Limiting**: API endpoints are rate-limited to prevent abuse
4. **HTTPS**: Always use HTTPS in production
5. **CORS**: Properly configured CORS headers
6. **SQL Injection**: Use parameterized queries for database operations

## Testing

### Unit Tests
```bash
go test ./core/smart_contract/...
```

### Integration Tests
```bash
go test -tags=integration ./...
```

### Testnet Testing
```bash
export BITCOIN_NETWORK=testnet
go run test_testnet.go
```

## Deployment

### Docker
```bash
docker build -t stargate-backend .
docker run -p 3001:3001 -e BITCOIN_NETWORK=testnet stargate-backend
```

### Kubernetes
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stargate-backend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: stargate-backend
  template:
    metadata:
      labels:
        app: stargate-backend
    spec:
      containers:
      - name: backend
        image: stargate-backend:latest
        ports:
        - containerPort: 3001
        env:
        - name: BITCOIN_NETWORK
          value: "mainnet"
```

## Monitoring

### Metrics
- Request rate and response times
- Bitcoin API connection status
- Smart contract creation rate
- Transaction monitoring statistics
- Error rates by type

### Health Checks
- `/api/health` endpoint
- Database connectivity
- External API connectivity
- Service availability

## Support

For support and questions:
- Documentation: `docs/API_DOCUMENTATION.md`
- Issues: GitHub repository issues
- Status: System status page

---

**Note**: This API documentation covers the complete Bitcoin Smart Contract System. For specific implementation details, refer to the individual component documentation files.