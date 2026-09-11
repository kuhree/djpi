import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const nixWorkflows = ["ci.yml", "release.yml"];

const checkoutAction =
  "https://github.com/actions/checkout@11d5960a326750d5838078e36cf38b85af677262";

for (const workflow of nixWorkflows) {
  test(`${workflow} uses the sandboxed Nix runner`, async () => {
    const source = await readFile(new URL(`../workflows/${workflow}`, import.meta.url), "utf8");

    assert.match(source, /runs-on:\s*host\/nix/);
    assert.doesNotMatch(source, /install-nix-action/);
    if (workflow === "ci.yml") {
      assert.match(
        source,
        /(^|\n)\s+group:\s+djpi-ci-\$\{\{ github\.event\.number \|\| github\.event\.pull_request\.number \|\| github\.ref \}\}/m,
      );
    } else {
      assert.match(source, /(^|\n)\s+group:\s+djpi-release-main\s*$/m);
    }
    assert.doesNotMatch(source.slice(source.indexOf("jobs:")), /\n\s+concurrency:/);
    assert.ok(source.includes(`${checkoutAction} # v4`));
    assert.doesNotMatch(source, /https:\/\/github\.com\/actions\/checkout@v4(?:\s|$)/);
  });
}
