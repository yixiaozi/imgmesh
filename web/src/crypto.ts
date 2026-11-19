const text = new TextEncoder();

export function encode(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

export function decode(value: string): Uint8Array {
  const b64 = value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - (value.length % 4)) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  if (encode(out) !== value) throw new Error("non-canonical id");
  return out;
}

export async function derive(master: Uint8Array, shareId: Uint8Array, info: string): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", master as BufferSource, "HKDF", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits(
    { name: "HKDF", hash: "SHA-256", salt: shareId as BufferSource, info: text.encode(info) as BufferSource },
    key,
    256,
  );
  return new Uint8Array(bits);
}

export async function accessTag(master: Uint8Array, shareId: Uint8Array): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", master as BufferSource, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const msg = new Uint8Array(text.encode("imgmesh-access-v1").length + shareId.length);
  msg.set(text.encode("imgmesh-access-v1"), 0);
  msg.set(shareId, text.encode("imgmesh-access-v1").length);
  return new Uint8Array(await crypto.subtle.sign("HMAC", key, msg as BufferSource));
}

export function aad(shareId: Uint8Array, objectId: Uint8Array): Uint8Array {
  const out = new Uint8Array(32);
  out.set(shareId, 0);
  out.set(objectId, 16);
  return out;
}

export async function decrypt(
  keyBytes: Uint8Array,
  nonce: Uint8Array,
  additional: Uint8Array,
  ciphertext: Uint8Array,
): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey("raw", keyBytes as BufferSource, "AES-GCM", false, ["decrypt"]);
  const plain = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: nonce as BufferSource, additionalData: additional as BufferSource },
    key,
    ciphertext as BufferSource,
  );
  return new Uint8Array(plain);
}

export async function sha256(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", bytes as BufferSource);
  return encode(new Uint8Array(digest));
}

export interface ManifestItem {
  id: string;
  name?: string;
  mime: string;
  bytes: number;
  nonce: string;
  sha256?: string;
  thumb?: { id: string; bytes: number; nonce: string };
}

export interface Manifest {
  v: number;
  title: string;
  items: ManifestItem[];
}
