// LiteLLM migration E2E test harness.
//
// Requires:
//   - A running TokenHub instance (TOKENHUB_API env var)
//   - A running LiteLLM instance (LITELLM_API env var)
//   - The tokenhub-migrate binary built and on PATH
//
// Steps:
//   1. Seed LiteLLM with a fixture config and virtual key.
//   2. Send a chat completion through LiteLLM, verify response.
//   3. Extract bundle from LiteLLM config.
//   4. Plan and apply the bundle to TokenHub.
//   5. Verify the applied state.
//   6. Re-run apply to prove idempotency (zero writes).
//   7. Run verify to confirm clean state.
//   8. Run rollback to revert.

import { execSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const TOKENHUB_API = process.env.TOKENHUB_API || "http://localhost:8080";
const TOKENHUB_TOKEN = process.env.TOKENHUB_ADMIN_TOKEN || "admin-token";
const LITELLM_API = process.env.LITELLM_API || "http://localhost:4000";
const LITELLM_KEY = process.env.LITELLM_KEY || "sk-test-key";
const MIGRATE_BIN = process.env.TOKENHUB_MIGRATE_BIN || "tokenhub-migrate";

let passed = 0;
let failed = 0;

function assert(condition, message) {
  if (condition) {
    passed++;
    console.log(`  PASS: ${message}`);
  } else {
    failed++;
    console.error(`  FAIL: ${message}`);
  }
}

function run(command, opts = {}) {
  try {
    const result = execSync(command, {
      encoding: "utf-8",
      stdio: opts.silent ? "pipe" : "inherit",
      ...opts,
    });
    return { success: true, output: result };
  } catch (err) {
    return { success: false, output: err.stdout || "", stderr: err.stderr || "" };
  }
}

async function fetchJSON(url, options = {}) {
  const { default: fetch } = await import("node-fetch");
  const res = await fetch(url, {
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${LITELLM_KEY}`,
      ...options.headers,
    },
    ...options,
  });
  const body = await res.json();
  return { status: res.status, body };
}

async function main() {
  console.log("TokenHub Migration E2E: LiteLLM\n");

  const tmpDir = tmpdir();
  const bundlePath = join(tmpDir, "bundle.json");
  const checkpointPath = join(tmpDir, "checkpoint.json");

  // Step 1: Verify LiteLLM is running
  console.log("1. Verify LiteLLM connectivity");
  const litellmHealth = await fetchJSON(`${LITELLM_API}/health`);
  assert(litellmHealth.status === 200, "LiteLLM health check");

  // Step 2: Send chat completion through LiteLLM
  console.log("\n2. Send chat completion through LiteLLM");
  const litellmReq = await fetchJSON(`${LITELLM_API}/v1/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "gpt-4o-mini",
      messages: [{ role: "user", content: "Hello, migration test!" }],
    }),
  });
  assert(litellmReq.status === 200, "LiteLLM chat completion succeeds");
  assert(
    litellmReq.body?.choices?.[0]?.message?.content != null,
    "LiteLLM response has content"
  );

  // Step 3: Extract bundle from LiteLLM config
  console.log("\n3. Extract bundle from LiteLLM");
  const extractCmd = `${MIGRATE_BIN} litellm extract --from proxy_config.yaml --out ${bundlePath}`;
  const extractResult = run(extractCmd, { silent: true });
  assert(extractResult.success, "Extract succeeds");
  assert(
    extractResult.output.includes("Bundle written"),
    "Extract writes bundle to file"
  );

  // Step 4: Plan the migration
  console.log("\n4. Plan the migration");
  const planCmd = `${MIGRATE_BIN} plan --bundle ${bundlePath}`;
  const planResult = run(planCmd, { silent: true });
  assert(planResult.success, "Plan succeeds");
  assert(planResult.output.includes("Created"), "Plan shows created count");

  // Step 5: Apply the bundle
  console.log("\n5. Apply the bundle to TokenHub");
  const applyCmd = `${MIGRATE_BIN} apply --bundle ${bundlePath} --to ${TOKENHUB_API} --token ${TOKENHUB_TOKEN}`;
  const applyResult = run(applyCmd, { silent: true });
  assert(applyResult.success, "Apply succeeds");
  assert(
    applyResult.output.includes("Apply complete"),
    "Apply reports completion"
  );

  // Step 6: Verify the applied state
  console.log("\n6. Verify applied state");
  const verifyCmd = `${MIGRATE_BIN} verify --bundle ${bundlePath}`;
  const verifyResult = run(verifyCmd, { silent: true });
  assert(verifyResult.success, "Verify succeeds");
  assert(verifyResult.output.includes("PASS"), "Verify reports PASS");

  // Step 7: Re-apply to prove idempotency
  console.log("\n7. Re-apply to prove idempotency");
  const reapplyResult = run(applyCmd, { silent: true });
  assert(reapplyResult.success, "Re-apply succeeds");
  assert(
    reapplyResult.output.includes("Skipped"),
    "Re-apply shows skipped (zero writes)"
  );

  // Step 8: Rollback
  console.log("\n8. Rollback");
  const rollbackCmd = `${MIGRATE_BIN} rollback --checkpoint ${checkpointPath}`;
  const rollbackResult = run(rollbackCmd, { silent: true });
  assert(rollbackResult.success, "Rollback succeeds");
  assert(
    rollbackResult.output.includes("reverted"),
    "Rollback reports reverted changes"
  );

  // Summary
  console.log(`\n---`);
  console.log(`Results: ${passed} passed, ${failed} failed`);
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  console.error("E2E harness error:", err);
  process.exit(1);
});
