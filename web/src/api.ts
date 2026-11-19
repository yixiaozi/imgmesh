export interface ShareMeta {
  id: string;
  photoCount: number;
  totalBytes: number;
  revoked: boolean;
  expired: boolean;
  owner?: PublicUser;
}

export interface AccountUser {
  id: string;
  displayName: string;
  bio?: string;
  city?: string;
  contact?: string;
  appleLinked: boolean;
  deviceId: string;
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

export interface Account {
  token: string;
  alreadyRegistered?: boolean;
  user: AccountUser;
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

const deviceKey = "imgmesh-device";
const accountKey = "imgmesh-account";

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? "request_failed";
  } catch {
    return "request_failed";
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

export async function register(displayName: string): Promise<Account> {
  const res = await fetch("/v1/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceId: deviceId(), displayName, platform: "web" }),
  });
  if (!res.ok) throw new Error(await readError(res));
  const account = (await res.json()) as Account;
  saveAccount(account);
  return account;
}

export async function resume(): Promise<Account | null> {
  const saved = loadAccount();
  if (saved) return saved;
  const res = await fetch("/v1/devices/session", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceId: deviceId() }),
  });
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
  const res = await fetch("/v1/me", {
    method: "PATCH",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify(fields),
  });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { user: AccountUser };
  const current = loadAccount();
  if (current) saveAccount({ ...current, user: body.user });
  return body.user;
}

export async function searchUsers(token: string, query: string): Promise<PublicUser[]> {
  const res = await fetch(`/v1/users?q=${encodeURIComponent(query)}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { users: PublicUser[] };
  return body.users;
}

export async function loadUser(token: string, id: string): Promise<PublicUser> {
  const res = await fetch(`/v1/users/${id}`, { headers: { Authorization: `Bearer ${token}` } });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { user: PublicUser };
  return body.user;
}

export async function photoCards(token: string, ids: string[]): Promise<PhotoCard[]> {
  const query = ids.map((id) => `id=${encodeURIComponent(id)}`).join("&");
  const res = await fetch(`/v1/photos?${query}`, { headers: { Authorization: `Bearer ${token}` } });
  if (!res.ok) throw new Error(await readError(res));
  const body = (await res.json()) as { photos: PhotoCard[] };
  return body.photos;
}

export class Api {
  constructor(
    private readonly shareId: string,
    private readonly access: string,
    private readonly token: string,
  ) {}

  private async send(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("X-Imgmesh-Access", this.access);
    headers.set("Authorization", `Bearer ${this.token}`);
    const res = await fetch(path, { ...init, headers });
    if (!res.ok) throw new Error(await readError(res));
    return res;
  }

  async meta(): Promise<ShareMeta> {
    const res = await this.send(`/v1/shares/${this.shareId}`);
    return (await res.json()) as ShareMeta;
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

  async claim(items: { sha256: string; kind: "shared" | "saved" }[]): Promise<void> {
    if (!items.length) return;
    await this.send(`/v1/shares/${this.shareId}/photos`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items }),
    });
  }

  async event(kind: "view" | "download" | "save" | "remove"): Promise<void> {
    await this.send(`/v1/shares/${this.shareId}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind, client: "web" }),
    });
  }

  async activity(): Promise<Activity | null> {
    const res = await fetch(`/v1/shares/${this.shareId}/events`, {
      headers: { Authorization: `Bearer ${this.token}` },
    });
    if (res.status === 404) return null;
    if (!res.ok) return null;
    return (await res.json()) as Activity;
  }
}

export async function fetchCiphertext(url: string): Promise<Uint8Array> {
  const res = await fetch(url);
  if (!res.ok) throw new Error("download_failed");
  return new Uint8Array(await res.arrayBuffer());
}
