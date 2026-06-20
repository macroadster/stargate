package agents

// Package agents implements stargate's built-in autonomous agent orchestration.
// It provides the coordination logic (proposal creation, task claiming, auditing)
// that was previously only available in the external Python starlight.agents module.
//
// The heavy "thinking" (LLM plan generation, coding, audits) is performed by
// pluggable Executor / Auditor implementations so the core loop can run without
// embedding an LLM.
