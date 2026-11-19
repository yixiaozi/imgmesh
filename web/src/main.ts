import { Api, fetchCiphertext, loadAccount, register, rename, resume, saveAccount, type Account, type Activity, type ActivityPerson } from "./api";
import { accessTag, aad, decode, decrypt, derive, encode, type Manifest, type ManifestItem } from "./crypto";
import { zipStore } from "./zip";

const app = document.querySelector<HTMLElement>("#app");
if (!app) throw new Error("missing app");

const shareId = location.pathname.replace(/\/+$/, "").split("/").pop() ?? "";
const keyParam = new URLSearchParams(location.hash.replace(/^#/, "")).get("k") ?? "";

type Photo = ManifestItem & { preview?: string };

const state: {
  manifest?: Manifest;
  photos: Photo[];
  active: number;
  status: string;
  api?: Api;
  fileKey?: Uint8Array;
  shareRaw?: Uint8Array;
  account: Account | null;
  activity: Activity | null;
  name: string;
  accountError: string;
  registeredNote: string;
} = {
  photos: [],
  active: -1,
  status: "正在打开",
  account: null,
  activity: null,
  name: "",
  accountError: "",
  registeredNote: "",
};

render();
void start();

function render() {
  if (!state.account) {
    renderRegister();
    return;
  }
  const title = state.manifest?.title || "加密照片";
  const active = state.active >= 0 ? state.photos[state.active] : undefined;
  app!.innerHTML = `
    <header class="viewer-bar">
      <a class="brand" href="/"><img class="brand-logo" src="/favicon.svg" width="28" height="28" alt="" /><span>imgmesh</span></a>
      <form class="rename" id="rename">
        <label>我的名称 <input name="displayName" maxlength="24" value="${escapeHTML(state.account.user.displayName)}" /></label>
        <button class="btn" type="submit">保存名称</button>
      </form>
    </header>
    <main class="viewer">
      <h1>${escapeHTML(title)}</h1>
      <p class="viewer-note">${escapeHTML(state.status)}</p>
      <p class="viewer-note">${escapeHTML(state.registeredNote)}</p>
      <p class="account-error">${escapeHTML(state.accountError)}</p>
      <div class="viewer-actions">
        <button class="btn btn-primary" id="save-all" type="button" ${state.photos.length ? "" : "disabled"}>全部下载</button>
      </div>
      <div class="grid" id="grid"></div>
      <div class="stage" id="stage"></div>
      <section class="activity" id="activity"></section>
    </main>
  `;
  const grid = document.querySelector<HTMLElement>("#grid")!;
  state.photos.forEach((photo, index) => {
    const button = document.createElement("button");
    button.className = "tile-btn";
    button.type = "button";
    button.innerHTML = photo.preview
      ? `<img alt="" src="${photo.preview}" />`
      : `<span class="tile-fallback">${escapeHTML(photo.name || "照片")}</span>`;
    button.addEventListener("click", () => {
      state.active = index;
      render();
      void showOriginal(index);
    });
    grid.append(button);
  });
  const stage = document.querySelector<HTMLElement>("#stage")!;
  if (active?.preview && active.mime !== "image/heic" && active.mime !== "image/heif") {
    const img = document.createElement("img");
    img.alt = active.name || "照片";
    img.src = active.preview;
    stage.append(img);
  } else if (active) {
    const box = document.createElement("div");
    box.className = "stage-fallback";
    box.textContent =
      active.mime === "image/heic" || active.mime === "image/heif"
        ? "这张是 HEIC。浏览器放不了预览，可以下载原文件。"
        : "可以下载原文件。";
    stage.append(box);
  }
  if (active) {
    const save = document.createElement("button");
    save.className = "btn btn-primary";
    save.type = "button";
    save.textContent = "下载这张";
    save.addEventListener("click", () => void saveOne(state.active));
    stage.append(save);
  }
  document.querySelector("#save-all")?.addEventListener("click", () => void saveAll());
  document.querySelector<HTMLFormElement>("#rename")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    void changeName(String(data.get("displayName") ?? ""));
  });
  renderActivity();
}

function renderRegister() {
  const broken = !shareId || !keyParam;
  app!.innerHTML = `
    <header class="viewer-bar">
      <a class="brand" href="/"><img class="brand-logo" src="/favicon.svg" width="28" height="28" alt="" /><span>imgmesh</span></a>
    </header>
    <main class="viewer">
      <h1>先登记名称</h1>
      <p class="viewer-note">${escapeHTML(broken ? state.status : "每台设备只能注册一次。名称不能和别人重复。注册之后才能查看和下载，这样发送者能看到是谁。")}</p>
      <form class="account" id="register">
        <label>名称 <input name="displayName" maxlength="24" required value="${escapeHTML(state.name)}" ${broken ? "disabled" : ""} /></label>
        <button class="btn btn-primary" type="submit" ${broken ? "disabled" : ""}>注册</button>
        <p class="account-error">${escapeHTML(state.accountError)}</p>
      </form>
    </main>
  `;
  const input = document.querySelector<HTMLInputElement>('input[name="displayName"]');
  input?.addEventListener("input", () => {
    state.name = input.value;
  });
  document.querySelector<HTMLFormElement>("#register")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    void signUp(String(data.get("displayName") ?? ""));
  });
}

function renderActivity() {
  const root = document.querySelector<HTMLElement>("#activity");
  if (!root || !state.activity) return;
  const { viewed, downloaded, phones } = state.activity;
  root.innerHTML = `
    <h2>记录</h2>
    ${listBlock("谁查看过", viewed, (person) => `${person.count ?? 1} 次`)}
    ${listBlock("谁下载过", downloaded, (person) => `${person.count ?? 1} 次`)}
    ${listBlock("在这些手机里", phones, () => "已存进相册")}
  `;
}

function listBlock(title: string, people: ActivityPerson[], extra: (person: ActivityPerson) => string): string {
  const items = people.length
    ? people.map((person) => `<li><strong>${escapeHTML(person.displayName || "未命名")}</strong> · ${escapeHTML(clientLabel(person.client))} · ${escapeHTML(extra(person))}</li>`).join("")
    : `<li>还没有记录。</li>`;
  return `<h3>${escapeHTML(title)}</h3><ul>${items}</ul>`;
}

function clientLabel(client: string): string {
  if (client === "ios") return "iPhone";
  if (client === "web") return "浏览器";
  return client;
}

async function start() {
  if (!shareId || !keyParam) {
    state.status = "这个链接里没有密钥。请使用完整链接，井号后面的部分也要保留。";
    render();
    return;
  }
  state.account = loadAccount() ?? (await resume().catch(() => null));
  if (!state.account) {
    state.status = "";
    render();
    return;
  }
  await openShare();
}

async function signUp(name: string) {
  state.accountError = "";
  state.name = name;
  try {
    const account = await register(name);
    state.account = account;
    state.registeredNote = account.alreadyRegistered
      ? `这台设备已经注册为「${account.user.displayName}」。名称保持第一次填写的。`
      : "";
    render();
    await openShare();
  } catch (err) {
    state.accountError = accountMessage(err);
    renderRegister();
  }
}

async function changeName(name: string) {
  if (!state.account) return;
  state.accountError = "";
  try {
    const user = await rename(state.account.token, name);
    state.account = { ...state.account, user };
    saveAccount(state.account);
    state.registeredNote = `名称已改为「${user.displayName}」。`;
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function openShare() {
  if (!state.account) return;
  try {
    const master = decode(keyParam);
    const shareRaw = decode(shareId);
    if (master.length !== 32 || shareRaw.length !== 16) throw new Error("bad key");
    const api = new Api(shareId, encode(await accessTag(master, shareRaw)), state.account.token);
    const meta = await api.meta();
    if (meta.revoked) throw new Error("revoked");
    if (meta.expired) throw new Error("expired");
    const listed = await api.manifest();
    const manifestKey = await derive(master, shareRaw, "imgmesh-manifest-v1");
    const manifestBytes = await decrypt(
      manifestKey,
      decode(listed.nonce),
      aad(shareRaw, decode(listed.id)),
      await fetchCiphertext(listed.url),
    );
    const manifest = JSON.parse(new TextDecoder().decode(manifestBytes)) as Manifest;
    if (manifest.v !== 1 || !Array.isArray(manifest.items)) throw new Error("bad manifest");
    state.manifest = manifest;
    state.photos = manifest.items;
    state.api = api;
    state.fileKey = await derive(master, shareRaw, "imgmesh-file-v1");
    state.shareRaw = shareRaw;
    state.status = `${manifest.items.length} 张照片。密钥没有发给服务器。你是「${state.account.user.displayName}」。`;
    state.activity = await api.activity();
    render();
    await api.event("view").catch(() => undefined);
    state.activity = await api.activity();
    await Promise.all(manifest.items.map((item, index) => loadPreview(index, item)));
    render();
  } catch (err) {
    if (err instanceof Error && err.message === "unauthorized") {
      localStorage.removeItem("imgmesh-account");
      state.account = null;
      state.accountError = "登录已失效，请再登记一次。这台设备会回到原来的名称。";
      render();
      return;
    }
    state.status = message(err);
    render();
  }
}

function accountMessage(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "name_taken") return "这个名称已经有人用了，换一个。";
  if (code === "bad_request") return "名称用 1 到 24 个字。";
  return "注册没有完成，可以再试一次。";
}

function message(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "revoked") return "这份分享已经撤销。";
  if (code === "expired") return "这份分享已经过期。";
  if (code === "access_denied" || code === "bad key") return "链接不完整，或密钥对不上。";
  if (code === "not_found") return "没有找到这份分享。";
  if (code === "name_required") return "先登记一个名称。";
  return "暂时打不开这份分享。";
}

async function loadPreview(index: number, item: ManifestItem) {
  if (!item.thumb || !state.fileKey || !state.shareRaw || !state.api) return;
  try {
    const url = await state.api.objectURL(item.thumb.id);
    const bytes = await decrypt(
      state.fileKey,
      decode(item.thumb.nonce),
      aad(state.shareRaw, decode(item.thumb.id)),
      await fetchCiphertext(url),
    );
    state.photos[index].preview = URL.createObjectURL(new Blob([bytes as BlobPart], { type: "image/jpeg" }));
  } catch {
    /* 缩略图打不开时仍可下载原图 */
  }
}

async function originalBytes(item: ManifestItem): Promise<Uint8Array> {
  if (!state.fileKey || !state.shareRaw || !state.api) throw new Error("closed");
  const url = await state.api.objectURL(item.id);
  return decrypt(
    state.fileKey,
    decode(item.nonce),
    aad(state.shareRaw, decode(item.id)),
    await fetchCiphertext(url),
  );
}

async function showOriginal(index: number) {
  const item = state.photos[index];
  if (!item || item.mime === "image/heic" || item.mime === "image/heif") return;
  try {
    const bytes = await originalBytes(item);
    item.preview = URL.createObjectURL(new Blob([bytes as BlobPart], { type: item.mime }));
    render();
  } catch {
    state.status = "这张照片没有解开。";
    render();
  }
}

async function saveOne(index: number) {
  const item = state.photos[index];
  if (!item) return;
  const bytes = await originalBytes(item);
  download(new Blob([bytes as BlobPart], { type: item.mime || "application/octet-stream" }), item.name || `photo-${index + 1}`);
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = "已开始下载。";
  render();
}

async function saveAll() {
  state.status = "正在打包原图";
  render();
  const names: string[] = [];
  const files: { name: string; data: Uint8Array }[] = [];
  for (const [index, item] of state.photos.entries()) {
    const data = await originalBytes(item);
    const name = uniqueName(item.name, index, names);
    names.push(name);
    files.push({ name, data });
  }
  download(zipStore(files), "imgmesh.zip");
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = "已开始下载。照片在压缩包里。";
  render();
}

function uniqueName(name: string | undefined, index: number, used: string[]): string {
  const base = name && name.trim() ? name.trim() : `photo-${index + 1}.jpg`;
  if (!used.includes(base)) return base;
  return `${index + 1}-${base}`;
}

function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

function escapeHTML(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}
