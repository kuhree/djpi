import assert from "node:assert/strict";
import test from "node:test";

import {
  buildReleasePayload,
  createForgejoClient,
  normalizeForgejoUrl,
  parseRepository,
  readForgejoEnvironment,
} from "./forgejo-publish.mjs";

const config = Object.freeze({
  baseUrl: "https://git.example.test",
  repository: Object.freeze({ owner: "example.org", repository: "djpi" }),
  token: "private-token-value",
});

test("validates Forgejo configuration without exposing credentials", () => {
  assert.equal(normalizeForgejoUrl("https://git.example.test/"), "https://git.example.test");
  assert.throws(() => normalizeForgejoUrl("https://token@git.example.test"), /credentials/);
  assert.deepEqual(parseRepository("example.org/djpi"), config.repository);
  assert.throws(() => parseRepository("djpi"), /owner\/repository/);
  assert.deepEqual(
    readForgejoEnvironment({
      FORGEJO_URL: "https://git.example.test",
      FORGEJO_REPOSITORY: "example.org/djpi",
      FORGEJO_TOKEN: "private-token-value",
    }),
    config,
  );
});

test("publishes a Forgejo release payload", async () => {
  const payload = buildReleasePayload(
    { gitTag: "v1.2.3", notes: "Generated notes" },
    "main",
  );
  assert.deepEqual(payload, {
    tag_name: "v1.2.3",
    target_commitish: "main",
    body: "Generated notes",
    draft: false,
    prerelease: false,
  });

  const requests = [];
  const client = createForgejoClient(config, async (url, options) => {
    requests.push({ url, options });
    return { ok: true, status: 201 };
  });
  await client.publish(payload);
  assert.equal(requests[0].url, "https://git.example.test/api/v1/repos/example.org/djpi/releases");
  assert.doesNotMatch(JSON.stringify(requests), /response-secret-value/);
});

test("redacts token and response bodies from API failures", async () => {
  const client = createForgejoClient(config, async () => ({
    ok: false,
    status: 403,
    text: async () => "response-secret-value",
  }));
  await assert.rejects(client.verify(), (error) => {
    assert.equal(error.message, "Forgejo API GET failed with status 403");
    assert.doesNotMatch(error.message, /private-token-value|response-secret-value/);
    return true;
  });
});
