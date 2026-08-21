const API_PREFIX = "api/v1";

function requiredEnvironment(env, name) {
  const value = env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

export function normalizeForgejoUrl(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error("FORGEJO_URL must be a valid URL");
  }
  if (!["http:", "https:"].includes(url.protocol)) {
    throw new Error("FORGEJO_URL must use HTTP or HTTPS");
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new Error("FORGEJO_URL must not contain credentials, a query, or a fragment");
  }
  return url.toString().replace(/\/$/, "");
}

export function parseRepository(value) {
  const parts = value.split("/");
  if (parts.length !== 2 || parts.some((part) => !part || part === "." || part === "..")) {
    throw new Error("FORGEJO_REPOSITORY must use the owner/repository format");
  }
  return Object.freeze({ owner: parts[0], repository: parts[1] });
}

export function readForgejoEnvironment(env = process.env) {
  return Object.freeze({
    baseUrl: normalizeForgejoUrl(requiredEnvironment(env, "FORGEJO_URL")),
    repository: parseRepository(requiredEnvironment(env, "FORGEJO_REPOSITORY")),
    token: requiredEnvironment(env, "FORGEJO_TOKEN"),
  });
}

export function buildReleasePayload(nextRelease, branchName) {
  if (!nextRelease?.gitTag || typeof nextRelease.notes !== "string") {
    throw new Error("semantic-release did not provide a tag and release notes");
  }
  if (!branchName) throw new Error("semantic-release did not provide a release branch");
  return Object.freeze({
    tag_name: nextRelease.gitTag,
    target_commitish: branchName,
    body: nextRelease.notes,
    draft: false,
    prerelease: false,
  });
}

export function buildApiUrl(baseUrl, path) {
  return new URL(`${API_PREFIX}/${path}`, `${baseUrl}/`).toString();
}

export function createForgejoClient(config, fetchImpl = globalThis.fetch) {
  async function request(method, path, body) {
    let response;
    try {
      response = await fetchImpl(buildApiUrl(config.baseUrl, path), {
        method,
        headers: {
          Accept: "application/json",
          Authorization: `Bearer ${config.token}`,
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      throw new Error(`Forgejo API ${method} request failed`);
    }
    if (!response.ok) throw new Error(`Forgejo API ${method} failed with status ${response.status}`);
  }

  const owner = encodeURIComponent(config.repository.owner);
  const repository = encodeURIComponent(config.repository.repository);
  return Object.freeze({
    verify: () => request("GET", "user"),
    publish: (payload) => request("POST", `repos/${owner}/${repository}/releases`, payload),
  });
}

export async function verifyConditions() {
  await createForgejoClient(readForgejoEnvironment()).verify();
}

export async function publish(_pluginConfig, context) {
  const config = readForgejoEnvironment();
  const payload = buildReleasePayload(context.nextRelease, context.branch?.name);
  await createForgejoClient(config).publish(payload);
  return {
    name: context.nextRelease.gitTag,
    url: `${config.baseUrl}/${encodeURIComponent(config.repository.owner)}/${encodeURIComponent(config.repository.repository)}/releases/tag/${encodeURIComponent(context.nextRelease.gitTag)}`,
  };
}
