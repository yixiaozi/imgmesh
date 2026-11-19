import {
  Api,
  fetchCiphertext,
  loadAccount,
  loadUser,
  photoCards,
  register,
  resume,
  saveAccount,
  saveProfile,
  searchUsers,
  type Account,
  type Activity,
  type ActivityPerson,
  type PhotoCard,
  type PublicUser,
} from "./api";
import { accessTag, aad, decode, decrypt, derive, encode, sha256, type Manifest, type ManifestItem } from "./crypto";
import { zipStore } from "./zip";

const app = document.querySelector<HTMLElement>("#app");
if (!app) throw new Error("missing app");

const shareId = location.pathname.replace(/\/+$/, "").split("/").pop() ?? "";
const keyParam = new URLSearchParams(location.hash.replace(/^#/, "")).get("k") ?? "";

type Photo = ManifestItem & { preview?: string };

const state: {
  manifest?: Manifest;
  photos: Photo[];
  cards: Record<string, PhotoCard>;
  active: number;
  cardOpen: boolean;
  status: string;
  api?: Api;
  fileKey?: Uint8Array;
  shareRaw?: Uint8Array;
  account: Account | null;
  activity: Activity | null;
  name: string;
  accountError: string;
  registeredNote: string;
  peopleQuery: string;
  people: PublicUser[];
  person: PublicUser | null;
} = {
  photos: [],
  cards: {},
  active: -1,
  cardOpen: false,
  status: "正在打开",
  account: null,
  activity: null,
  name: "",
  accountError: "",
  registeredNote: "",
  peopleQuery: "",
  people: [],
  person: null,
};

document.addEventListener("keydown", (event) => {
  if (state.active < 0) return;
  const target = event.target as HTMLElement | null;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return;
  if (event.key === "ArrowRight") void step(1);
  if (event.key === "ArrowLeft") void step(-1);
  if (event.key === "Escape") {
    state.active = -1;
    state.cardOpen = false;
    render();
  }
});

render();
void start();

function render() {
  if (!state.account) {
    renderRegister();
    return;
  }
  const user = state.account.user;
  const title = state.manifest?.title || "加密照片";
  const active = state.active >= 0 ? state.photos[state.active] : undefined;
  app!.innerHTML = `
    <header class="viewer-bar">
      <a class="brand" href="/"><img class="brand-logo" src="/favicon.svg" width="28" height="28" alt="" /><span>imgmesh</span></a>
      <form class="find" id="find">
        <label>找人 <input name="q" maxlength="24" value="${escapeHTML(state.peopleQuery)}" placeholder="名称、城市或联系方式" /></label>
        <button class="btn" type="submit">查找</button>
      </form>
    </header>
    <main class="viewer">
      <h1>${escapeHTML(title)}</h1>
      <p class="viewer-note">${escapeHTML(state.status)}</p>
      <p class="viewer-note">${escapeHTML(state.registeredNote)}</p>
      <p class="account-error">${escapeHTML(state.accountError)}</p>
      <section class="people" id="people"></section>
      <div class="viewer-actions">
        <button class="btn btn-primary" id="save-all" type="button" ${state.photos.length ? "" : "disabled"}>全部下载</button>
      </div>
      <div class="grid" id="grid"></div>
      <section class="browser" id="browser"></section>
      <form class="profile" id="profile">
        <h2>我的资料</h2>
        <label>名称 <input name="displayName" maxlength="24" value="${escapeHTML(user.displayName)}" /></label>
        <label>介绍 <input name="bio" maxlength="80" value="${escapeHTML(user.bio ?? "")}" /></label>
        <label>城市 <input name="city" maxlength="24" value="${escapeHTML(user.city ?? "")}" /></label>
        <label>联系方式 <input name="contact" maxlength="40" value="${escapeHTML(user.contact ?? "")}" /></label>
        <button class="btn" type="submit">保存资料</button>
      </form>
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
    const badge = document.createElement("span");
    badge.className = "tile-badge";
    badge.textContent = tileLabel(photo);
    button.append(badge);
    button.addEventListener("click", () => {
      state.active = index;
      state.cardOpen = true;
      render();
      void showOriginal(index);
    });
    grid.append(button);
  });
  renderBrowser(active);
  renderPeople();
  document.querySelector("#save-all")?.addEventListener("click", () => void saveAll());
  document.querySelector<HTMLFormElement>("#profile")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    void changeProfile({
      displayName: String(data.get("displayName") ?? ""),
      bio: String(data.get("bio") ?? ""),
      city: String(data.get("city") ?? ""),
      contact: String(data.get("contact") ?? ""),
    });
  });
  document.querySelector<HTMLFormElement>("#find")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    void findPeople(String(data.get("q") ?? ""));
  });
  renderActivity();
}

function renderBrowser(active: Photo | undefined) {
  const root = document.querySelector<HTMLElement>("#browser");
  if (!root || !active) return;
  const card = active.sha256 ? state.cards[active.sha256] : undefined;
  root.innerHTML = `
    <div class="browser-photo" id="stage"></div>
    <aside class="id-card">
      <h2>身份证</h2>
      <p class="id-number">${active.sha256 ? escapeHTML(shortID(active.sha256)) : "打开原图后生成"}</p>
      <p class="id-full">${active.sha256 ? escapeHTML(active.sha256) : ""}</p>
      ${cardBody(card)}
      <div class="browser-actions">
        <button class="btn" id="prev" type="button">上一张</button>
        <button class="btn" id="next" type="button">下一张</button>
        <button class="btn btn-primary" id="download" type="button">下载这张</button>
        <button class="btn" id="close-photo" type="button">回到网格</button>
      </div>
    </aside>
  `;
  const stage = document.querySelector<HTMLElement>("#stage")!;
  if (active.preview && active.mime !== "image/heic" && active.mime !== "image/heif") {
    const img = document.createElement("img");
    img.alt = active.name || "照片";
    img.src = active.preview;
    stage.append(img);
  } else {
    const box = document.createElement("div");
    box.className = "stage-fallback";
    box.textContent =
      active.mime === "image/heic" || active.mime === "image/heif"
        ? "这张是 HEIC。浏览器放不了预览，可以下载原文件。"
        : "可以下载原文件。";
    stage.append(box);
  }
  const film = document.createElement("div");
  film.className = "film";
  state.photos.forEach((photo, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = index === state.active ? "film-btn on" : "film-btn";
    if (photo.preview) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = photo.preview;
      button.append(img);
    }
    button.addEventListener("click", () => {
      state.active = index;
      render();
      void showOriginal(index);
    });
    film.append(button);
  });
  stage.append(film);
  document.querySelector("#prev")?.addEventListener("click", () => void step(-1));
  document.querySelector("#next")?.addEventListener("click", () => void step(1));
  document.querySelector("#download")?.addEventListener("click", () => void saveOne(state.active));
  document.querySelector("#close-photo")?.addEventListener("click", () => {
    state.active = -1;
    render();
  });
  root.querySelectorAll<HTMLButtonElement>("[data-user]").forEach((button) => {
    button.addEventListener("click", () => void openPerson(button.dataset.user ?? ""));
  });
}

function cardBody(card: PhotoCard | undefined): string {
  if (!card) return `<p>这张照片还没有身份证记录。</p>`;
  const shared = card.sharedBy.length
    ? card.sharedBy.map((person) => personButton(person.id, person.displayName)).join("、")
    : "还没有人分享";
  const holders = card.holders.length
    ? card.holders
        .map((person) => {
          const how = person.saved ? "保存了" : "分享了";
          const from =
            person.saved && person.from && person.from.id !== person.id
              ? `，保存自 ${personButton(person.from.id, person.from.displayName)}`
              : "";
          return `<li>${personButton(person.id, person.displayName)} ${how}${from}</li>`;
        })
        .join("")
    : `<li>还没有人登记。</li>`;
  const savers = card.savers.length
    ? card.savers
        .map((person) => {
          const from =
            person.from && person.from.id !== person.id
              ? `保存自 ${personButton(person.from.id, person.from.displayName)}`
              : "自己留下的";
          return `<li>${personButton(person.id, person.displayName)}，${from}</li>`;
        })
        .join("")
    : `<li>还没有人保存。</li>`;
  return `
    <p><strong>${card.sharedBy.length ? "已分享" : "还没分享"}</strong> · ${shared}</p>
    <h3>谁有这张</h3><ul>${holders}</ul>
    <h3>谁保存了</h3><ul>${savers}</ul>
  `;
}

function personButton(id: string, name: string): string {
  return `<button type="button" class="person-link" data-user="${escapeHTML(id)}">${escapeHTML(name || "未命名")}</button>`;
}

function renderPeople() {
  const root = document.querySelector<HTMLElement>("#people");
  if (!root) return;
  if (!state.people.length && !state.person) {
    root.hidden = true;
    return;
  }
  root.hidden = false;
  const list = state.people
    .map(
      (person) =>
        `<li><button type="button" class="person-link" data-user="${escapeHTML(person.id)}">${escapeHTML(person.displayName)}</button> · ${escapeHTML(person.city || "未填城市")} · 分享 ${person.sharedCount} · 保存 ${person.savedCount}</li>`,
    )
    .join("");
  const profile = state.person
    ? `<article class="person-card"><h2>${escapeHTML(state.person.displayName)}</h2><p>${escapeHTML(state.person.bio || "还没有介绍")}</p><p>${escapeHTML(state.person.city || "未填城市")} · ${escapeHTML(state.person.contact || "未填联系方式")}</p><p>分享了 ${state.person.sharedCount} 张，保存了 ${state.person.savedCount} 张。</p></article>`
    : "";
  root.innerHTML = `${profile}<ul>${list}</ul>`;
  root.querySelectorAll<HTMLButtonElement>("[data-user]").forEach((button) => {
    button.addEventListener("click", () => void openPerson(button.dataset.user ?? ""));
  });
}

function tileLabel(photo: Photo): string {
  const card = photo.sha256 ? state.cards[photo.sha256] : undefined;
  if (!card) return photo.sha256 ? "身份证" : "照片";
  const shared = card.sharedBy.length > 0;
  const count = card.holders.length;
  if (shared && count) return `已分享 · ${count} 人有`;
  if (shared) return "已分享";
  if (count) return `${count} 人有`;
  return "未分享";
}

function shortID(id: string): string {
  return `IM-${id.slice(0, 4)} ${id.slice(4, 8)}`;
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
    <h2>这一组链接</h2>
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

async function changeProfile(fields: { displayName: string; bio: string; city: string; contact: string }) {
  if (!state.account) return;
  state.accountError = "";
  try {
    const user = await saveProfile(state.account.token, fields);
    state.account = { ...state.account, user };
    saveAccount(state.account);
    state.registeredNote = `资料已保存。别人可以用名称、城市或联系方式找到「${user.displayName}」。`;
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function findPeople(query: string) {
  if (!state.account) return;
  state.peopleQuery = query.trim();
  state.accountError = "";
  if (!state.peopleQuery) {
    state.people = [];
    render();
    return;
  }
  try {
    state.people = await searchUsers(state.account.token, state.peopleQuery);
    state.person = state.people[0] ?? null;
    if (!state.people.length) state.accountError = "没有找到这个人。";
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function openPerson(id: string) {
  if (!state.account || !id) return;
  try {
    state.person = await loadUser(state.account.token, id);
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function step(delta: number) {
  if (!state.photos.length || state.active < 0) return;
  state.active = (state.active + delta + state.photos.length) % state.photos.length;
  render();
  await showOriginal(state.active);
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
    state.photos = manifest.items.map((item) => ({ ...item }));
    state.api = api;
    state.fileKey = await derive(master, shareRaw, "imgmesh-file-v1");
    state.shareRaw = shareRaw;
    state.status = `${manifest.items.length} 张照片。点开一张可以看身份证：谁分享了、谁有、保存自谁。`;
    state.activity = await api.activity();
    await refreshCards();
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

async function refreshCards() {
  if (!state.account) return;
  const ids = [...new Set(state.photos.map((photo) => photo.sha256).filter((id): id is string => Boolean(id)))];
  if (!ids.length) {
    state.cards = {};
    return;
  }
  const cards = await photoCards(state.account.token, ids).catch(() => []);
  state.cards = Object.fromEntries(cards.map((card) => [card.id, card]));
}

function accountMessage(err: unknown): string {
  const code = err instanceof Error ? err.message : "";
  if (code === "name_taken") return "这个名称已经有人用了，换一个。";
  if (code === "bad_request") return "名称用 1 到 24 个字，介绍、城市和联系方式不要太长。";
  return "没有完成，可以再试一次。";
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
    const digest = await sha256(bytes);
    item.sha256 = digest;
    item.preview = URL.createObjectURL(new Blob([bytes as BlobPart], { type: item.mime }));
    await refreshCards();
    if (state.active === index) render();
  } catch {
    state.status = "这张照片没有解开。";
    render();
  }
}

async function rememberSaved(items: ManifestItem[]) {
  const claims = items
    .filter((item) => item.sha256)
    .map((item) => ({ sha256: item.sha256 as string, kind: "saved" as const }));
  await state.api?.claim(claims).catch(() => undefined);
  await refreshCards();
}

async function saveOne(index: number) {
  const item = state.photos[index];
  if (!item) return;
  const bytes = await originalBytes(item);
  item.sha256 = await sha256(bytes);
  download(new Blob([bytes as BlobPart], { type: item.mime || "application/octet-stream" }), item.name || `photo-${index + 1}`);
  await rememberSaved([item]);
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = "已开始下载，并记到这张照片的身份证上。";
  render();
}

async function saveAll() {
  state.status = "正在打包原图";
  render();
  const names: string[] = [];
  const files: { name: string; data: Uint8Array }[] = [];
  for (const [index, item] of state.photos.entries()) {
    const data = await originalBytes(item);
    item.sha256 = await sha256(data);
    const name = uniqueName(item.name, index, names);
    names.push(name);
    files.push({ name, data });
  }
  download(zipStore(files), "imgmesh.zip");
  await rememberSaved(state.photos);
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = "已开始下载。每张照片的保存记录写在身份证上。";
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
