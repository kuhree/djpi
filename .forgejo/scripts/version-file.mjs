import { execFile } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { promisify } from "node:util";

const exec = promisify(execFile);

export async function prepare(_pluginConfig, context) {
  const { cwd, nextRelease } = context;
  if (!nextRelease?.version) {
    throw new Error("semantic-release did not provide a version");
  }

  await writeFile(`${cwd}/VERSION`, `${nextRelease.version}\n`);

  await exec("git", ["add", "VERSION"], { cwd });
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
