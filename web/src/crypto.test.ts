import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { accessTag, aad, decode, decrypt, derive, encrypt } from "./crypto.ts";

const vector = JSON.parse(readFileSync(new URL("../../testdata/vector.json", import.meta.url), "utf8"));

test("web crypto matches the frozen vector", async () => {
  const master = decode(vector.masterKey);
  const shareId = decode(vector.shareId);
  const objectId = decode(vector.objectId);
  const fileKey = await derive(master, shareId, "imgmesh-file-v1");
  const manifestKey = await derive(master, shareId, "imgmesh-manifest-v1");
  assert.equal(Buffer.from(fileKey).toString("base64url"), vector.fileKey);
  assert.equal(Buffer.from(manifestKey).toString("base64url"), vector.manifestKey);
  assert.equal(Buffer.from(await accessTag(master, shareId)).toString("base64url"), vector.accessTag);
  assert.equal(Buffer.from(aad(shareId, objectId)).toString("base64url"), vector.aad);
  const plain = await decrypt(fileKey, decode(vector.nonce), aad(shareId, objectId), decode(vector.ciphertext));
  assert.equal(Buffer.from(plain).toString("base64url"), vector.plaintext);
  const again = await encrypt(fileKey, decode(vector.nonce), aad(shareId, objectId), plain);
  assert.equal(Buffer.from(again).toString("base64url"), vector.ciphertext);
});
