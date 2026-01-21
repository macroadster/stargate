#!/usr/bin/env node

// Demonstration of improved MCP error handling
// This script shows the difference between old generic errors and new structured errors

const examples = {
  // Old generic error (what we had before)
  old: {
    success: false,
    error_code: "TOOL_ERROR",
    message: "Tool execution failed", 
    error: "task_id is required",
    code: 400
  },

  // New structured error (what we have now)
  new: {
    success: false,
    error_code: "VALIDATION_FAILED",
    message: "Invalid request parameters",
    error: "task_id is required and must be a string",
    code: 400,
    hint: "Add 'task_id' to your request parameters",
    details: {
      tool: "claim_task",
      validation_errors: {
        task_id: {
          value: null,
          message: "task_id is required and must be a string",
          required: true
        }
      },
      all_errors: {
        task_id: {
          value: null,
          message: "task_id is required and must be a string",
          required: true
        }
      }
    },
    required_fields: ["task_id"],
    timestamp: "2026-01-19T19:08:56-08:00",
    version: "1.0.0"
  }
};

function analyzeError(error, label) {
  console.log(`\n=== ${label} Error Response ===`);
  console.log(JSON.stringify(error, null, 2));
  
  console.log(`\n🔍 AI Analysis:`);
  
  // How an AI would handle the old error
  if (label === 'OLD') {
    console.log(`❌ Generic error code: ${error.error_code}`);
    console.log(`❌ Must parse error message to understand the issue`);
    console.log(`❌ No field-level details`);
    console.log(`❌ No hint for fixing the issue`);
    console.log(`❌ AI needs to use string matching on error message`);
  }
  
  // How an AI handles the new error
  if (label === 'NEW') {
    console.log(`✅ Specific error code: ${error.error_code}`);
    console.log(`✅ Clear field-level validation details`);
    console.log(`✅ Missing fields list: ${error.required_fields.join(', ')}`);
    console.log(`✅ Actionable hint: "${error.hint}"`);
    console.log(`✅ Tool context: ${error.details.tool}`);
    
    // Programmatic error handling
    console.log(`\n🤖 AI can programmatically handle this:`);
    if (error.error_code === 'VALIDATION_FAILED') {
      error.required_fields.forEach(field => {
        console.log(`   → Add required field: ${field}`);
      });
    }
    
    // Field-specific fixes
    Object.entries(error.details.validation_errors).forEach(([field, err]) => {
      console.log(`   → ${field}: ${err.message}`);
    });
  }
}

console.log('🚀 MCP Error Handling Improvements Demo');
console.log('=====================================');

analyzeError(examples.old, 'OLD');
analyzeError(examples.new, 'NEW');

console.log(`\n📊 Key Improvements for AI Understanding:`);
console.log(`   1. Structured error codes instead of generic "TOOL_ERROR"`);
console.log(`   2. Field-level validation with specific error messages`);
console.log(`   3. Required fields array for easy parsing`);
console.log(`   4. Actionable hints for fixing issues`);
console.log(`   5. Tool context for better error handling`);
console.log(`   6. Timestamps and versioning for debugging`);
console.log(`   7. Consistent JSON structure across all tools`);

console.log(`\n✨ Result: AI can now understand and fix errors programmatically!`);