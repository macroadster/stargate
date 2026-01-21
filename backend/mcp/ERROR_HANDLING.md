# MCP Tool Error Handling Improvements

## Overview

The MCP tool call error handling has been significantly improved to provide structured, actionable error responses that help AI clients understand and handle errors more effectively.

## Problems Solved

### Before: Generic Error Messages
```json
{
  "success": false,
  "error_code": "TOOL_ERROR", 
  "message": "Tool execution failed",
  "error": "task_id is required"
}
```

**Issues:**
- All errors got the same generic `TOOL_ERROR` code
- Error context was buried in plain text
- No field-level validation details
- Difficult to programmatically handle different error types

### After: Structured Error Responses
```json
{
  "success": false,
  "error_code": "VALIDATION_FAILED",
  "message": "Invalid request parameters", 
  "error": "task_id is required",
  "code": 400,
  "hint": "Add 'task_id' to your request parameters",
  "details": {
    "tool": "claim_task",
    "validation_errors": {
      "task_id": {
        "value": null,
        "message": "task_id is required and must be a string",
        "required": true
      }
    },
    "all_errors": {
      "task_id": {
        "value": null,
        "message": "task_id is required and must be a string", 
        "required": true
      }
    }
  },
  "required_fields": ["task_id"],
  "timestamp": "2026-01-19T19:08:56-08:00",
  "version": "1.0.0"
}
```

**Improvements:**
- Specific error codes for different error types
- Field-level validation details
- Clear hints for fixing the issue
- Structured `required_fields` array for easy parsing
- Tool name included in response
- Timestamp and version for debugging

## Error Types and Codes

### Validation Errors
- `MISSING_REQUIRED_FIELD` - Required parameter is missing
- `INVALID_FIELD_TYPE` - Parameter has wrong data type  
- `INVALID_FIELD_VALUE` - Parameter value is invalid
- `VALIDATION_FAILED` - General validation failure

### Business Logic Errors
- `RESOURCE_NOT_FOUND` - Requested resource doesn't exist
- `RESOURCE_ALREADY_EXISTS` - Resource already exists
- `CONFLICT` - Operation conflicts with existing state
- `UNAUTHORIZED` - Authentication required or invalid
- `FORBIDDEN` - Operation not permitted
- `RATE_LIMITED` - Too many requests

### Infrastructure Errors  
- `SERVICE_UNAVAILABLE` - External service down
- `INTERNAL_ERROR` - Unexpected server error
- `BAD_GATEWAY` - Upstream service error

### Tool-Specific Error Codes
Tools now have prefixed error codes for better categorization:

- `CLAIM_TASK_MISSING_REQUIRED_FIELD`
- `CREATE_PROPOSAL_VALIDATION_FAILED` 
- `SUBMIT_WORK_INVALID_FIELD_TYPE`
- `APPROVE_PROPOSAL_UNAUTHORIZED`
- `CREATE_CONTRACT_INSCRIBE_ERROR`

## Field-Level Validation Details

### Multiple Field Errors
```json
{
  "success": false,
  "error_code": "VALIDATION_FAILED",
  "message": "Invalid request parameters",
  "details": {
    "tool": "create_proposal", 
    "validation_errors": {
      "title": {
        "value": "",
        "message": "title is required and must be a non-empty string",
        "required": true
      },
      "visible_pixel_hash": {
        "value": "",
        "message": "visible_pixel_hash is required and must be a string", 
        "required": true
      },
      "budget_sats": {
        "value": -100,
        "message": "budget_sats must be a non-negative number",
        "required": false
      }
    }
  },
  "required_fields": ["title", "visible_pixel_hash"]
}
```

### Type Errors
```json
{
  "task_id": {
    "value": 123,
    "message": "Expected type string",
    "expected": "string",
    "field_type": "type",
    "required": true
  }
}
```

## Error Response Structure

All error responses now include these fields:

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | Always `false` for errors |
| `error_code` | string | Specific error code for programmatic handling |
| `message` | string | Human-readable error summary |
| `error` | string | Primary error details |
| `code` | number | HTTP status code |
| `hint` | string | Actionable hint for fixing the error |
| `details` | object | Structured error details (optional) |
| `required_fields` | array | List of missing required fields (optional) |
| `timestamp` | string | ISO 8601 timestamp |
| `version` | string | API version |

## Tool Handler Improvements

### Enhanced Validation
Each tool now validates inputs and returns structured errors:

```go
func (h *HTTPMCPServer) handleClaimTask(ctx context.Context, args map[string]interface{}, apiKey string) (interface{}, error) {
    validation := NewValidationError("claim_task", "Invalid request parameters")
    
    taskID, ok := args["task_id"].(string)
    if !ok || taskID == "" {
        validation.AddFieldError("task_id", args["task_id"], "task_id is required and must be a string", true)
    }
    
    if validation.HasErrors() {
        return nil, validation
    }
    
    // ... proceed with valid inputs
}
```

### Error Conversion
Common errors are converted to structured responses:

```go
claim, err := h.store.ClaimTask(taskID, wallet, nil)
if err != nil {
    if strings.Contains(err.Error(), "not found") {
        return nil, NewNotFoundError("claim_task", "task", taskID)
    }
    if strings.Contains(err.Error(), "already claimed") {
        return nil, NewClaimTaskError("ALREADY_CLAIMED", "Task has already been claimed", "task_id")
    }
    return nil, err
}
```

## Benefits for AI Clients

### 1. Better Error Understanding
AI can distinguish between validation errors, business logic errors, and infrastructure issues using specific error codes.

### 2. Easier Error Recovery  
Field-level validation details tell AI exactly what to fix:
```json
"required_fields": ["task_id"],
"validation_errors": {
  "task_id": {
    "message": "task_id is required and must be a string"
  }
}
```

### 3. Structured Handling
AI can programmatically handle errors based on codes:
```javascript
if (response.error_code.startsWith('CLAIM_TASK_')) {
  // Handle claim_task specific errors
} else if (response.error_code === 'VALIDATION_FAILED') {
  // Fix validation errors
  response.required_fields.forEach(field => addField(field));
}
```

### 4. Clear Documentation
Error responses include hints and tool context:
```json
{
  "details": {
    "tool": "claim_task"
  },
  "hint": "Add 'task_id' to your request parameters"
}
```

## Migration Guide

### For AI Clients
1. **Check `error_code` instead of parsing error messages**
2. **Use `required_fields` array to identify missing parameters**  
3. **Examine `validation_errors` for field-specific issues**
4. **Handle tool-specific error codes with appropriate fallbacks**

### Error Handling Pattern
```javascript
if (!response.success) {
  switch (response.error_code) {
    case 'VALIDATION_FAILED':
      // Fix validation issues
      response.required_fields?.forEach(field => {
        console.log(`Add required field: ${field}`);
      });
      break;
      
    case 'RESOURCE_NOT_FOUND':
      // Handle missing resources
      retryWithDifferentId();
      break;
      
    case 'UNAUTHORIZED':
      // Handle auth issues  
      authenticateUser();
      break;
      
    default:
      // Generic error handling
      console.error(response.error);
  }
}
```

## Testing

Comprehensive tests ensure error handling works correctly:

- ✅ Field-level validation errors
- ✅ Multiple field validation errors  
- ✅ Type validation errors
- ✅ Resource not found errors
- ✅ Service unavailable errors
- ✅ Error response structure validation
- ✅ Backward compatibility with existing tests

## Backward Compatibility

The improved error handling maintains backward compatibility:
- Existing fields (`success`, `error`, `code`) remain unchanged
- New fields are added as optional enhancements
- Error responses remain valid JSON
- Existing integrations continue to work

This upgrade provides significantly better error context for AI clients while maintaining compatibility with existing systems.