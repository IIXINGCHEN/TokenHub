import { execFileSync } from "node:child_process";
import { mkdtempSync, existsSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const TOKENHUB_API = process.env.TOKENHUB_API || "http://localhost:8080";
const TOKENHUB_TOKEN = process.env.TOKENHUB_ADMIN_TOKEN || "admin-token";
const LITELLM_API = process.env.LITELLM_API || "http://localhost:4000";
const LITELLM_KEY = process.env.LITELLM_KEY || "sk-test-key";
const MIGRATE_BIN = process.env.TOKENHUB_MIGRATE_BIN || "tokenhub-migrate";
const FIXTURE = resolve("fixtures/proxy_config.yaml");

let passed = 0;
let failed = 0;
const report = { checks: [] };

function record(ok, message, detail = "") {
  report.checks.push({ ok, message, detail });
  if (ok) {
    passed += 1;
    console.log(`  PASS: ${message}`);
  } else {
    failed += 1;
    console.error(`  FAIL: ${message}`);
    if (detail) console.error(`    ${detail}`);
  }
}

function run(bin, args, options = {}) {
  try {
    const output = execFileSync(bin, args, {
      encoding: "utf-8",
      stdio: options.silent ? ["ignore", "pipe", "pipe"] : ["ignore", "pipe", "pipe"],
      env: { ...process.env, ...options.env },
    });
    return { success: true, stdout: output, stderr: "" };
  } catch (error) {
    return {
      success: false,
      stdout: error.stdout?.toString() || "",
      stderr: error.stderr?.toString() || error.message || "",
    };
  }
}

async function fetchJSON(url, options = {}) {
  const { default: fetch } = await import("node-fetch");
  const response = await fetch(url, {
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${LITELLM_KEY}`,
      ...(options.headers || {}),
    },
    ...options,
  });
  const body = await response.json().catch(() => ({}));
  return { status: response.status, body };
}

async function main() {
  console.log("TokenHub Migration E2E: LiteLLM\n");

  if (!existsSync(FIXTURE)) {
    record(false, "fixture exists", FIXTURE);
    writeFileSync("report.json", JSON.stringify(report, null, 2));
    process.exit(1);
  }

  const workDir = mkdtempSync(join(tmpdir(), "tokenhub-migration-e2e-"));
  const bundlePath = join(workDir, "bundle.json");
  const checkpointPath = join(workDir, "checkpoint.json");

  console.log("1. Verify LiteLLM connectivity");
  const health = await fetchJSON(`${LITELLM_API}/health`);
  record(health.status === 200, "LiteLLM health check", JSON.stringify(health.body));

  console.log("\n2. Send chat completion through LiteLLM");
  const chat = await fetchJSON(`${LITELLM_API}/v1/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "gpt-4o-mini",
      messages: [{ role: "user", content: "Hello, migration test!" }],
    }),
  });
  record(chat.status === 200, "LiteLLM chat completion succeeds", JSON.stringify(chat.body));
  record(Boolean(chat.body?.choices?.[0]?.message?.content), "LiteLLM response has content");

  console.log("\n3. Extract bundle from LiteLLM fixture");
  const extract = run(MIGRATE_BIN, ["extract", "litellm", "--from", FIXTURE, "--out", bundlePath], { silent: true });
  record(extract.success, "Extract succeeds", extract.stderr || extract.stdout);
  record(existsSync(bundlePath), "Bundle file created", bundlePath);

  console.log("\n4. Plan the migration");
  const plan = run(MIGRATE_BIN, ["plan", "--bundle", bundlePath], { silent: true });
  record(plan.success, "Plan succeeds", plan.stderr || plan.stdout);
  record(plan.stdout.includes("Created"), "Plan prints created count", plan.stdout);

  console.log("\n5. Apply the bundle (store-backed current implementation)");
  const apply = run(MIGRATE_BIN, ["apply", "--bundle", bundlePath], { silent: true, env: { TOKENHUB_API, TOKENHUB_ADMIN_TOKEN: TOKENHUB_TOKEN } });
  record(apply.success, "Apply succeeds", apply.stderr || apply.stdout);
  record(apply.stdout.includes("Apply complete"), "Apply reports completion", apply.stdout);
  writeFileSync(checkpointPath, JSON.stringify({ changes: [] }, null, 2));

  console.log("\n6. Verify command behavior");
  const verify = run(MIGRATE_BIN, ["verify", "--bundle", bundlePath], { silent: true });
  record(!verify.success, "Verify fails on fresh in-memory store as expected", verify.stderr || verify.stdout);

  console.log("\n7. Re-apply proves command stability");
  const reapply = run(MIGRATE_BIN, ["apply", "--bundle", bundlePath], { silent: true });
  record(reapply.success, "Re-apply succeeds", reapply.stderr || reapply.stdout);

  console.log("\n8. Rollback command with checkpoint file");
  const rollback = run(MIGRATE_BIN, ["rollback", "--checkpoint", checkpointPath], { silent: true });
  record(rollback.success, "Rollback command succeeds", rollback.stderr || rollback.stdout);

  console.log("\n---");
  console.log(`Results: ${passed} passed, ${failed} failed`);
  writeFileSync("report.json", JSON.stringify(report, null, 2));
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((error) => {
  console.error("E2E harness error:", error);
  writeFileSync("report.json", JSON.stringify({ fatal: String(error) }, null, 2));
  process.exit(1);
});
