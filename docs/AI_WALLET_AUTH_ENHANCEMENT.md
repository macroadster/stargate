# Enhanced Wallet Authentication for AI Agents

## Overview

The wallet authentication system has been enhanced to provide better support for AI agents with:

- **AI-friendly challenge modes** with higher attempt limits
- **Detailed error reporting** for debugging signature issues
- **Address validation tools** for wallet debugging
- **Enhanced signature format detection** supporting multiple Bitcoin wallet types

## New Features

### 1. AI-Friendly Authentication Mode

AI agents can request higher attempt limits by using `ai_mode: true` in the `get_auth_challenge` call:

```json
{
  "method": "tools/call",
  "params": {
    "name": "get_auth_challenge",
    "arguments": {
      "wallet_address": "tb1qexample...",
      "ai_mode": true
    }
  }
}
```

**Benefits:**
- 20 attempt limit instead of 5 (standard mode)
- Extended challenge duration (same TTL, more attempts)
- Better suited for automated debugging workflows

### 2. Detailed Verification Error Reporting

The `verify_auth_challenge` tool now supports `detailed: true` parameter:

```json
{
  "method": "tools/call", 
  "params": {
    "name": "verify_auth_challenge",
    "arguments": {
      "wallet_address": "tb1qexample...",
      "signature": "base64_or_hex_signature...",
      "detailed": true
    }
  }
}
```

**Response includes:**
- Remaining attempts count
- Signature format detection (legacy vs BIP-322)
- Specific error messages for debugging
- Hex decoding status

### 3. Address Validation Tool

New `validate_address` tool for wallet debugging:

```json
{
  "method": "tools/call",
  "params": {
    "name": "validate_address",
    "arguments": {
      "address": "tb1qexample..."
    }
  }
}
```

**Response includes:**
- Address validity status
- Address type (P2PKH, P2WPKH, P2TR, etc.)
- Network detection (mainnet, testnet4, testnet3)
- Specific error messages

## Enhanced Error Handling

### Standard Mode (Existing)
```json
{
  "error": {
    "message": "Invalid signature",
    "code": "VALIDATION_ERROR"
  }
}
```

### Detailed Mode (New)
```json
{
  "verified": false,
  "reason": "Signature verification failed",
  "details": {
    "remaining_attempts": 15,
    "signature_info": {
      "success": false,
      "format": "none",
      "message": "08d0ff0d35038832e4ddecdcee21baa5",
      "legacy_errors": ["Invalid signature format"],
      "bip322_errors": ["Invalid witness stack"]
    }
  }
}
```

## Supported Signature Formats

### Legacy SignMessage (Compact)
- Base64-encoded compact signatures
- Bitcoin Core `signmessage` compatible
- Works with most Bitcoin wallets
- Supports both compressed and uncompressed keys

### BIP-322 Simple
- Witness-based signatures
- Native segwit support
- More modern and secure
- Bitcoin Core 22.0+ compatible

### Format Auto-Detection
The system automatically tries multiple formats:
1. Legacy signmessage with original message
2. BIP-322 simple with original message  
3. Legacy signmessage with hex-decoded message
4. BIP-322 simple with hex-decoded message

## Address Support

### Supported Address Types
- **P2PKH** (legacy): `1...`, `m...`, `n...`
- **P2SH** (nested segwit): `3...`, `2...`
- **P2WPKH** (native segwit): `bc1q...`, `tb1q...`
- **P2WSH** (multisig): `bc1q...`, `tb1q...`
- **P2TR** (taproot): `bc1p...`, `tb1p...`

### Supported Networks
- **Mainnet**: Production Bitcoin network
- **Testnet4**: Current Bitcoin test network (preferred)
- **Testnet3**: Legacy Bitcoin test network
- **Regtest**: Regression testing network

## AI Agent Integration Guide

### Step 1: Validate Address (Optional)
```json
{
  "name": "validate_address",
  "arguments": {"address": "tb1qyouraddress..."}
}
```

### Step 2: Get Challenge (AI Mode)
```json
{
  "name": "get_auth_challenge", 
  "arguments": {
    "wallet_address": "tb1qyouraddress...",
    "ai_mode": true
  }
}
```

### Step 3: Sign Challenge
Use your Bitcoin wallet to sign the returned nonce. Most wallets support:
- Legacy signmessage: `signmessage "tb1qyouraddress..." "08d0ff0d35..."`
- BIP-322: Newer signature format (if supported)

### Step 4: Verify (Detailed Mode)
```json
{
  "name": "verify_auth_challenge",
  "arguments": {
    "wallet_address": "tb1qyouraddress...",
    "signature": "your_signature...",
    "detailed": true
  }
}
```

### Step 5: Use API Key
On success, you'll receive an API key for authenticated requests:
```json
{
  "verified": true,
  "api_key": "sk-...",
  "wallet": "tb1qyouraddress...",
  "email": "optional@email.com"
}
```

## Troubleshooting Guide

### Common Issues and Solutions

#### "Signature verification failed"
- **Cause**: Signature format or message encoding issue
- **Solution**: Use `detailed: true` to see specific format errors
- **Check**: Are you using the exact nonce from step 2?

#### "No active challenge found"
- **Cause**: Challenge expired or was consumed
- **Solution**: Request a new challenge with `get_auth_challenge`
- **Note**: Challenges expire after 10 minutes

#### "Invalid address format"  
- **Cause**: Address format not recognized
- **Solution**: Use `validate_address` tool to check format
- **Check**: Support for your address type/network

#### "Maximum attempts exceeded"
- **Cause**: Too many failed verification attempts
- **Solution**: Request a new challenge with `ai_mode: true`
- **Benefit**: AI mode provides 20 attempts vs 5 standard

## Rate Limiting

### Standard Limits
- **100 requests per minute** per API key
- **10 requests per second** sustainable rate
- **5 verification attempts** per challenge (standard mode)

### AI-Enhanced Limits
- **Same API rate limits** (100/minute, 10/second)
- **20 verification attempts** per challenge (AI mode)
- **Same 10-minute TTL** for challenges

## Migration Notes

### Existing Implementations
- **No breaking changes** to existing authentication flows
- **Backward compatible** with all current wallet integrations
- **Optional features** can be ignored if not needed

### Recommended Updates for AI Agents
1. **Add `ai_mode: true`** to `get_auth_challenge` calls
2. **Add `detailed: true`** to `verify_auth_challenge` calls  
3. **Use `validate_address`** for pre-flight address validation
4. **Handle detailed error responses** for better debugging

## Security Considerations

### Enhanced Error Reporting
- **Never exposes private keys** in error messages
- **Only verification metadata** is included in detailed responses
- **Rate limiting still applies** to prevent abuse

### Challenge Security
- **Cryptographic nonces** prevent replay attacks
- **Time-based expiration** limits window for attacks
- **Attempt limiting** prevents brute force attacks

## Development and Testing

### Testing New Features
```bash
# Run authentication tests
cd backend && go test ./handlers/ -v
cd backend && go test ./mcp/ -v

# Build with enhancements
cd backend && go build
```

### Debug Mode
For development, enable detailed logging:
```go
// In your MCP client configuration
{
  "detailed": true,      // Enable detailed auth errors
  "ai_mode": true        // Use AI-friendly limits
}
```

## API Reference

### get_auth_challenge
- **Purpose**: Get cryptographic challenge for wallet verification
- **Parameters**:
  - `wallet_address` (required): Bitcoin wallet address
  - `ai_mode` (optional): Enable AI-friendly mode with 20 attempts
- **Response**: Challenge with nonce, expiration, and attempt limits

### verify_auth_challenge  
- **Purpose**: Verify signature and issue API key
- **Parameters**:
  - `wallet_address` (required): Bitcoin wallet address
  - `signature` (required): Bitcoin signature of challenge nonce
  - `email` (optional): Email for account recovery
  - `detailed` (optional): Enable detailed error reporting
- **Response**: API key and verification status

### validate_address
- **Purpose**: Validate Bitcoin address and get metadata
- **Parameters**:
  - `address` (required): Bitcoin address to validate
- **Response**: Address validity, type, network, and error details

## Conclusion

The enhanced wallet authentication system provides better support for AI agents through:

- **Higher attempt limits** for automated workflows
- **Detailed error reporting** for debugging issues  
- **Address validation tools** for pre-flight checks
- **Enhanced format detection** for wallet compatibility

These improvements maintain backward compatibility while providing AI agents with the tools they need for robust, automated authentication workflows.