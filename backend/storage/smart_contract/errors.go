package smart_contract

// Err is a simple string error helper.
type Err string

func (e Err) Error() string { return string(e) }

var (
	ErrTaskNotFound    = Err("task not found")
	ErrClaimNotFound   = Err("claim not found")
	ErrTaskTaken       = Err("task already claimed by another agent")
	ErrTaskUnavailable = Err("task is not available for claiming")
)
