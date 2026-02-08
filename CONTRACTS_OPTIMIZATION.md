# Stargate Frontend Contracts Route Optimization

## Problem
The `/contracts` route was causing a storm of API calls:
- Initial call: `GET /api/data/block-summaries?limit=12&cursor_height=*`
- For each block with `smart_contract_count > 0`: `GET /api/data/block-inscriptions/{height}`
- Result: 1 + N API calls per page, where N = number of blocks with contracts

## Solution Overview
Optimized to use direct Postgres queries with cursor-based pagination:
- New API endpoint: `GET /api/data/contracts-with-pagination`
- Single query to mcp_contracts table
- Cursor-based pagination by confirmation date
- Result: 1 API call per page regardless of number of contracts

## Files Modified

### Backend - Database Schema
**`backend/storage/smart_contract/schema_manager.go`**
- Added `confirmed_block_height INTEGER` column to mcp_contracts table
- Added `confirmed_at TIMESTAMP WITH TIME ZONE` column to mcp_contracts table
- Added `idx_mcp_contracts_confirmed_at` index for efficient date-based queries

### Backend - Data Types
**`backend/core/smart_contract/types.go`**
- Added `ConfirmedBlockHeight int` field to Contract struct
- Added `ConfirmedAt *time.Time` field to Contract struct
- Added `ConfirmedAt *time.Time` field to ContractFilter for date-based filtering
- Added `CursorDate *time.Time` field to ContractFilter for cursor pagination
- Added `CursorType string` field to ContractFilter ('before' or 'after')

### Backend - Repository Layer
**`backend/storage/smart_contract/contracts_repository.go`**
- Updated `List()` method with cursor-based pagination logic:
  - Filters contracts by confirmation date (before/after cursor)
  - Orders by `confirmed_at DESC` for chronological display
  - Uses limit parameter for pagination
- Added `UpdateContractStatusWithConfirmation()` method to update confirmation metadata

**`backend/storage/smart_contract/pg_store.go`**
- Updated `ListContracts()` with cursor-based pagination support:
  - Query builder for date-based filtering
  - Proper ordering by confirmed_at
  - Limit-based pagination
- Added `UpdateContractStatusWithConfirmation()` implementation
- Added `UpdateContractConfirmation()` method for explicit confirmation updates

**`backend/storage/smart_contract/memory_store.go`**
- Updated `UpdateContractStatus()` to accept optional `confirmedAt *time.Time` parameter

**`backend/middleware/smart_contract/storage.go`**
- Updated Store interface to support confirmation date in UpdateContractStatus

### Backend - API Layer
**`backend/handlers/handlers.go`**
- Added new handler `HandleGetContractsWithPagination()`
- Supports query parameters:
  - `limit`: Number of contracts per page (default: 12)
  - `cursor_date`: ISO 8601 timestamp for pagination cursor
  - `cursor_type`: 'before' or 'after' cursor direction
- Returns JSON response with contracts array and pagination info

**`backend/stargate_backend.go`**
- Registered new route: `GET /api/data/contracts-with-pagination`

### Frontend - Hooks
**`frontend/src/hooks/useContracts.js`** (NEW FILE)
- Created optimized hook for contract fetching
- Uses cursor-based pagination (cursor_date, cursor_type)
- Provides `loadContracts()` and `loadMore()` methods
- Manages loading states and error handling
- Returns contracts array with pagination metadata

### Frontend - Pages
**`frontend/src/pages/ContractsPage.jsx`**
- Replaced block-based fetching with direct contract queries
- Integrated `useContracts` hook
- Uses cursor-based pagination instead of block-scrolling
- Maintains same UI but with optimized data fetching
- Reduced from N+1 API calls to 1 API call per page

## API Usage Comparison

### Before (Unoptimized)
```
Page 1: 
  1. GET /api/data/block-summaries?limit=12&cursor_height=*
  2. GET /api/data/block-inscriptions/{height1}
  3. GET /api/data/block-inscriptions/{height2}
  ... (N more calls for each block with contracts)
  
Page 2:
  1. GET /api/data/block-summaries?limit=12&cursor_height=xxx
  2. GET /api/data/block-inscriptions/{height1}
  ...
```

### After (Optimized)
```
Page 1:
  1. GET /api/data/contracts-with-pagination?limit=12
  
Page 2:
  1. GET /api/data/contracts-with-pagination?limit=12&cursor_date=2024-01-15T10:30:00Z&cursor_type=before
```

## Performance Impact
- **Before**: 1 + N API calls per page (N = blocks with contracts)
- **After**: 1 API call per page
- **Reduction**: ~90% fewer API calls (assuming average 10 contracts per page)
- **Latency**: Reduced from multiple sequential calls to single database query
- **Scalability**: Database pagination scales better than block-scanning

## Migration Notes
1. The optimization requires the `confirmed_at` field to be populated for existing contracts
2. New contracts will automatically have `confirmed_at` set when processed by BlockMonitor
3. Existing contracts without `confirmed_at` will still appear but at the end of the list
4. To backfill: Run a migration script to set `confirmed_at = block_timestamp` for existing confirmed contracts

## Testing Checklist
- [x] Backend compiles successfully
- [ ] New API endpoint returns contracts with pagination
- [ ] Frontend uses new hook correctly
- [ ] Cursor-based pagination works for next/previous pages
- [ ] Contract details display correctly
- [ ] Infinite scroll works as expected
- [ ] No regression in existing functionality

## Future Enhancements
1. Add caching layer for frequently accessed contracts
2. Implement search/filter by contract title, status, or skills
3. Add sorting options (date, budget, task count)
4. Consider Redis for caching cursor positions
5. Add metrics endpoint to track API call reduction

## Status
✅ All changes implemented and compiling
✅ Backend builds successfully
✅ Frontend components updated
⏳ Ready for testing and deployment
