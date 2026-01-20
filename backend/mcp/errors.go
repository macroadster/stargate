package mcp

import (
	"fmt"
	"strings"
)

// ToolError represents a structured error from tool execution
type ToolError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Tool       string                 `json:"tool,omitempty"`
	Field      string                 `json:"field,omitempty"`
	FieldValue interface{}            `json:"field_value,omitempty"`
	Hint       string                 `json:"hint,omitempty"`
	DocsURL    string                 `json:"docs_url,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
	HttpStatus int                    `json:"http_status,omitempty"`
}

func (e *ToolError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (field: %s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidationError represents field-level validation errors
type ValidationError struct {
	Tool       string                 `json:"tool"`
	Message    string                 `json:"message"`
	Fields     map[string]*FieldError `json:"fields"`
	DocsURL    string                 `json:"docs_url,omitempty"`
	Hint       string                 `json:"hint,omitempty"`
	HttpStatus int                    `json:"http_status,omitempty"`
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return e.Message
	}

	fieldNames := make([]string, 0, len(e.Fields))
	for field := range e.Fields {
		fieldNames = append(fieldNames, field)
	}
	return fmt.Sprintf("%s: invalid fields: %s", e.Message, strings.Join(fieldNames, ", "))
}

// FieldError represents validation error for a specific field
type FieldError struct {
	Value     interface{} `json:"value"`
	Message   string      `json:"message"`
	Expected  string      `json:"expected,omitempty"`
	Required  bool        `json:"required"`
	FieldType string      `json:"type,omitempty"`
}

// Tool-specific error code constants
const (
	// Validation error codes
	ErrCodeMissingRequired  = "MISSING_REQUIRED_FIELD"
	ErrCodeInvalidType      = "INVALID_FIELD_TYPE"
	ErrCodeInvalidValue     = "INVALID_FIELD_VALUE"
	ErrCodeValidationFailed = "VALIDATION_FAILED"

	// Business logic error codes
	ErrCodeNotFound      = "RESOURCE_NOT_FOUND"
	ErrCodeAlreadyExists = "RESOURCE_ALREADY_EXISTS"
	ErrCodeConflict      = "CONFLICT"
	ErrCodeUnauthorized  = "UNAUTHORIZED"
	ErrCodeForbidden     = "FORBIDDEN"
	ErrCodeRateLimited   = "RATE_LIMITED"

	// Infrastructure error codes
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeInternalError      = "INTERNAL_ERROR"
	ErrCodeBadGateway         = "BAD_GATEWAY"

	// Tool-specific prefixes
	ToolPrefixClaimTask       = "CLAIM_TASK"
	ToolPrefixCreateProposal  = "CREATE_PROPOSAL"
	ToolPrefixSubmitWork      = "SUBMIT_WORK"
	ToolPrefixApproveProposal = "APPROVE_PROPOSAL"
	ToolPrefixCreateWish      = "CREATE_WISH"
)

// Helper functions to create common error types

// NewValidationError creates a validation error for missing/invalid fields
func NewValidationError(tool, message string) *ValidationError {
	return &ValidationError{
		Tool:       tool,
		Message:    message,
		Fields:     make(map[string]*FieldError),
		HttpStatus: 400,
	}
}

// AddFieldError adds a field-level validation error
func (e *ValidationError) AddFieldError(fieldName string, value interface{}, message string, required bool) {
	e.Fields[fieldName] = &FieldError{
		Value:    value,
		Message:  message,
		Required: required,
	}
}

// AddTypeError adds a type validation error
func (e *ValidationError) AddTypeError(fieldName string, value interface{}, expectedType string) {
	e.Fields[fieldName] = &FieldError{
		Value:     value,
		Message:   fmt.Sprintf("Expected type %s", expectedType),
		Expected:  expectedType,
		FieldType: "type",
	}
}

// HasErrors returns true if validation errors exist
func (e *ValidationError) HasErrors() bool {
	return len(e.Fields) > 0
}

// ToToolError converts ValidationError to ToolError (for single field errors)
func (e *ValidationError) ToToolError() *ToolError {
	if len(e.Fields) == 0 {
		return &ToolError{
			Code:       ErrCodeValidationFailed,
			Message:    e.Message,
			Tool:       e.Tool,
			HttpStatus: e.HttpStatus,
		}
	}

	// For multiple field errors, return the first one as primary error
	var firstField string
	var firstError *FieldError
	for field, err := range e.Fields {
		firstField = field
		firstError = err
		break
	}

	code := ErrCodeValidationFailed
	if firstError.Required {
		code = ErrCodeMissingRequired
	} else if firstError.FieldType == "type" {
		code = ErrCodeInvalidType
	} else {
		code = ErrCodeInvalidValue
	}

	return &ToolError{
		Code:       code,
		Message:    firstError.Message,
		Tool:       e.Tool,
		Field:      firstField,
		FieldValue: firstError.Value,
		Hint:       e.Hint,
		DocsURL:    e.DocsURL,
		HttpStatus: e.HttpStatus,
		Details: map[string]interface{}{
			"all_errors": e.Fields,
		},
	}
}

// Tool-specific error creators

// NewMissingFieldError creates an error for missing required field
func NewMissingFieldError(tool, field string) *ToolError {
	return &ToolError{
		Code:       ErrCodeMissingRequired,
		Message:    fmt.Sprintf("Field '%s' is required", field),
		Tool:       tool,
		Field:      field,
		HttpStatus: 400,
		Hint:       fmt.Sprintf("Add '%s' to your request parameters", field),
	}
}

// NewNotFoundError creates a resource not found error
func NewNotFoundError(tool, resourceType, resourceID string) *ToolError {
	return &ToolError{
		Code:       ErrCodeNotFound,
		Message:    fmt.Sprintf("%s '%s' not found", resourceType, resourceID),
		Tool:       tool,
		HttpStatus: 404,
		Hint:       fmt.Sprintf("Verify the %s ID is correct", strings.ToLower(resourceType)),
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(tool, message string) *ToolError {
	if message == "" {
		message = "Authentication required"
	}
	return &ToolError{
		Code:       ErrCodeUnauthorized,
		Message:    message,
		Tool:       tool,
		HttpStatus: 401,
		Hint:       "Provide a valid API key in X-API-Key header or Authorization: Bearer <key>",
	}
}

// NewConflictError creates a conflict error
func NewConflictError(tool, message string) *ToolError {
	return &ToolError{
		Code:       ErrCodeConflict,
		Message:    message,
		Tool:       tool,
		HttpStatus: 409,
	}
}

// NewServiceUnavailableError creates a service unavailable error
func NewServiceUnavailableError(tool, service string) *ToolError {
	return &ToolError{
		Code:       ErrCodeServiceUnavailable,
		Message:    fmt.Sprintf("%s service is unavailable", service),
		Tool:       tool,
		HttpStatus: 503,
		Hint:       "Try again later or contact support if the issue persists",
	}
}

// NewInternalError creates an internal server error
func NewInternalError(tool, message string) *ToolError {
	if message == "" {
		message = "Internal server error"
	}
	return &ToolError{
		Code:       ErrCodeInternalError,
		Message:    message,
		Tool:       tool,
		HttpStatus: 500,
		Hint:       "Please try again. If the problem persists, contact support",
	}
}

// Tool-specific error creators with prefixed codes

// NewClaimTaskError creates a claim_task specific error
func NewClaimTaskError(baseCode, message, field string) *ToolError {
	return &ToolError{
		Code:       fmt.Sprintf("%s_%s", ToolPrefixClaimTask, baseCode),
		Message:    message,
		Tool:       "claim_task",
		Field:      field,
		HttpStatus: 400,
	}
}

// NewCreateProposalError creates a create_proposal specific error
func NewCreateProposalError(baseCode, message, field string) *ToolError {
	return &ToolError{
		Code:       fmt.Sprintf("%s_%s", ToolPrefixCreateProposal, baseCode),
		Message:    message,
		Tool:       "create_proposal",
		Field:      field,
		HttpStatus: 400,
	}
}

// NewSubmitWorkError creates a submit_work specific error
func NewSubmitWorkError(baseCode, message, field string) *ToolError {
	return &ToolError{
		Code:       fmt.Sprintf("%s_%s", ToolPrefixSubmitWork, baseCode),
		Message:    message,
		Tool:       "submit_work",
		Field:      field,
		HttpStatus: 400,
	}
}

// NewCreateWishError creates a create_wish specific error
func NewCreateWishError(baseCode, message, field string) *ToolError {
	return &ToolError{
		Code:       fmt.Sprintf("%s_%s", ToolPrefixCreateWish, baseCode),
		Message:    message,
		Tool:       "create_wish",
		Field:      field,
		HttpStatus: 400,
	}
}

// IsToolError checks if error is a ToolError
func IsToolError(err error) (*ToolError, bool) {
	if toolErr, ok := err.(*ToolError); ok {
		return toolErr, true
	}
	return nil, false
}

// IsValidationError checks if error is a ValidationError
func IsValidationError(err error) (*ValidationError, bool) {
	if validationErr, ok := err.(*ValidationError); ok {
		return validationErr, true
	}
	return nil, false
}

// GetHTTPStatusFromError extracts HTTP status from error types
func GetHTTPStatusFromError(err error) int {
	if toolErr, ok := IsToolError(err); ok {
		return toolErr.HttpStatus
	}
	if validationErr, ok := IsValidationError(err); ok {
		return validationErr.HttpStatus
	}
	return 500 // default for unknown errors
}
