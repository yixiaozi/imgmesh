import {
  accountMessage,
  complete,
  createShare,
  decrypt,
  deviceId,
  encode,
  fetchCiphertext,
  loadAccount,
  openManifest,
  photoCards,
  putCipher,
  randomBytes,
  register,
  reserve,
  resume,
  revoke,
  saveAccount,
  saveProfile,
  searchUsers,
  sha256,
  shareMessage,
  aad,
  accessTag,
  decode,
  derive,
  encrypt,
  type Account,
  type Activity,
  type ManifestItem,
  type PhotoCard,
  type PublicUser,
  type UploadPlan,
} from "./api";

type LocalShare = {
  id: string;
  masterKey: string;
  title: string;
  role: "sent" | "received";
  photoCount: number;
};

type Photo = ManifestItem & { preview?: string };

const libraryKey = "imgmesh-desktop-library";
const app = document.querySelector<HTMLElement>("#app");
if (!app) throw new Error("missing app");

const state: {
  account: Account | null;
  shares: LocalShare[];
  selected: string;
  photos: Photo[];
  cards: Record<string, PhotoCard>;
  active: number;
  activity: Activity | null;
  status: string;
  error: string;
  name: string;
  bio: string;
  city: string;
  contact: string;
  title: string;
  pasted: string;
  query: string;
  people: PublicUser[];
  person: PublicUser | null;
  link: string;
  busy: boolean;
  fileKey?: Uint8Array;
  shareRaw?: Uint8Array;
  api?: Awaited<ReturnType<typeof openManifest>>["api"];
} = {
  account: null,
  shares: loadLibrary(),
  selected: "",
  photos: [],
  cards: {},
  active: -1,
  activity: null,
  status: "",
  error: "",
  name: "",
  bio: "",
  city: "",
  contact: "",
  title: "",
  pasted: "",
  query: "",
  people: [],
  person: null,
  link: "",
  busy: false,
};

document.addEventListener("keydown", (event) => {
  const target = event.target as HTMLElement | null;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return;
  if (state.active < 0) return;
  if (event.key === "ArrowRight") void step(1);
  if (event.key === "ArrowLeft") void step(-1);
  if (event.key === "Escape") {
    state.active = -1;
    render();
  }
});

render();
void boot();

function osLabel(): string {
  const os = window.imgmesh?.os;
  if (os === "darwin") return "Mac";
  if (os === "win32") return "Windows";
  if (os === "linux") return "Linux";
  return "Windows / Mac";
}

function render() {
  const user = state.account?.user;
  app!.innerHTML = `
    <div class="app">
      <aside class="side">
        <h1 class="brand">imgmesh</h1>
        <p class="quiet">${escapeHTML(osLabel())} · 同一套客户端</p>
        <p class="error">${escapeHTML(state.error)}</p>
        <p class="note">${escapeHTML(state.status)}</p>
        ${user ? profileForm(user) : registerForm()}
        <form id="find" class="block">
          <label>找人 <input name="q" maxlength="24" value="${escapeHTML(state.query)}" placeholder="名称、城市或联系方式" /></label>
          <button class="btn" type="submit" ${user ? "" : "disabled"}>查找</button>
        </form>
        <div id="people"></div>
        <form id="send" class="block">
          <label>标题 <input name="title" maxlength="40" value="${escapeHTML(state.title)}" placeholder="例如三亚" /></label>
          <input id="files" type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif,.heic,.heif" multiple hidden />
          <button class="btn btn-primary" id="pick" type="button" ${user && !state.busy ? "" : "disabled"}>选择照片并分享</button>
        </form>
        <form id="open" class="block">
          <label>打开链接 <input name="link" value="${escapeHTML(state.pasted)}" placeholder="粘贴完整链接" /></label>
          <button class="btn" type="submit" ${user ? "" : "disabled"}>打开</button>
        </form>
        <h2>我的链接</h2>
        <ul class="shares" id="shares"></ul>
      </aside>
      <main class="main" id="main"></main>
    </div>
  `;
  renderPeople();
  renderShares();
  renderMain();
  bind();
}

function profileForm(user: Account["user"]): string {
  return `
    <form id="profile" class="block">
      <label>名称 <input name="displayName" maxlength="24" value="${escapeHTML(state.name || user.displayName)}" /></label>
      <label>介绍 <input name="bio" maxlength="80" value="${escapeHTML(state.bio || user.bio || "")}" /></label>
      <label>城市 <input name="city" maxlength="24" value="${escapeHTML(state.city || user.city || "")}" /></label>
      <label>联系方式 <input name="contact" maxlength="40" value="${escapeHTML(state.contact || user.contact || "")}" /></label>
      <button class="btn" type="submit">保存资料</button>
    </form>
  `;
}

function registerForm(): string {
  return `
    <form id="register" class="block">
      <p class="note">每台电脑只能注册一次，名称不能和别人重复。</p>
      <label>名称 <input name="displayName" maxlength="24" required value="${escapeHTML(state.name)}" /></label>
      <button class="btn btn-primary" type="submit">注册</button>
    </form>
  `;
}

function renderPeople() {
  const root = document.querySelector<HTMLElement>("#people");
  if (!root) return;
  if (!state.person && !state.people.length) {
    root.replaceChildren();
    return;
  }
  const card = document.createElement("article");
  card.className = "person-card";
  if (state.person) {
    card.innerHTML = `<h2>${escapeHTML(state.person.displayName)}</h2><p>${escapeHTML(state.person.bio || "还没有介绍")}</p><p>${escapeHTML(state.person.city || "未填城市")} · ${escapeHTML(state.person.contact || "未填联系方式")}</p><p>分享了 ${state.person.sharedCount} 张，保存了 ${state.person.savedCount} 张。</p>`;
  }
  const list = document.createElement("ul");
  for (const person of state.people) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.className = "person-link";
    button.type = "button";
    button.textContent = person.displayName;
    button.addEventListener("click", () => {
      state.person = person;
      render();
    });
    item.append(button, ` · ${person.city || "未填城市"} · 分享 ${person.sharedCount} · 保存 ${person.savedCount}`);
    list.append(item);
  }
  root.replaceChildren(card, list);
}

function renderShares() {
  const list = document.querySelector<HTMLElement>("#shares");
  if (!list) return;
  if (!state.shares.length) {
    const empty = document.createElement("p");
    empty.className = "note";
    empty.textContent = "还没有分享。";
    list.replaceWith(empty);
    empty.id = "shares";
    return;
  }
  for (const share of state.shares) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = share.id === state.selected ? "on" : "";
    button.textContent = `${share.title} · ${share.role === "sent" ? "发出" : "收到"}`;
    button.addEventListener("click", () => void openShare(share));
    item.append(button);
    list.append(item);
  }
}

function renderMain() {
  const main = document.querySelector<HTMLElement>("#main");
  if (!main) return;
  const share = state.shares.find((item) => item.id === state.selected);
  if (!share) {
    main.innerHTML = `
      <h1>在电脑上收发照片</h1>
      <p class="note">Windows 和 Mac 用的是同一套 imgmesh。照片在这台电脑上加密，密钥只留在链接的井号后面。点开一张照片，可以看到身份证：谁分享了、谁有、保存自谁、谁保存了。</p>
      ${state.link ? `<p class="link-box" id="new-link"></p><button class="btn" id="copy-link" type="button">复制链接</button>` : ""}
    `;
    const box = document.querySelector("#new-link");
    if (box) box.textContent = state.link;
    document.querySelector("#copy-link")?.addEventListener("click", () => void copyLink());
    return;
  }
  main.innerHTML = `<h1></h1><p class="note"></p><div class="actions"></div><div class="grid" id="grid"></div><section class="browser" id="browser"></section><section id="activity"></section>`;
  main.querySelector("h1")!.textContent = share.title;
  main.querySelector("p")!.textContent = state.photos.length ? `${state.photos.length} 张照片` : "正在打开";
  const actions = main.querySelector(".actions")!;
  const save = button("保存到文件夹", () => void saveFolder(share));
  save.classList.add("btn-primary");
  actions.append(save, button("从这台电脑移除", () => void removeShare(share)));
  if (share.role === "sent") actions.append(button("撤销链接", () => void revokeShare(share)));
  const grid = main.querySelector<HTMLElement>("#grid")!;
  state.photos.forEach((photo, index) => {
    const tile = document.createElement("button");
    tile.className = "tile-btn";
    tile.type = "button";
    if (photo.preview) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = photo.preview;
      tile.append(img);
    }
    const badge = document.createElement("span");
    badge.className = "tile-badge";
    badge.textContent = tileLabel(photo);
    tile.append(badge);
    tile.addEventListener("click", () => {
      state.active = index;
      render();
      void showOriginal(index);
    });
    grid.append(tile);
  });
  renderBrowser();
  renderActivity();
}

function renderBrowser() {
  const root = document.querySelector<HTMLElement>("#browser");
  const photo = state.active >= 0 ? state.photos[state.active] : undefined;
  if (!root || !photo) return;
  const card = photo.sha256 ? state.cards[photo.sha256] : undefined;
  const photoBox = document.createElement("div");
  if (photo.preview) {
    const img = document.createElement("img");
    img.className = "stage";
    img.alt = photo.name || "照片";
    img.src = photo.preview;
    photoBox.append(img);
  }
  const film = document.createElement("div");
  film.className = "film";
  state.photos.forEach((item, index) => {
    const thumb = document.createElement("button");
    thumb.type = "button";
    thumb.className = index === state.active ? "film-btn on" : "film-btn";
    if (item.preview) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = item.preview;
      thumb.append(img);
    }
    thumb.addEventListener("click", () => {
      state.active = index;
      render();
      void showOriginal(index);
    });
    film.append(thumb);
  });
  photoBox.append(film);
  const aside = document.createElement("aside");
  aside.className = "id-card";
  aside.append(heading("身份证"), paragraph(photo.sha256 ? shortID(photo.sha256) : "打开原图后生成", "id-number"));
  if (photo.sha256) aside.append(paragraph(photo.sha256, "id-full"));
  aside.append(paragraph(card && card.sharedBy.length ? `已分享 · ${names(card.sharedBy)}` : "还没分享"));
  aside.append(heading("谁有这张", "h3"));
  aside.append(holderList(card?.holders ?? []));
  aside.append(heading("谁保存了", "h3"));
  aside.append(saverList(card?.savers ?? []));
  const actions = document.createElement("div");
  actions.className = "actions";
  actions.append(
    button("上一张", () => void step(-1)),
    button("下一张", () => void step(1)),
    button("回到网格", () => {
      state.active = -1;
      render();
    }),
  );
  aside.append(actions);
  root.replaceChildren(photoBox, aside);
}

function renderActivity() {
  const root = document.querySelector<HTMLElement>("#activity");
  const share = state.shares.find((item) => item.id === state.selected);
  if (!root || !state.activity || share?.role !== "sent") return;
  root.append(
    heading("这一组链接"),
    heading("谁查看过", "h3"),
    peopleList(state.activity.viewed, (person) => `${person.count ?? 1} 次`),
    heading("谁下载过", "h3"),
    peopleList(state.activity.downloaded, (person) => `${person.count ?? 1} 次`),
    heading("保存在这些设备里", "h3"),
    peopleList(state.activity.phones, (person) => (person.client === "desktop" ? "已存进文件夹" : "已存进相册")),
  );
}

function bind() {
  document.querySelector<HTMLFormElement>("#register")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    state.name = String(data.get("displayName") ?? "");
    void signUp();
  });
  document.querySelector<HTMLFormElement>("#profile")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    state.name = String(data.get("displayName") ?? "");
    state.bio = String(data.get("bio") ?? "");
    state.city = String(data.get("city") ?? "");
    state.contact = String(data.get("contact") ?? "");
    void updateProfile();
  });
  document.querySelector<HTMLFormElement>("#find")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    state.query = String(data.get("q") ?? "");
    void findPeople();
  });
  document.querySelector<HTMLFormElement>("#open")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    state.pasted = String(data.get("link") ?? "");
    acceptLink(state.pasted);
  });
  document.querySelector("#pick")?.addEventListener("click", () => document.querySelector<HTMLInputElement>("#files")?.click());
  document.querySelector<HTMLInputElement>("#files")?.addEventListener("change", (event) => {
    const input = event.currentTarget as HTMLInputElement;
    const files = Array.from(input.files ?? []);
    input.value = "";
    const title = String(new FormData(document.querySelector<HTMLFormElement>("#send")!).get("title") ?? "");
    state.title = title;
    void sendPhotos(files, title);
  });
}

async function boot() {
  deviceId();
  try {
    state.account = loadAccount() ?? (await resume());
    if (state.account) {
      state.name = state.account.user.displayName;
      state.bio = state.account.user.bio ?? "";
      state.city = state.account.user.city ?? "";
      state.contact = state.account.user.contact ?? "";
    }
  } catch (err) {
    state.error = accountMessage(err);
  }
  render();
}

async function signUp() {
  state.error = "";
  try {
    state.account = await register(state.name);
    state.status = state.account.alreadyRegistered
      ? `这台电脑已经注册为「${state.account.user.displayName}」。`
      : `已注册为「${state.account.user.displayName}」。现在可以分享照片。`;
    render();
  } catch (err) {
    state.error = accountMessage(err);
    render();
  }
}

async function updateProfile() {
  if (!state.account) return;
  state.error = "";
  try {
    const user = await saveProfile(state.account.token, {
      displayName: state.name,
      bio: state.bio,
      city: state.city,
      contact: state.contact,
    });
    state.account = { ...state.account, user };
    saveAccount(state.account);
    state.status = "资料已保存。别人可以用名称、城市或联系方式找到你。";
    render();
  } catch (err) {
    state.error = accountMessage(err);
    render();
  }
}

async function findPeople() {
  if (!state.account) return;
  state.error = "";
  const query = state.query.trim();
  if (!query) {
    state.people = [];
    state.person = null;
    render();
    return;
  }
  try {
    state.people = await searchUsers(state.account.token, query);
    state.person = state.people[0] ?? null;
    if (!state.people.length) state.error = "没有找到这个人。";
    render();
  } catch (err) {
    state.error = accountMessage(err);
    render();
  }
}

async function sendPhotos(files: File[], title: string) {
  if (!state.account || state.busy || !files.length) return;
  if (files.length > 200) {
    state.error = "一次最多 200 张。";
    render();
    return;
  }
  state.busy = true;
  state.error = "";
  state.link = "";
  state.status = "正在读取照片";
  render();
  try {
    const shareRaw = randomBytes(16);
    const master = randomBytes(32);
    const shareText = encode(shareRaw);
    const masterText = encode(master);
    const tag = encode(await accessTag(master, shareRaw));
    await createShare(state.account.token, shareText, tag);
    const fileKey = await derive(master, shareRaw, "imgmesh-file-v1");
    const items: ManifestItem[] = [];
    const blobs = new Map<string, Uint8Array>();
    let total = 0;
    for (const [index, file] of files.entries()) {
      state.status = `正在加密 ${index + 1}/${files.length}`;
      renderStatus();
      const plain = new Uint8Array(await file.arrayBuffer());
      if (plain.byteLength > 50 * 1024 * 1024) throw new Error("too_large");
      total += plain.byteLength;
      if (total > 2 * 1024 * 1024 * 1024) throw new Error("too_large");
      const mime = mimeOf(file);
      const objectID = randomBytes(16);
      const nonce = randomBytes(12);
      const objectText = encode(objectID);
      blobs.set(objectText, await encrypt(fileKey, nonce, aad(shareRaw, objectID), plain));
      const thumbPlain = await thumbnail(plain, mime);
      let thumb: ManifestItem["thumb"];
      if (thumbPlain) {
        const thumbID = randomBytes(16);
        const thumbNonce = randomBytes(12);
        const thumbText = encode(thumbID);
        blobs.set(thumbText, await encrypt(fileKey, thumbNonce, aad(shareRaw, thumbID), thumbPlain));
        thumb = { id: thumbText, bytes: thumbPlain.byteLength, nonce: encode(thumbNonce) };
      }
      items.push({
        id: objectText,
        name: file.name,
        mime,
        bytes: plain.byteLength,
        nonce: encode(nonce),
        sha256: await sha256(plain),
        thumb,
      });
    }
    const manifest = { v: 1, title: title.trim() || "旅行照片", items };
    const manifestPlain = new TextEncoder().encode(JSON.stringify(manifest));
    const manifestID = randomBytes(16);
    const manifestNonce = randomBytes(12);
    const manifestText = encode(manifestID);
    const manifestKey = await derive(master, shareRaw, "imgmesh-manifest-v1");
    blobs.set(manifestText, await encrypt(manifestKey, manifestNonce, aad(shareRaw, manifestID), manifestPlain));
    const objects = [];
    for (const item of items) {
      objects.push(payload(item.id, "original", item.bytes, blobs.get(item.id)?.byteLength ?? 0));
      if (item.thumb) objects.push(payload(item.thumb.id, "thumb", item.thumb.bytes, blobs.get(item.thumb.id)?.byteLength ?? 0));
    }
    objects.push(payload(manifestText, "manifest", manifestPlain.byteLength, blobs.get(manifestText)?.byteLength ?? 0));
    state.status = "正在上传";
    renderStatus();
    const plans = await reserve(state.account.token, shareText, objects);
    const uploaded = [];
    for (const plan of plans) {
      const bytes = blobs.get(plan.id);
      if (!bytes) throw new Error("upload_failed");
      uploaded.push(await uploadPlan(plan, bytes));
    }
    await complete(state.account.token, shareText, uploaded, manifestText, encode(manifestNonce));
    const api = (await openManifest(masterText, shareText, state.account.token)).api;
    await api.claim(items.filter((item) => item.sha256).map((item) => ({ sha256: item.sha256 as string, kind: "shared" as const }))).catch(() => undefined);
    remember({ id: shareText, masterKey: masterText, title: manifest.title, role: "sent", photoCount: items.length });
    state.link = `https://imgmesh.com/s/${shareText}#k=${masterText}`;
    state.status = "链接已经准备好。发给对方时要带上井号后面的密钥。";
    state.selected = "";
    state.photos = [];
    state.busy = false;
    render();
  } catch (err) {
    state.busy = false;
    state.status = "";
    state.error = shareMessage(err);
    render();
  }
}

function acceptLink(text: string) {
  const parsed = parseShare(text);
  if (!parsed) {
    state.error = "这个链接里没有密钥。请粘贴完整链接，井号后面的部分也要保留。";
    render();
    return;
  }
  if (!state.shares.some((share) => share.id === parsed.id)) {
    remember({ id: parsed.id, masterKey: parsed.key, title: "收到的照片", role: "received", photoCount: 0 });
  }
  const share = state.shares.find((item) => item.id === parsed.id);
  if (share) void openShare(share);
}

async function openShare(share: LocalShare) {
  if (!state.account) return;
  state.selected = share.id;
  state.photos = [];
  state.active = -1;
  state.activity = null;
  state.error = "";
  state.status = "正在解密";
  render();
  try {
    const opened = await openManifest(share.masterKey, share.id, state.account.token);
    state.api = opened.api;
    state.fileKey = opened.fileKey;
    state.shareRaw = opened.shareRaw;
    state.photos = opened.manifest.items.map((item) => ({ ...item }));
    const current = state.shares.find((item) => item.id === share.id);
    if (current) {
      current.title = opened.manifest.title || current.title;
      current.photoCount = state.photos.length;
      saveLibrary();
    }
    await refreshCards();
    await opened.api.event("view").catch(() => undefined);
    state.activity = await opened.api.activity();
    state.status = "";
    render();
    await Promise.all(state.photos.map((item, index) => loadPreview(index, item)));
    render();
  } catch (err) {
    if (err instanceof Error && err.message === "unauthorized") {
      localStorage.removeItem("imgmesh-desktop-account");
      state.account = null;
    }
    state.status = "";
    state.error = shareMessage(err);
    render();
  }
}

async function saveFolder(share: LocalShare) {
  if (!state.account || !state.fileKey || !state.shareRaw || !state.api) return;
  state.error = "";
  try {
    const files: { name: string; data: Uint8Array }[] = [];
    const names: string[] = [];
    for (const [index, item] of state.photos.entries()) {
      const data = await originalBytes(item);
      item.sha256 = await sha256(data);
      const name = uniqueName(item.name, index, names);
      names.push(name);
      files.push({ name, data });
    }
    if (window.imgmesh) {
      const dir = await window.imgmesh.saveFiles(files);
      if (!dir) return;
      state.status = `已经存进 ${dir}`;
    } else {
      for (const file of files) download(file.data, file.name, state.photos.find((item) => item.name === file.name)?.mime);
      state.status = "已开始下载。";
    }
    await state.api.claim(state.photos.filter((item) => item.sha256).map((item) => ({ sha256: item.sha256 as string, kind: "saved" as const }))).catch(() => undefined);
    await state.api.event("save").catch(() => undefined);
    await refreshCards();
    state.activity = await state.api.activity();
    render();
  } catch (err) {
    state.error = shareMessage(err);
    render();
  }
}

async function removeShare(share: LocalShare) {
  await state.api?.event("remove").catch(() => undefined);
  state.shares = state.shares.filter((item) => item.id !== share.id);
  saveLibrary();
  if (state.selected === share.id) {
    state.selected = "";
    state.photos = [];
    state.active = -1;
  }
  render();
}

async function revokeShare(share: LocalShare) {
  if (!state.account) return;
  try {
    await revoke(state.account.token, share.id);
    await removeShare(share);
    state.status = "链接已经失效。对方已经保存的照片还在。";
    render();
  } catch (err) {
    state.error = shareMessage(err);
    render();
  }
}

async function step(delta: number) {
  if (!state.photos.length || state.active < 0) return;
  state.active = (state.active + delta + state.photos.length) % state.photos.length;
  render();
  await showOriginal(state.active);
}

async function showOriginal(index: number) {
  const item = state.photos[index];
  if (!item || item.mime === "image/heic" || item.mime === "image/heif") return;
  try {
    const bytes = await originalBytes(item);
    item.sha256 = await sha256(bytes);
    item.preview = URL.createObjectURL(new Blob([bytes as BlobPart], { type: item.mime }));
    await refreshCards();
    if (state.active === index) render();
  } catch {
    state.error = "这张照片没有解开。";
    render();
  }
}

async function loadPreview(index: number, item: ManifestItem) {
  if (!item.thumb || !state.fileKey || !state.shareRaw || !state.api) return;
  try {
    const url = await state.api.objectURL(item.thumb.id);
    const bytes = await decrypt(state.fileKey, decode(item.thumb.nonce), aad(state.shareRaw, decode(item.thumb.id)), await fetchCiphertext(url));
    state.photos[index].preview = URL.createObjectURL(new Blob([bytes as BlobPart], { type: "image/jpeg" }));
  } catch {
    /* 缩略图打不开时仍可保存原图 */
  }
}

async function originalBytes(item: ManifestItem): Promise<Uint8Array> {
  if (!state.fileKey || !state.shareRaw || !state.api) throw new Error("closed");
  const url = await state.api.objectURL(item.id);
  return decrypt(state.fileKey, decode(item.nonce), aad(state.shareRaw, decode(item.id)), await fetchCiphertext(url));
}

async function refreshCards() {
  if (!state.account) return;
  const ids = [...new Set(state.photos.map((photo) => photo.sha256).filter((id): id is string => Boolean(id)))];
  const cards = await photoCards(state.account.token, ids).catch(() => []);
  state.cards = Object.fromEntries(cards.map((card) => [card.id, card]));
}

async function uploadPlan(plan: UploadPlan, payload: Uint8Array) {
  const parts: { partNumber: number; etag: string }[] = [];
  if (!plan.ready) {
    let offset = 0;
    const chunk = plan.partSize || payload.byteLength;
    for (const part of plan.parts) {
      const end = part.partNumber === 0 ? payload.byteLength : Math.min(offset + Math.max(chunk, 1), payload.byteLength);
      const etag = await putCipher(part.url, payload.subarray(offset, end));
      parts.push({ partNumber: part.partNumber, etag });
      if (part.partNumber > 0) offset = end;
    }
  }
  return { id: plan.id, byteSize: payload.byteLength, parts };
}

async function thumbnail(bytes: Uint8Array, mime: string): Promise<Uint8Array | null> {
  if (mime === "image/heic" || mime === "image/heif") return null;
  try {
    const bitmap = await createImageBitmap(new Blob([bytes as BlobPart], { type: mime }));
    const scale = Math.min(1, 480 / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.7));
    if (!blob || blob.size > 2 * 1024 * 1024) return null;
    return new Uint8Array(await blob.arrayBuffer());
  } catch {
    return null;
  }
}

function mimeOf(file: File): string {
  const name = file.name.toLowerCase();
  if (file.type === "image/png" || name.endsWith(".png")) return "image/png";
  if (file.type === "image/webp" || name.endsWith(".webp")) return "image/webp";
  if (name.endsWith(".heic")) return "image/heic";
  if (name.endsWith(".heif")) return "image/heif";
  return "image/jpeg";
}

function payload(id: string, kind: string, plain: number, cipher: number) {
  return { id, kind, plaintextBytes: plain, ciphertextBytes: cipher };
}

function parseShare(text: string): { id: string; key: string } | null {
  try {
    const url = new URL(text.trim());
    const host = url.hostname.toLowerCase();
    if (host !== "imgmesh.com" && host !== "www.imgmesh.com") return null;
    const parts = url.pathname.split("/").filter(Boolean);
    if (parts.length !== 2 || parts[0] !== "s") return null;
    const key = new URLSearchParams(url.hash.replace(/^#/, "")).get("k") ?? "";
    if (!key) return null;
    return { id: parts[1], key };
  } catch {
    return null;
  }
}

function remember(share: LocalShare) {
  state.shares = [share, ...state.shares.filter((item) => item.id !== share.id)];
  saveLibrary();
}

function loadLibrary(): LocalShare[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(libraryKey) ?? "[]") as LocalShare[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function saveLibrary() {
  localStorage.setItem(libraryKey, JSON.stringify(state.shares));
}

async function copyLink() {
  if (window.imgmesh) await window.imgmesh.copy(state.link);
  else await navigator.clipboard.writeText(state.link);
  state.status = "已复制。发给对方时保留井号后的密钥。";
  render();
}

function tileLabel(photo: Photo): string {
  const card = photo.sha256 ? state.cards[photo.sha256] : undefined;
  if (!card) return "照片";
  const count = card.holders.length;
  if (card.sharedBy.length) return count ? `已分享 · ${count} 人有` : "已分享";
  return count ? `${count} 人有` : "未分享";
}

function shortID(id: string): string {
  return `IM-${id.slice(0, 4)} ${id.slice(4, 8)}`;
}

function names(people: { displayName: string }[]): string {
  return people.map((person) => person.displayName || "未命名").join("、");
}

function uniqueName(name: string | undefined, index: number, used: string[]): string {
  const base = name && name.trim() ? name.trim() : `photo-${index + 1}.jpg`;
  if (!used.includes(base)) return base;
  return `${index + 1}-${base}`;
}

function download(data: Uint8Array, name: string, mime = "image/jpeg") {
  const url = URL.createObjectURL(new Blob([data as BlobPart], { type: mime }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

function button(text: string, onClick: () => void): HTMLButtonElement {
  const node = document.createElement("button");
  node.className = "btn";
  node.type = "button";
  node.textContent = text;
  node.addEventListener("click", onClick);
  return node;
}

function heading(text: string, tag: "h2" | "h3" = "h2"): HTMLElement {
  const node = document.createElement(tag);
  node.textContent = text;
  return node;
}

function paragraph(text: string, className = ""): HTMLParagraphElement {
  const node = document.createElement("p");
  if (className) node.className = className;
  node.textContent = text;
  return node;
}

function holderList(holders: PhotoCard["holders"]): HTMLElement {
  const list = document.createElement("ul");
  if (!holders.length) {
    list.append(item("还没有人登记。"));
    return list;
  }
  for (const holder of holders) {
    const row = document.createElement("li");
    row.append(personButton(holder), ` ${holder.saved ? "保存了" : "分享了"}`);
    if (holder.saved && holder.from && holder.from.id !== holder.id) {
      row.append("，保存自 ", personButton(holder.from));
    }
    list.append(row);
  }
  return list;
}

function saverList(savers: PhotoCard["savers"]): HTMLElement {
  const list = document.createElement("ul");
  if (!savers.length) {
    list.append(item("还没有人保存。"));
    return list;
  }
  for (const saver of savers) {
    const row = document.createElement("li");
    row.append(personButton(saver), saver.from && saver.from.id !== saver.id ? "，保存自 " : "，自己留下的");
    if (saver.from && saver.from.id !== saver.id) row.append(personButton(saver.from));
    list.append(row);
  }
  return list;
}

function peopleList(people: Activity["viewed"], extra: (person: Activity["viewed"][number]) => string): HTMLElement {
  const list = document.createElement("ul");
  if (!people.length) {
    list.append(item("还没有记录。"));
    return list;
  }
  for (const person of people) {
    const place = person.client === "ios" ? "iPhone" : person.client === "desktop" ? "电脑" : "浏览器";
    list.append(item(`${person.displayName || "未命名"} · ${place} · ${extra(person)}`));
  }
  return list;
}

function personButton(person: { id: string; displayName: string }): HTMLButtonElement {
  const node = document.createElement("button");
  node.className = "person-link";
  node.type = "button";
  node.textContent = person.displayName || "未命名";
  node.addEventListener("click", () => {
    state.person = {
      id: person.id,
      displayName: person.displayName,
      bio: "",
      city: "",
      contact: "",
      sharedCount: 0,
      savedCount: 0,
    };
    const known = state.people.find((item) => item.id === person.id);
    if (known) state.person = known;
    render();
  });
  return node;
}

function item(text: string): HTMLLIElement {
  const node = document.createElement("li");
  node.textContent = text;
  return node;
}

function renderStatus() {
  const note = document.querySelector(".side .note");
  if (note) note.textContent = state.status;
}

function escapeHTML(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}
