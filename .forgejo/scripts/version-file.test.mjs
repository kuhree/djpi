import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { prepare } from "./version-file.mjs";

const exec = promisify(execFile);

async function repositoryWithOrigin() {
  const root = await mkdtemp(join(tmpdir(), "djpi-release-test-"));
  const origin = join(root, "origin.git");
  const cwd = join(root, "work");
  await exec("git", ["init", "--bare", "--initial-branch=main", origin]);
  await exec("git", ["init", "--initial-branch=main", cwd]);
  await exec("git", ["config", "user.name", "test"], { cwd });
  await exec("git", ["config", "user.email", "test@example.test"], { cwd });
  await exec("git", ["remote", "add", "origin", origin], { cwd });
  await writeFile(join(cwd, "VERSION"), "0.1.0\n");
  await exec("git", ["add", "."], { cwd });
  await exec("git", ["commit", "-m", "initial"], { cwd });
  await exec("git", ["push", "origin", "main"], { cwd });
  return { cwd, origin };
}

test("prepares one release commit containing the single VERSION source", async () => {
  const { cwd } = await repositoryWithOrigin();

  await prepare({}, { cwd, nextRelease: { version: "1.2.3" }, branch: { name: "main" } });

  assert.equal(await readFile(join(cwd, "VERSION"), "utf8"), "1.2.3\n");
  const { stdout } = await exec("git", ["log", "-1", "--format=%s"], { cwd });
  assert.equal(stdout.trim(), "chore(release): 1.2.3 [skip ci]");
});

test("pushes the release commit so the tag lands in the branch history", async () => {
  const { cwd, origin } = await repositoryWithOrigin();

  await prepare({}, { cwd, nextRelease: { version: "1.2.3" }, branch: { name: "main" } });

  const local = await exec("git", ["rev-parse", "HEAD"], { cwd });
  const remote = await exec("git", ["rev-parse", "main"], { cwd: origin });
  assert.equal(remote.stdout.trim(), local.stdout.trim());
});

test("refuses to prepare a release without a branch", async () => {
  const { cwd } = await repositoryWithOrigin();

  await assert.rejects(
    prepare({}, { cwd, nextRelease: { version: "1.2.3" } }),
    /release branch/,
  );
});
