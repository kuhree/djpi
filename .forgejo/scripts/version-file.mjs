import { execFile } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { promisify } from "node:util";

const exec = promisify(execFile);
const VERSION_LINE = /(^\s*version\s*=\s*")[^"]+(";\s*$)/m;

export function renderPackageVersion(source, version) {
  if (!VERSION_LINE.test(source)) {
    throw new Error("nix/package.nix must contain exactly one simple version assignment");
  }
  return source.replace(VERSION_LINE, `$1${version}$2`);
}

export async function prepare(_pluginConfig, context) {
  const { cwd, nextRelease } = context;
  if (!nextRelease?.version) {
    throw new Error("semantic-release did not provide a version");
  }

  const packagePath = `${cwd}/nix/package.nix`;
  const packageSource = await readFile(packagePath, "utf8");
  await writeFile(`${cwd}/VERSION`, `${nextRelease.version}\n`);
  await writeFile(packagePath, renderPackageVersion(packageSource, nextRelease.version));

  await exec("git", ["add", "VERSION", "nix/package.nix"], { cwd });
  const staged = await exec("git", ["diff", "--cached", "--quiet"], { cwd }).then(
    () => false,
    (error) => {
      if (error.code === 1) return true;
      throw error;
    },
  );
  if (!staged) return;

  await exec(
    "git",
    ["commit", "-m", `chore(release): ${nextRelease.version} [skip ci]`],
    { cwd },
  );
}
