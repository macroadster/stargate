-- Prune orphan proposals, tasks, claims, and submissions
-- where the referenced contract/wish no longer exists

-- Begin transaction for safety
BEGIN;

-- First, let's see what orphans exist (dry run - comment out BEGIN and ROLLBACK for actual execution)
SELECT 'ORPHAN TASKS' as check_type, COUNT(*) as count
FROM mcp_tasks t
LEFT JOIN mcp_contracts c ON t.contract_id = c.contract_id
WHERE c.contract_id IS NULL

UNION ALL

SELECT 'ORPHAN CLAIMS' as check_type, COUNT(*) as count
FROM mcp_claims cl
LEFT JOIN mcp_tasks t ON cl.task_id = t.task_id
WHERE t.task_id IS NULL

UNION ALL

SELECT 'ORPHAN SUBMISSIONS' as check_type, COUNT(*) as count
FROM mcp_submissions s
LEFT JOIN mcp_claims cl ON s.claim_id = cl.claim_id
WHERE cl.claim_id IS NULL

UNION ALL

SELECT 'ORPHAN PROPOSALS (by contract_id)' as check_type, COUNT(*) as count
FROM mcp_proposals p
LEFT JOIN mcp_contracts c ON (
  (p.metadata->>'contract_id' = c.contract_id OR p.metadata->>'contract_id' = c.contract_id) 
  OR (p.metadata->>'ingestion_id' = c.contract_id)
  OR (p.metadata->>'visible_pixel_hash' = c.contract_id)
  OR (p.id = c.contract_id)
  OR ('wish-' || p.visible_pixel_hash = c.contract_id)
)
WHERE c.contract_id IS NULL
  AND (p.metadata->>'contract_id' IS NOT NULL 
       OR p.metadata->>'ingestion_id' IS NOT NULL 
       OR p.metadata->>'visible_pixel_hash' IS NOT NULL
       OR p.visible_pixel_hash IS NOT NULL);

-- Show sample orphan records for review
SELECT 'SAMPLE ORPHAN CLAIMS' as info, cl.claim_id, cl.task_id
FROM mcp_claims cl
LEFT JOIN mcp_tasks t ON cl.task_id = t.task_id
WHERE t.task_id IS NULL
LIMIT 10;

SELECT 'SAMPLE ORPHAN SUBMISSIONS' as info, s.submission_id, s.claim_id
FROM mcp_submissions s
LEFT JOIN mcp_claims cl ON s.claim_id = cl.claim_id
WHERE cl.claim_id IS NULL
LIMIT 10;

-- =============================================================================
-- DELETE SECTION - Uncomment the following DELETE statements to actually prune
-- =============================================================================

-- Delete orphan submissions (no corresponding claim exists)
-- DELETE FROM mcp_submissions
-- WHERE claim_id NOT IN (SELECT claim_id FROM mcp_claims);

-- Delete orphan claims (no corresponding task exists)
-- DELETE FROM mcp_claims
-- WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks);

-- Delete orphan tasks (no corresponding contract exists)
-- DELETE FROM mcp_tasks
-- WHERE contract_id NOT IN (SELECT contract_id FROM mcp_contracts);

-- Delete orphan escort status (no corresponding task exists)
-- DELETE FROM mcp_escort_status
-- WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks);

-- Delete orphan proposals (those referencing non-existent contracts)
-- Note: Be careful with this - proposals might have standalone value
-- DELETE FROM mcp_proposals
-- WHERE id NOT IN (
--   SELECT DISTINCT p.id
--   FROM mcp_proposals p
--   JOIN mcp_contracts c ON (
--     (p.metadata->>'contract_id' = c.contract_id)
--     OR (p.metadata->>'ingestion_id' = c.contract_id)
--     OR (p.metadata->>'visible_pixel_hash' = c.contract_id)
--     OR (p.id = c.contract_id)
--     OR ('wish-' || p.visible_pixel_hash = c.contract_id)
--   )
-- );

-- Rollback for dry run (remove this line for actual execution)
ROLLBACK;

-- Uncomment the following for actual execution:
-- COMMIT;
