import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const nixWorkflows = ["ci.yml", "release.yml"];

for (const workflow of nixWorkflows) {
  test(`${workflow} uses the sandboxed Nix runner`, async () => {
    const source = await readFile(new URL(`../workflows/${workflow}`, import.meta.url), "utf8");

    assert.match(source, /runs-on:\s*host\/nix/);
    assert.doesNotMatch(source, /install-nix-action/);
    assert.match(source, /(^|\n)concurrency:\n\s+group:\s+djpi-/m);
    assert.doesNotMatch(source.slice(source.indexOf("jobs:")), /\n\s+concurrency:/);
  });
}
