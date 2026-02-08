-- Prune orphan proposals, tasks, claims, and submissions
-- where the referenced contract/wish no longer exists
-- 
-- EXECUTION VERSION - This will actually delete data!
-- Run: psql -U postgres -d stargate -f scripts/prune_orphans_exec.sql

-- Begin transaction
BEGIN;

-- Show what will be deleted
SELECT 'ORPHAN TASKS TO DELETE' as check_type, COUNT(*) as count
FROM mcp_tasks t
LEFT JOIN mcp_contracts c ON t.contract_id = c.contract_id
WHERE c.contract_id IS NULL

UNION ALL

SELECT 'ORPHAN CLAIMS TO DELETE' as check_type, COUNT(*) as count
FROM mcp_claims cl
LEFT JOIN mcp_tasks t ON cl.task_id = t.task_id
WHERE t.task_id IS NULL

UNION ALL

SELECT 'ORPHAN SUBMISSIONS TO DELETE' as check_type, COUNT(*) as count
FROM mcp_submissions s
LEFT JOIN mcp_claims cl ON s.claim_id = cl.claim_id
WHERE cl.claim_id IS NULL

UNION ALL

SELECT 'ORPHAN PROPOSALS TO DELETE (by contract_id)' as check_type, COUNT(*) as count
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

-- Show samples
SELECT 'SAMPLE ORPHAN CLAIMS' as info, cl.claim_id, cl.task_id
FROM mcp_claims cl
LEFT JOIN mcp_tasks t ON cl.task_id = t.task_id
WHERE t.task_id IS NULL;

SELECT 'SAMPLE ORPHAN SUBMISSIONS' as info, s.submission_id, s.claim_id
FROM mcp_submissions s
LEFT JOIN mcp_claims cl ON s.claim_id = cl.claim_id
WHERE cl.claim_id IS NULL;

-- =============================================================================
-- DELETE ORPHANS (in dependency order: submissions -> claims -> tasks)
-- =============================================================================

-- 1. Delete orphan submissions (no corresponding claim exists)
DELETE FROM mcp_submissions
WHERE claim_id NOT IN (SELECT claim_id FROM mcp_claims);

-- 2. Delete orphan claims (no corresponding task exists)
DELETE FROM mcp_claims
WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks);

-- 3. Delete orphan tasks (no corresponding contract exists)
DELETE FROM mcp_tasks
WHERE contract_id NOT IN (SELECT contract_id FROM mcp_contracts);

-- 4. Delete orphan escort status (no corresponding task exists)
DELETE FROM mcp_escort_status
WHERE task_id NOT IN (SELECT task_id FROM mcp_tasks);

-- 5. Delete orphan proposals (those referencing non-existent contracts)
-- Note: This deletes proposals that have contract_id/ingestion_id refs that no longer exist
DELETE FROM mcp_proposals
WHERE id NOT IN (
  SELECT DISTINCT p.id
  FROM mcp_proposals p
  JOIN mcp_contracts c ON (
    (p.metadata->>'contract_id' = c.contract_id)
    OR (p.metadata->>'ingestion_id' = c.contract_id)
    OR (p.metadata->>'visible_pixel_hash' = c.contract_id)
    OR (p.id = c.contract_id)
    OR ('wish-' || p.visible_pixel_hash = c.contract_id)
  )
)
AND (metadata->>'contract_id' IS NOT NULL 
     OR metadata->>'ingestion_id' IS NOT NULL 
     OR metadata->>'visible_pixel_hash' IS NOT NULL
     OR visible_pixel_hash IS NOT NULL);

-- Show what was deleted
SELECT 'AFTER DELETION - REMAINING' as check_type, 'submissions' as table_name, COUNT(*) as count FROM mcp_submissions
UNION ALL
SELECT 'AFTER DELETION - REMAINING', 'claims', COUNT(*) FROM mcp_claims
UNION ALL
SELECT 'AFTER DELETION - REMAINING', 'tasks', COUNT(*) FROM mcp_tasks
UNION ALL
SELECT 'AFTER DELETION - REMAINING', 'proposals', COUNT(*) FROM mcp_proposals;

-- Commit the changes
COMMIT;

-- Verify deletions
SELECT 'CLEANUP COMPLETE' as status;
