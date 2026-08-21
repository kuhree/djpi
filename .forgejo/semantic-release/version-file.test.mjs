import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { prepare, renderPackageVersion } from "./version-file.mjs";

const exec = promisify(execFile);

test("renders the Nix package version without changing surrounding text", () => {
  assert.equal(
    renderPackageVersion('  version = "0.1.0";\n  pname = "djpi";\n', "1.2.3"),
    '  version = "1.2.3";\n  pname = "djpi";\n',
  );
  assert.throws(() => renderPackageVersion('pname = "djpi";\n', "1.2.3"), /version assignment/);
});

test("prepares one release commit containing VERSION and package metadata", async () => {
  const cwd = await mkdtemp(join(tmpdir(), "djpi-release-test-"));
  await mkdir(join(cwd, "nix"));
  await writeFile(join(cwd, "VERSION"), "0.1.0\n");
  await writeFile(join(cwd, "nix/package.nix"), '{\n  version = "0.1.0";\n}\n');
  await exec("git", ["init", "--initial-branch=main"], { cwd });
  await exec("git", ["config", "user.name", "test"], { cwd });
  await exec("git", ["config", "user.email", "test@example.test"], { cwd });
  await exec("git", ["add", "."], { cwd });
  await exec("git", ["commit", "-m", "initial"], { cwd });

  await prepare({}, { cwd, nextRelease: { version: "1.2.3" } });

  assert.equal(await readFile(join(cwd, "VERSION"), "utf8"), "1.2.3\n");
  assert.match(await readFile(join(cwd, "nix/package.nix"), "utf8"), /version = "1\.2\.3"/);
  const { stdout } = await exec("git", ["log", "-1", "--format=%s"], { cwd });
  assert.equal(stdout.trim(), "chore(release): 1.2.3 [skip ci]");
});
