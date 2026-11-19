export interface ShareMeta {
  id: string;
  photoCount: number;
  totalBytes: number;
  revoked: boolean;
  expired: boolean;
}

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? "request_failed";
  } catch {
    return "request_failed";
  }
}

export class Api {
  constructor(
    private readonly shareId: string,
    private readonly access: string,
  ) {}

  private async send(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("X-Imgmesh-Access", this.access);
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

  async event(kind: "open" | "save" | "remove"): Promise<void> {
    await this.send(`/v1/shares/${this.shareId}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind, client: "web" }),
    });
  }
}

export async function fetchCiphertext(url: string): Promise<Uint8Array> {
  const res = await fetch(url);
  if (!res.ok) throw new Error("download_failed");
  return new Uint8Array(await res.arrayBuffer());
}
