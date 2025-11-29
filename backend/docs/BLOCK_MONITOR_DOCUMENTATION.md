# Bitcoin Block Monitor - Comprehensive Data Extraction System

## Overview

The Bitcoin Block Monitor is a comprehensive background monitoring system that continuously watches for new Bitcoin blocks and performs extensive data extraction and analysis. It creates persistent, searchable records of blockchain data with a focus on steganographic content detection.

## Features

### Real-Time Monitoring
- **60-second interval checks** for new Bitcoin blocks
- **Automatic height comparison** to detect new blocks
- **Graceful error handling** with retry mechanisms
- **Background goroutine** that runs continuously

### Comprehensive Data Extraction
For each new block, the system extracts:

1. **Block Header Data**
   - Block hash, height, timestamp
   - Previous hash, merkle root
   - Difficulty, nonce, size
   - Transaction count

2. **Transaction Data**
   - Complete transaction details
   - Input/output information
   - Witness data presence
   - Fee calculations

3. **Witness Data Analysis**
   - Extract all witness data from transactions
   - Identify image signatures in witness data
   - Extract text content and hex data
   - Categorize data types

4. **Image Extraction**
   - Extract images from witness data
   - Support for JPEG, PNG, GIF, WebP formats
   - Save images to organized directory structure
   - Generate image hashes for identification

5. **Ordinal Inscriptions**
   - Fetch inscription data from Hiro API
   - Filter by block height
   - Extract content type and metadata
   - Identify image vs text inscriptions

6. **Steganography Analysis**
   - AI-powered steganography detection
   - Multiple steganography method detection
   - Confidence scoring and probability analysis
   - Hidden message extraction

7. **Smart Contract Creation**
   - Automatic smart contract creation for stego content
   - Comprehensive metadata storage
   - Global contract array updates
   - Persistent storage

## File Structure

The system creates an organized directory structure for each block:

```
blocks/
├── 925456_00000000/
│   ├── 925456_block.json          # Complete block data
│   ├── transactions.json          # All transaction data
│   ├── witness_data.json          # Extracted witness data
│   ├── extracted_images.json      # Image metadata
│   ├── inscriptions.json         # Ordinal inscriptions
│   ├── smart_contracts.json      # Stego smart contracts
│   ├── metadata.json             # Block statistics
│   └── images/                  # Extracted image files
│       ├── tx123_img_0.png
│       ├── tx123_img_1.webp
│       └── ...
└── 925457_00000001/
    └── ...
```

## API Endpoints

### Block Monitor Control
- `GET /api/block-monitor/status` - Get monitor status and statistics
- `POST /api/block-monitor/start` - Start the block monitor
- `POST /api/block-monitor/stop` - Stop the block monitor

### Status Response Example
```json
{
  "is_running": true,
  "current_height": 925456,
  "last_checked": "2024-01-01T12:00:00Z",
  "blocks_processed": 150,
  "total_transactions": 375000,
  "total_images": 1250,
  "total_stego_contracts": 45,
  "total_inscriptions": 280,
  "last_process_time": "2.5s",
  "check_interval": "1m0s",
  "blocks_directory": "blocks"
}
```

## Data Models

### BlockData Structure
```go
type BlockData struct {
    BlockHeader       BlockHeader            `json:"block_header"`
    Transactions      []TransactionData      `json:"transactions"`
    WitnessData       []WitnessData          `json:"witness_data"`
    ExtractedImages   []ExtractedImageData   `json:"extracted_images"`
    Inscriptions      []InscriptionData      `json:"inscriptions"`
    SmartContracts    []SmartContractData    `json:"smart_contracts"`
    Metadata          BlockMetadata          `json:"metadata"`
    ProcessingInfo    ProcessingInfo         `json:"processing_info"`
}
```

### Smart Contract Creation
When steganography is detected with high confidence (>0.7), the system automatically creates smart contracts:

```json
{
  "contract_id": "stego_1234567890abcdef_0",
  "block_height": 925456,
  "tx_id": "1234567890abcdef...",
  "image_index": 0,
  "contract_type": "steganographic",
  "stego_image": "/blocks/925456_00000000/images/tx123_img_0.png",
  "stego_type": "lsb.rgb",
  "confidence": 0.85,
  "extracted_message": "Hidden message here",
  "detection_method": "starlight_ai_scanner",
  "created_at": 1700678400
}
```

## Configuration

### Default Settings
- **Check Interval**: 60 seconds
- **Max Retries**: 3
- **Retry Delay**: 5 seconds
- **Stego Confidence Threshold**: 0.7
- **Blocks Directory**: `blocks/`

### API Sources
- **Blockstream API**: Primary block and transaction data
- **Hiro API**: Ordinal inscription data
- **Mempool.space**: Fallback block data

## Performance Considerations

### Optimization Features
1. **Concurrent Processing**: Multiple goroutines for data extraction
2. **Caching**: Block statistics caching to avoid repeated API calls
3. **Rate Limiting**: Respectful API request handling
4. **Error Recovery**: Robust error handling with retries
5. **Memory Management**: Efficient data structure usage

### Resource Usage
- **Memory**: Moderate (stores current block data in memory)
- **Disk**: Growing (stores all processed blocks)
- **Network**: Active (continuous API polling)
- **CPU**: Medium (steganography analysis)

## Monitoring and Logging

### Log Messages
The system provides comprehensive logging:
```
INFO: Block monitor started. Current height: 925456, checking every 1m0s
INFO: Found 2 new blocks (from 925457 to 925458)
INFO: Processing block 925457...
INFO: Extracting data for 2847 transactions in block 925457
INFO: Completed processing block 925457 in 2.5s: 2847 txs, 12 images, 1 stego contracts, 5 inscriptions
```

### Statistics Tracking
- Blocks processed count
- Total transactions processed
- Total images extracted
- Stego contracts created
- Inscriptions found
- Processing times

## Error Handling

### Robust Error Recovery
1. **API Failures**: Automatic retries with fallback APIs
2. **Network Issues**: Graceful degradation and retry logic
3. **Data Corruption**: Validation and error reporting
4. **Storage Issues**: Error logging and continuation

### Fallback Mechanisms
- Multiple API sources for redundancy
- Local caching for offline operation
- Partial data processing on failures
- Error reporting without stopping

## Integration Points

### Existing System Integration
- **Smart Contracts Array**: Updates global smart contracts
- **Stego Scanner**: Uses existing steganography detection
- **Bitcoin Client**: Leverages existing Bitcoin API client
- **File Storage**: Integrates with existing upload system

### External APIs
- **Blockstream.info**: Primary blockchain data
- **Hiro.so**: Ordinal inscriptions
- **Mempool.space**: Fallback blockchain data

## Security Considerations

### Data Privacy
- **Local Storage**: All data stored locally
- **No External Sharing**: Data not transmitted externally
- **Hash Identification**: Uses hashes for image identification

### API Security
- **Rate Limiting**: Respects API rate limits
- **Error Handling**: Doesn't expose sensitive error details
- **Request Validation**: Validates all API responses

## Testing

### Test Script
Run the comprehensive test script:
```bash
./test_block_monitor.sh
```

### Test Coverage
- Backend health checks
- Block monitor status
- Bitcoin API connectivity
- Inscription API testing
- Data structure validation
- File structure creation
- Steganography scanner integration
- Real-time monitoring simulation
- Performance metrics

## Troubleshooting

### Common Issues

1. **Block Monitor Not Starting**
   - Check Bitcoin API connectivity
   - Verify directory permissions
   - Check for port conflicts

2. **Missing Images**
   - Verify witness data extraction
   - Check image format support
   - Validate storage permissions

3. **Stego Scanner Issues**
   - Ensure Python backend is running on port 8080
   - Check scanner initialization
   - Verify model loading

4. **API Rate Limits**
   - Monitor API response times
   - Check for rate limit errors
   - Implement backoff strategies

### Debug Mode
Enable detailed logging by setting log level:
```go
log.SetFlags(log.LstdFlags | log.Lshortfile)
```

## Future Enhancements

### Planned Features
1. **Real-time WebSocket Updates**: Live block notifications
2. **Advanced Filtering**: Custom block filtering criteria
3. **Database Integration**: Store data in PostgreSQL/MongoDB
4. **Distributed Processing**: Multiple worker nodes
5. **Machine Learning**: Improved steganography detection
6. **Web Interface**: Visual block exploration interface

### Scalability Improvements
1. **Horizontal Scaling**: Multiple monitor instances
2. **Load Balancing**: Distribute processing load
3. **Caching Layer**: Redis for frequently accessed data
4. **Message Queue**: RabbitMQ for task distribution

## License and Support

This system is part of the Stargate project and follows the same licensing terms. For support and questions, refer to the project documentation and issue tracking.