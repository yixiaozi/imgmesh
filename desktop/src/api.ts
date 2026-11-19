import { accessTag, aad, decode, decrypt, derive, encode, encrypt, sha256, type Manifest, type ManifestItem } from "../../web/src/crypto";

export interface AccountUser {
  id: string;
  displayName: string;
  bio?: string;
  city?: string;
  contact?: string;
  appleLinked: boolean;
  deviceId: string;
}

export interface Account {
  token: string;
  alreadyRegistered?: boolean;
  user: AccountUser;
}

export interface PublicUser {
  id: string;
  displayName: string;
  bio: string;
  city: string;
  contact: string;
  sharedCount: number;
  savedCount: number;
}

export interface NamedUser {
  id: string;
  displayName: string;
  city?: string;
}

export interface PhotoHolder extends NamedUser {
  shared?: boolean;
  saved?: boolean;
  from?: NamedUser;
}

export interface PhotoCard {
  id: string;
  sharedBy: NamedUser[];
  holders: PhotoHolder[];
  savers: PhotoHolder[];
}

export interface ActivityPerson {
  displayName: string;
  client: string;
  count?: number;
  at?: string;
  savedAt?: string;
}

export interface Activity {
  viewed: ActivityPerson[];
  downloaded: ActivityPerson[];
  phones: ActivityPerson[];
}

export interface UploadPlan {
  id: string;
  kind: string;
  ready: boolean;
  partSize?: number;
  parts: { partNumber: number; url: string }[];
}

const deviceKey = "imgmesh-desktop-device";
const accountKey = "imgmesh-desktop-account";

export function apiBase(): string {
  return window.imgmesh?.apiBase?.replace(/\/$/, "") ?? "";
}

export function resolveAPI(url: string): string {
  const base = apiBase();
  try {
    const parsed = new URL(url, base || location.origin);
    if (parsed.pathname.startsWith("/v1/")) return `${base}${parsed.pathname}${parsed.search}`;
    return parsed.toString();
  } catch {
    return url;
  }
}

export function deviceId(): string {
  const current = localStorage.getItem(deviceKey) ?? "";
  if (/^[A-Za-z0-9_-]{16,64}$/.test(current)) return current;
  const created = crypto.randomUUID();
  localStorage.setItem(deviceKey, created);
  return created;
}

export function loadAccount(): Account | null {
  try {
    const raw = localStorage.getItem(accountKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Account;
    if (!parsed.token || !parsed.user?.displayName) return null;
    return parsed;
  } catch {
    return null;
  }
}

export function saveAccount(account: Account) {
  localStorage.setItem(accountKey, JSON.stringify(account));
}

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? "request_failed";
  } catch {
    return "request_failed";
  }
}

async function sendJSON(path: string, method: string, token: string | null, body?: unknown): Promise<Response> {
  const headers = new Headers();
  if (body !== undefined) headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`${apiBase()}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return res;
}

export async function register(displayName: string): Promise<Account> {
  const res = await sendJSON("/v1/register", "POST", null, { deviceId: deviceId(), displayName, platform: "desktop" });
  if (!res.ok) throw new Error(await readError(res));
  const account = (await res.json()) as Account;
  saveAccount(account);
  return account;
}

export async function resume(): Promise<Account | null> {
  const saved = loadAccount();
  if (saved) return saved;
  const res = await sendJSON("/v1/devices/session", "POST", null, { deviceId: deviceId() });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(await readError(res));
  const account = (await res.json()) as Account;
  saveAccount(account);
  return account;
}

export async function saveProfile(
  token: string,
  fields: { displayName?: string; bio?: string; city?: string; contact?: string },
): Promise<AccountUser> {
  const res = await sendJSON("/v1/me", "PATCH", token, fields);
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { user: AccountUser };
  const current = loadAccount();
  if (current) saveAccount({ ...current, user: body.user });
  return body.user;
}

export async function searchUsers(token: string, query: string): Promise<PublicUser[]> {
  const res = await fetch(`${apiBase()}/v1/users?q=${encodeURIComponent(query)}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { users: PublicUser[] };
  return body.users;
}

export async function photoCards(token: string, ids: string[]): Promise<PhotoCard[]> {
  if (!ids.length) return [];
  const query = ids.map((id) => `id=${encodeURIComponent(id)}`).join("&");
  const res = await fetch(`${apiBase()}/v1/photos?${query}`, { headers: { Authorization: `Bearer ${token}` } });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { photos: PhotoCard[] };
  return body.photos;
}

export async function createShare(token: string, id: string, accessTagValue: string): Promise<void> {
  const res = await sendJSON("/v1/shares", "POST", token, { id, accessTag: accessTagValue });
  if (!res.ok) throw new Error(await readError(res));
}

export async function reserve(token: string, shareID: string, objects: unknown[]): Promise<UploadPlan[]> {
  const res = await sendJSON(`/v1/shares/${shareID}/uploads`, "POST", token, { objects });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { uploads: UploadPlan[] };
  return body.uploads;
}

export async function putCipher(url: string, data: Uint8Array): Promise<string> {
  const res = await fetch(resolveAPI(url), {
    method: "PUT",
    headers: { "Content-Type": "application/octet-stream" },
    body: data as BufferSource,
  });
  if (!res.ok) throw new Error(await readError(res));
  return res.headers.get("ETag") ?? "";
}

export async function complete(
  token: string,
  shareID: string,
  objects: unknown[],
  manifestID: string,
  nonce: string,
): Promise<void> {
  const res = await sendJSON(`/v1/shares/${shareID}/uploads/complete`, "POST", token, {
    objects,
    manifestObjectId: manifestID,
    manifestNonce: nonce,
  });
  if (!res.ok) throw new Error(await readError(res));
}

export async function revoke(token: string, shareID: string): Promise<void> {
  const res = await sendJSON(`/v1/shares/${shareID}`, "DELETE", token);
  if (!res.ok) throw new Error(await readError(res));
}

export class ShareAPI {
  constructor(
    private readonly shareId: string,
    private readonly access: string,
    private readonly token: string,
  ) {}

  private async send(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("X-Imgmesh-Access", this.access);
    headers.set("Authorization", `Bearer ${this.token}`);
    const res = await fetch(`${apiBase()}${path}`, { ...init, headers });
    if (!res.ok) throw new Error(await readError(res));
    return res;
  }

  async manifest(): Promise<{ id: string; nonce: string; url: string }> {
    const res = await this.send(`/v1/shares/${this.shareId}/manifest`);
    return (await res.json()) as { id: string; nonce: string; url: string };
  }

  async objectURL(id: string): Promise<string> {
    const res = await this.send(`/v1/shares/${this.shareId}/objects/${id}`);
    const body = (await res.json()) as { url: string };
    return body.url;
  }

  async event(kind: "view" | "download" | "save" | "remove"): Promise<void> {
    await this.send(`/v1/shares/${this.shareId}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind, client: "desktop" }),
    });
  }

  async activity(): Promise<Activity | null> {
    const res = await fetch(`${apiBase()}/v1/shares/${this.shareId}/events`, {
      headers: { Authorization: `Bearer ${this.token}` },
    });
    if (!res.ok) return null;
    return (await res.json()) as Activity;
  }

  async claim(items: { sha256: string; kind: "shared" | "saved" }[]): Promise<void> {
    if (!items.length) return;
    await this.send(`/v1/shares/${this.shareId}/photos`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items }),
    });
  }
}

export async function fetchCiphertext(url: string): Promise<Uint8Array> {
  const res = await fetch(resolveAPI(url));
  if (!res.ok) throw new Error("download_failed");
  return new Uint8Array(await res.arrayBuffer());
}

export async function openManifest(masterText: string, shareText: string, token: string): Promise<{
  api: ShareAPI;
  manifest: Manifest;
  fileKey: Uint8Array;
  shareRaw: Uint8Array;
}> {
  const master = decode(masterText);
  const shareRaw = decode(shareText);
  if (master.length !== 32 || shareRaw.length !== 16) throw new Error("bad key");
  const api = new ShareAPI(shareText, encode(await accessTag(master, shareRaw)), token);
  const listed = await api.manifest();
  const manifestKey = await derive(master, shareRaw, "imgmesh-manifest-v1");
  const plain = await decrypt(manifestKey, decode(listed.nonce), aad(shareRaw, decode(listed.id)), await fetchCiphertext(listed.url));
  const manifest = JSON.parse(new TextDecoder().decode(plain)) as Manifest;
  if (manifest.v !== 1 || !Array.isArray(manifest.items)) throw new Error("bad manifest");
  return { api, manifest, fileKey: await derive(master, shareRaw, "imgmesh-file-v1"), shareRaw };
}

export async function sealBytes(key: Uint8Array, shareRaw: Uint8Array, objectID: Uint8Array, nonce: Uint8Array, plain: Uint8Array): Promise<Uint8Array> {
  return encrypt(key, nonce, aad(shareRaw, objectID), plain);
}

export { aad, accessTag, decode, decrypt, derive, encode, encrypt, sha256, type Manifest, type ManifestItem };

export function randomBytes(n: number): Uint8Array {
  const out = new Uint8Array(n);
  crypto.getRandomValues(out);
  return out;
}

export function accountMessage(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "name_taken") return "这个名称已经有人用了，换一个。";
  if (code === "bad_request") return "名称用 1 到 24 个字，介绍、城市和联系方式不要太长。";
  if (code === "unauthorized") return "登录已失效，请再登记一次。这台电脑会回到原来的名称。";
  return "没有完成，可以再试一次。";
}

export function shareMessage(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "revoked") return "这份分享已经撤销。";
  if (code === "expired") return "这份分享已经过期。";
  if (code === "access_denied" || code === "bad key") return "链接不完整，或密钥对不上。";
  if (code === "not_found") return "没有找到这份分享。";
  if (code === "quota") return "空间已经用完。";
  if (code === "too_large") return "照片太多，或单张超过 50MB。";
  return "暂时没有完成。";
}
