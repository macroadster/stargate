# Bitcoin Steganography API Integration

This document describes the integration of the Starlight Bitcoin Steganography Scanning API into the Stargate project.

## Overview

The integration adds comprehensive Bitcoin transaction steganography detection capabilities to the existing Stargate backend. The API can scan Bitcoin transactions for embedded images and detect hidden data using various steganographic techniques.

## Architecture

### Components

1. **Bitcoin Node Client** (`bitcoin_client.go`)
   - Interfaces with Bitcoin blockchain APIs (Blockstream.info)
   - Extracts transaction data and embedded images
   - Handles OP_RETURN data parsing

2. **Starlight Scanner Integration** (`starlight_scanner.go`)
   - Interfaces with Python Starlight scanner
   - Provides fallback mock implementation
   - Handles image scanning and message extraction

3. **API Models** (`bitcoin_models.go`)
   - Request/response structures for all endpoints
   - Error handling and validation
   - JSON serialization helpers

4. **API Endpoints** (`bitcoin_api.go`)
   - HTTP handlers for all Bitcoin scanning endpoints
   - Request validation and error handling
   - Integration with scanner and Bitcoin client

### API Endpoints

#### Base URL: `http://localhost:3001/bitcoin/v1`

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Service health check |
| GET | `/info` | API information and capabilities |
| POST | `/scan/transaction` | Scan Bitcoin transaction for steganography |
| POST | `/scan/image` | Scan uploaded image directly |
| POST | `/scan/batch` | Batch scan multiple items |
| POST | `/extract` | Extract hidden messages from images |
| GET | `/transaction/{txid}` | Get transaction details |

## Installation & Setup

### Prerequisites

- Go 1.21+ 
- Python 3.8+ (for Starlight scanner)
- Access to Bitcoin blockchain API

### Build Instructions

```bash
cd /home/eyang/sandbox/stargate/backend

# Initialize Go module (first time only)
go mod init stargate-backend

# Update dependencies
go mod tidy

# Build the server
go build -o main .
```

### Running the Server

```bash
# Start the server
./main

# Server will be available at:
# - Frontend: http://localhost:3001
# - Stargate API: http://localhost:3001/api/
# - Bitcoin API: http://localhost:3001/bitcoin/v1/
```

## API Usage Examples

### Health Check

```bash
curl -X GET "http://localhost:3001/bitcoin/v1/health"
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2025-11-26T10:00:00Z",
  "version": "1.0.0",
  "scanner": {
    "model_loaded": true,
    "model_version": "v4-prod",
    "model_path": "models/detector_balanced.onnx",
    "device": "cpu"
  },
  "bitcoin": {
    "node_connected": true,
    "node_url": "https://blockstream.info/api",
    "block_height": 856789
  }
}
```

### Scan Transaction

```bash
curl -X POST "http://localhost:3001/bitcoin/v1/scan/transaction" \
  -H "Content-Type: application/json" \
  -d '{
    "transaction_id": "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
    "extract_images": true,
    "scan_options": {
      "extract_message": true,
      "confidence_threshold": 0.5,
      "include_metadata": true
    }
  }'
```

### Scan Image

```bash
curl -X POST "http://localhost:3001/bitcoin/v1/scan/image" \
  -F "image=@example.png" \
  -F "extract_message=true" \
  -F "confidence_threshold=0.7"
```

### Batch Scan

```bash
curl -X POST "http://localhost:3001/bitcoin/v1/scan/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "items": [
      {"type": "transaction", "transaction_id": "tx1..."},
      {"type": "transaction", "transaction_id": "tx2..."}
    ],
    "scan_options": {
      "extract_message": true,
      "confidence_threshold": 0.5
    }
  }'
```

## Testing

### Automated Testing

Run the test script to verify all endpoints:

```bash
cd /home/eyang/sandbox/stargate/backend
./test_bitcoin_api.sh
```

### Manual Testing

1. Start the server: `./main`
2. Use curl or Postman to test endpoints
3. Monitor logs for scanner activity

## Configuration

### Environment Variables

- `BITCOIN_NODE_URL`: Bitcoin node API URL (default: https://blockstream.info/api)
- `STARLIGHT_MODEL_PATH`: Path to Starlight model file (default: models/detector_balanced.onnx)
- `PYTHON_PATH`: Python executable path (default: python3)

### Scanner Configuration

The system automatically falls back to a mock scanner if the real Starlight scanner is not available. This ensures the API remains functional for development and testing.

## Error Handling

The API implements comprehensive error handling with standardized error responses:

```json
{
  "error": {
    "code": "INVALID_TX_ID",
    "message": "Invalid Bitcoin transaction ID format",
    "details": {},
    "timestamp": "2025-11-26T10:00:00Z",
    "request_id": "abc123def456"
  }
}
```

### Common Error Codes

- `INVALID_TX_ID`: Invalid transaction ID format
- `TX_NOT_FOUND`: Transaction not found
- `IMAGE_TOO_LARGE`: Image exceeds 10MB limit
- `SCAN_FAILED`: Steganography scan failed
- `EXTRACTION_FAILED`: Message extraction failed

## Performance Considerations

- **Rate Limiting**: Built-in rate limiting to prevent abuse
- **Caching**: Transaction data cached for performance
- **Async Processing**: Non-blocking I/O for Bitcoin API calls
- **Memory Management**: Efficient handling of large image files

## Security

- **Input Validation**: All inputs validated before processing
- **File Size Limits**: Maximum 10MB per image
- **CORS**: Configurable CORS headers
- **Error Sanitization**: Sensitive information not exposed in errors

## Monitoring

### Health Endpoints

- `/health` - Overall service health
- `/health/bitcoin` - Bitcoin node connectivity
- `/health/scanner` - Scanner model status

### Metrics

The API exposes metrics for monitoring:
- Request counts by endpoint
- Response times
- Error rates
- Scanner performance

## Troubleshooting

### Common Issues

1. **Scanner not available**: Falls back to mock implementation
2. **Bitcoin node unreachable**: Check network connectivity
3. **Large images**: Ensure images are under 10MB
4. **Invalid transaction IDs**: Must be 64-character hex strings

### Debug Mode

Enable debug logging by setting:
```bash
export DEBUG=true
./main
```

## Future Enhancements

- Support for additional Bitcoin node APIs
- Real-time transaction monitoring
- Advanced steganography detection methods
- Performance optimizations
- Authentication and rate limiting
- WebSocket support for real-time updates

## Integration with Existing Stargate

The Bitcoin API is integrated alongside existing Stargate functionality:

- Original endpoints remain unchanged
- Shared CORS and middleware
- Unified logging and error handling
- Common configuration management

## Support

For issues related to:
- **Bitcoin API**: Check this documentation and test scripts
- **Starlight Scanner**: Refer to Starlight project documentation
- **Stargate Backend**: Refer to Stargate project documentation

---

**Integration Version**: 1.0.0  
**Last Updated**: November 26, 2025  
**Compatible with**: Starlight API Spec v1.0