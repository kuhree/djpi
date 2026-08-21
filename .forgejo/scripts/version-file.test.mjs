import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { prepare } from "./version-file.mjs";

const exec = promisify(execFile);

test("prepares one release commit containing the single VERSION source", async () => {
  const cwd = await mkdtemp(join(tmpdir(), "djpi-release-test-"));
  await writeFile(join(cwd, "VERSION"), "0.1.0\n");
  await exec("git", ["init", "--initial-branch=main"], { cwd });
  await exec("git", ["config", "user.name", "test"], { cwd });
  await exec("git", ["config", "user.email", "test@example.test"], { cwd });
  await exec("git", ["add", "."], { cwd });
  await exec("git", ["commit", "-m", "initial"], { cwd });

  await prepare({}, { cwd, nextRelease: { version: "1.2.3" } });

  assert.equal(await readFile(join(cwd, "VERSION"), "utf8"), "1.2.3\n");
  const { stdout } = await exec("git", ["log", "-1", "--format=%s"], { cwd });
  assert.equal(stdout.trim(), "chore(release): 1.2.3 [skip ci]");
});
