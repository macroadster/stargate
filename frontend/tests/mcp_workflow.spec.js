const { test, expect } = require('@playwright/test');

// Complete MCP Workflow Test
// This test covers the full agent workflow from wish creation to fulfillment:
// 1. Human Wish Creation: POST to /api/inscribe creates contract with "pending" status
// 2. AI Agent Proposal Competition: Multiple agents submit proposals to /api/smart_contract/proposals
// 3. Human Review & Selection: Human evaluates and selects best proposal
// 4. Contract Activation: POST to /api/smart_contract/proposals/{id}/approve changes status to "active"
// 5. AI Agent Task Competition: Agents claim available tasks
// 6. Work Submission: Agents submit completed work
// 7. Human Review & Completion: Human reviewers evaluate work and mark wish as fulfilled

const BASE_URL = process.env.BASE_URL || 'http://starlight.local';
const CONTRACT_ID = 'test-contract-id';
const PROPOSAL_ID = 'test-proposal-id';
const TASK_ID = 'test-task-id';
const CLAIM_ID = 'test-claim-id';
const SUBMISSION_ID = 'test-submission-id';

const HUMAN_API_KEY = '2048ca06fe2d0f6f4059a2fa41d8573712e5f2c82d84834c2c6fbba9306c04c3';
const AGENT_API_KEY = '391ee17687d21299114268662858fb4b94677f41adcb8344fa9ca4cf915ebe6a';

test.describe('MCP Workflow E2E (Mocked)', () => {
  // State to simulate backend changes
  let isApproved = false;
  let isClaimed = false;
  let isSubmitted = false;

  test.beforeEach(async ({ page }) => {
    // Reset state
    isApproved = false;
    isClaimed = false;
    isSubmitted = false;

    // Mock Auth logic (simulating keys in local storage)
    await page.addInitScript(() => {
      localStorage.setItem('X-API-Key', 'mock-api-key');
      localStorage.setItem('X-Wallet-Address', 'bc1qmockwallet');
    });

    // --- MOCKS ---

    // 0. Create Wish (Human Wish Creation)
    await page.route(`**/api/inscribe*`, async route => {
      await route.fulfill({
        json: {
          success: true,
          result: {
            contract_id: CONTRACT_ID,
            status: 'pending',
            headline: 'Test Wish',
            description: 'A test wish for the MCP workflow',
            budget_sats: 1000,
            created_at: new Date().toISOString()
          }
        }
      });
    });

    // 1. Search
    await page.route(`**/api/search*`, async route => {
      await route.fulfill({
        json: {
          data: {
            contracts: [{
              id: CONTRACT_ID,
              contract_id: CONTRACT_ID,
              headline: 'Test Contract',
              image_url: '',
              metadata: { visible_pixel_hash: CONTRACT_ID }
            }],
            inscriptions: [],
            blocks: []
          }
        }
      });
    });

    // 2. MCP Calls
    await page.route(`**/mcp/call`, async route => {
      const postData = route.request().postDataJSON();
      const tool = postData.tool;

      if (tool === 'create_proposal') {
        await route.fulfill({ json: { success: true, result: { proposal_id: PROPOSAL_ID } } });
      } else if (tool === 'list_tasks') {
        await route.fulfill({
          json: {
            success: true,
            result: {
              tasks: [{
                task_id: TASK_ID,
                title: 'Test Task',
                budget_sats: 1000,
                status: 'available'
              }]
            }
          }
        });
      } else {
        await route.continue();
      }
    });

    // Mock Claim Task REST API
    await page.route(`**/api/smart_contract/tasks/${TASK_ID}/claim`, async route => {
      isClaimed = true;
      await route.fulfill({ json: { success: true, result: { claim_id: CLAIM_ID } } });
    });

    // Mock Submit Work REST API
    await page.route(`**/api/smart_contract/claims/${CLAIM_ID}/submit`, async route => {
      isSubmitted = true;
      await route.fulfill({ json: { success: true, result: { submission_id: SUBMISSION_ID } } });
    });

    // 3. Get Proposals (UI Polling)
    await page.route(`**/api/smart_contract/proposals*`, async route => {
      await route.fulfill({
        json: {
          proposals: [
            {
              id: PROPOSAL_ID,
              title: 'Agent 1 Proposal',
              description_md: 'This is proposal from agent 1',
              status: isApproved ? 'approved' : 'pending',
              budget_sats: 1000,
              tasks: [{
                task_id: TASK_ID,
                contract_id: CONTRACT_ID,
                title: 'Test Task',
                budget_sats: 1000,
                status: isSubmitted ? 'submitted' : (isApproved ? 'available' : 'pending'),
                active_claim_id: (isClaimed || isSubmitted) ? CLAIM_ID : undefined,
                claimed_by: (isClaimed || isSubmitted) ? 'bc1qmockwallet' : undefined,
                contractor_wallet: 'bc1qcontractorwallet'
              }]
            },
            {
              id: 'test-proposal-id-2',
              title: 'Agent 2 Proposal',
              description_md: 'This is proposal from agent 2',
              status: 'pending',
              budget_sats: 1000,
              tasks: []
            }
          ],
          submissions: isSubmitted ? [{
            submission_id: SUBMISSION_ID,
            task_id: TASK_ID,
            claim_id: CLAIM_ID,
            status: 'pending_review',
            created_at: new Date().toISOString()
          }] : []
        }
      });
    });

    // 4. Approve Proposal
    await page.route(`**/api/smart_contract/proposals/${PROPOSAL_ID}/approve`, async route => {
      isApproved = true;
      await route.fulfill({ json: { success: true } });
    });

    // 5. Submissions (UI Polling)
    await page.route(`**/api/smart_contract/submissions*`, async route => {
      await route.fulfill({
        json: {
            submissions: isSubmitted ? [{
            submission_id: SUBMISSION_ID,
            task_id: TASK_ID,
            claim_id: CLAIM_ID,
            status: 'pending_review',
            created_at: new Date().toISOString()
          }] : []
        }
      });
    });

    // 6. PSBT Generation
    await page.route(`**/api/smart_contract/contracts/${CONTRACT_ID}/psbt`, async route => {
      await route.fulfill({
        json: {
          data: {
            psbt_base64: 'cHNidP8BAFICAAAAAZ38...', // Dummy PSBT
            selected_sats: 1000,
            fee_sats: 100,
            change_sats: 0,
            payout_script: 'dummy-script'
          }
        }
      });
    });

    // 7. Inscription/Contract Details (fetch on modal open)
    // The UI might try to fetch block summaries or inscription details.
    // We mocked search, so when we click, it might assume it has data or fetch more.
    // If it fetches `/api/data/block-inscriptions/...`, mock it.
    // Or `/api/smart_contract/contracts/${CONTRACT_ID}`?
    // Let's watch for failures.

    await page.goto(`${BASE_URL}/`);
  });

  test('Complete Workflow: Wish Creation to Fulfillment', async ({ page, request }) => {
    // Step 1: Human Wish Creation
    // Simulate POST request to /api/inscribe to create a new wish
    await page.evaluate(async ({ url, contractId }) => {
      const response = await fetch(`${url}/api/inscribe`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-API-Key': '2048ca06fe2d0f6f4059a2fa41d8573712e5f2c82d84834c2c6fbba9306c04c3'
        },
        body: JSON.stringify({
          headline: 'Test Wish',
          description: 'A test wish for the MCP workflow',
          budget_sats: 1000
        })
      });
      const data = await response.json();
      console.log('Wish created:', data.result);
    }, { url: BASE_URL, contractId: CONTRACT_ID });

    // Step 2: AI Agent Proposal Competition
    // Simulate multiple agents competing to submit proposals
    await page.evaluate(async ({ url, contractId }) => {
      // Agent 1 submits a proposal
      await fetch(`${url}/mcp/call`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tool: 'create_proposal',
          arguments: {
            contract_id: contractId,
            title: 'Agent 1 Proposal',
            description_md: 'This is proposal from agent 1'
          }
        })
      });

      // Agent 2 submits a competing proposal
      await fetch(`${url}/mcp/call`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tool: 'create_proposal',
          arguments: {
            contract_id: contractId,
            title: 'Agent 2 Proposal',
            description_md: 'This is proposal from agent 2'
          }
        })
      });
    }, { url: BASE_URL, contractId: CONTRACT_ID });

    // 2. Search for Contract in UI
    const searchInput = page.locator('input[placeholder="Search..."]');
    await searchInput.click();
    await searchInput.fill(CONTRACT_ID);
    await searchInput.press('Enter');

    // Click result
    const contractButton = page.locator('button').filter({ hasText: CONTRACT_ID }).first();
    await contractButton.waitFor({ state: 'visible' });
    await contractButton.click();

    // 3. Approve Proposal
    const proposalsTab = page.getByRole('button', { name: 'Proposals' });
    await proposalsTab.click();

    const approveButton = page.getByRole('button', { name: 'Approve' }).first();
    await approveButton.waitFor({ state: 'visible' });
    await approveButton.click();

    // Verify Approved
    await expect(page.getByText('Approved').first()).toBeVisible();

    // Close modal
    // await page.getByRole('button').filter({ hasText: '' }).first().click(); // Close button usually has no text, or find by icon. 
    // Actually, let's just navigate to /discover, safer.
    await page.goto(`${BASE_URL}/discover`);

    // 4. Contractor Work (UI Action)
    // Wait for task to appear
    await expect(page.getByText('Test Task')).toBeVisible();

    // Claim
    const claimButton = page.getByRole('button', { name: 'Claim' }).first();
    await claimButton.click();

    // Wait for "Submit work" area to appear (means claim was successful and UI updated)
    const submitButton = page.getByRole('button', { name: 'Submit work' }).first();
    await expect(submitButton).toBeVisible();

    // Fill notes
    const notesArea = page.getByPlaceholder('Notes / deliverables');
    await notesArea.fill('Done this task');
    
    // Submit
    await submitButton.click();

    // Verify submitted status (button changes or status text changes)
    await expect(page.getByText('Submission: pending_review').first()).toBeVisible();

    // 5. Build PSBT (Back to Inscription Modal)
    await page.goto(`${BASE_URL}/`);
    
    // Search again or just re-open if preserved? Search is safer.
    await searchInput.click();
    await searchInput.fill(CONTRACT_ID);
    await searchInput.press('Enter');
    
    await contractButton.click();
    
    const deliverablesTab = page.getByRole('button', { name: 'Deliverables' });
    await deliverablesTab.click();

    // Refresh to ensure submission is seen
    const refreshButton = page.getByRole('button', { name: 'Refresh' }).first();
    if (await refreshButton.isVisible()) {
        await refreshButton.click();
    }

    const buildButton = page.getByRole('button', { name: /Publish & Build/i });
    await buildButton.waitFor({ state: 'visible' });
    await expect(buildButton).toBeEnabled();
    await buildButton.click();

    // 6. Verify PSBT (Work is considered complete when PSBT is built)
    await expect(page.getByText(/Selected.*1000.*sats/)).toBeVisible();
    await expect(page.getByText('Copied base64')).not.toBeVisible();
    // Just check for presence of result area
    await expect(page.locator('textarea')).toHaveValue(/cHNidP8BAFICAAAAAZ38/);
  });
});
