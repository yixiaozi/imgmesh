import {
  Api,
  fetchCiphertext,
  addComment,
  createRecovery,
  deleteComment,
  loadAccount,
  loadDevices,
  loadFeed,
  loadUser,
  loadFriends,
  loadMessages,
  markFeed,
  photoCards,
  recover,
  register,
  requestFriend,
  respondFriend,
  resume,
  saveAccount,
  saveProfile,
  searchUsers,
  sendMessage,
  setLike,
  type Account,
  type Activity,
  type ActivityPerson,
  type DeviceInfo,
  type DirectMessage,
  type FeedItem,
  type PhotoCard,
  type PublicUser,
  type SocialUser,
} from "./api";
import { accessTag, aad, decode, decrypt, derive, encode, encrypt, randomBytes, sha256, type Manifest, type ManifestItem } from "./crypto";
import { heicJPEG } from "./heic";
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
  owner: boolean;
  autoSave: boolean;
  adding: boolean;
  socialOpen: boolean;
  friends: SocialUser[];
  incoming: SocialUser[];
  outgoing: SocialUser[];
  chatWith: string;
  messages: DirectMessage[];
  messageDraft: string;
  feedOpen: boolean;
  feed: FeedItem[];
  feedUnread: number;
  recoveryCode: string;
  devices: DeviceInfo[];
  commentDraft: string;
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
  owner: false,
  autoSave: false,
  adding: false,
  socialOpen: false,
  friends: [],
  incoming: [],
  outgoing: [],
  chatWith: "",
  messages: [],
  messageDraft: "",
  feedOpen: false,
  feed: [],
  feedUnread: 0,
  recoveryCode: "",
  devices: [],
  commentDraft: "",
};

setInterval(() => void refreshShare(), 15000);
setInterval(() => void refreshSocial(false), 8000);
setInterval(() => void refreshFeed(false), 15000);

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
      <button class="btn" id="open-feed" type="button">${escapeHTML(feedLabel())}</button>
      <button class="btn" id="open-social" type="button">${escapeHTML(friendLabel())}</button>
    </header>
    <main class="viewer">
      <h1>${escapeHTML(title)}</h1>
      <p class="viewer-note">${escapeHTML(state.status)}</p>
      <p class="viewer-note">${escapeHTML(state.registeredNote)}</p>
      <p class="account-error">${escapeHTML(state.accountError)}</p>
      <section class="people" id="people"></section>
      <section class="feed" id="feed"></section>
      <section class="social" id="social"></section>
      <div class="viewer-actions">
        <button class="btn btn-primary" id="save-all" type="button" ${state.photos.length && !state.adding ? "" : "disabled"}>全部下载</button>
        <label class="auto-save"><input id="auto-save" type="checkbox" ${state.autoSave ? "checked" : ""}/> 自动下载以后新增的照片</label>
        <label class="btn">添加照片<input id="add-photos" type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif" multiple hidden ${state.adding ? "disabled" : ""}/></label>
        <p class="viewer-note">拿着这条完整链接的人都可以加照片。不能撤销别人的链接，也不能改掉已经在里面的照片。</p>
      </div>
      <div class="grid" id="grid"></div>
      <section class="browser" id="browser"></section>
      <form class="profile" id="profile">
        <h2>我的资料</h2>
        <p class="viewer-note">这台设备：${escapeHTML(state.account.user.deviceId)}。${state.devices.length > 1 ? `一共 ${state.devices.length} 台设备在这个名称下。` : "可以生成恢复码，把另一台设备并进来。"}</p>
        ${state.recoveryCode ? `<p class="recovery-code">恢复码只显示这一次：<strong>${escapeHTML(state.recoveryCode)}</strong>。请记下来。服务器不保存明文。</p>` : `<button class="btn" id="make-recovery" type="button">生成恢复码</button>`}
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
    button.setAttribute("aria-label", photo.name || "照片");
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
  renderFeed();
  renderSocial();
  document.querySelector("#save-all")?.addEventListener("click", () => void saveAll());
  document.querySelector<HTMLInputElement>("#auto-save")?.addEventListener("change", (event) => {
    void setAuto((event.currentTarget as HTMLInputElement).checked);
  });
  document.querySelector<HTMLInputElement>("#add-photos")?.addEventListener("change", (event) => {
    const input = event.currentTarget as HTMLInputElement;
    const files = Array.from(input.files ?? []);
    input.value = "";
    void addFiles(files);
  });
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
  document.querySelector("#open-feed")?.addEventListener("click", () => {
    state.feedOpen = !state.feedOpen;
    render();
    if (state.feedOpen) void refreshFeed(true);
  });
  document.querySelector("#make-recovery")?.addEventListener("click", () => void makeRecovery());
  document.querySelector("#open-social")?.addEventListener("click", () => {
    state.socialOpen = !state.socialOpen;
    if (!state.socialOpen) state.chatWith = "";
    render();
    if (state.socialOpen) void refreshSocial(true);
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
  if (active.preview) {
    const img = document.createElement("img");
    img.alt = active.name || "照片";
    img.src = active.preview;
    stage.append(img);
  } else {
    const box = document.createElement("div");
    box.className = "stage-fallback";
    box.textContent =
      active.mime === "image/heic" || active.mime === "image/heif"
        ? `${active.name || "这张 HEIC"} 在浏览器里解不开，可以下载原文件。`
        : active.name || "可以下载原文件。";
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
  root.querySelector<HTMLFormElement>("#comment-form")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    const card = active?.sha256 ? state.cards[active.sha256] : undefined;
    void postComment(card?.id ?? "", String(data.get("body") ?? ""));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-delete-comment]").forEach((button) => {
    button.addEventListener("click", () => void removeComment(button.dataset.photo ?? "", button.dataset.deleteComment ?? ""));
  });
  const commentInput = root.querySelector<HTMLInputElement>('#comment-form input[name="body"]');
  commentInput?.addEventListener("input", () => {
    state.commentDraft = commentInput.value;
  });
  root.querySelector<HTMLButtonElement>("[data-like]")?.addEventListener("click", (event) => {
    const button = event.currentTarget as HTMLButtonElement;
    void toggleLike(button.dataset.like ?? "", button.dataset.liked !== "1");
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
    <h3>谁点了赞</h3>
    <p><button class="btn" type="button" data-like="${escapeHTML(card.id)}" data-liked="${card.liked ? "1" : "0"}">${card.liked ? "已赞" : "点赞"} · ${card.likeCount}</button></p>
    <ul>${
      card.likedBy.length
        ? card.likedBy.map((person) => `<li>${personButton(person.id, person.displayName)}</li>`).join("")
        : "<li>还没有人点赞。</li>"
    }</ul>
    <h3>留言</h3>
    <ul>${
      card.comments?.length
        ? card.comments
            .map(
              (comment) =>
                `<li>${escapeHTML(comment.displayName)}：${escapeHTML(comment.body)}${
                  comment.mine ? ` <button type="button" class="btn" data-delete-comment="${escapeHTML(comment.id)}" data-photo="${escapeHTML(card.id)}">删除</button>` : ""
                }</li>`,
            )
            .join("")
        : "<li>还没有留言。</li>"
    }</ul>
    <form class="composer" id="comment-form">
      <input name="body" maxlength="200" value="${escapeHTML(state.commentDraft)}" placeholder="写一句，1 到 200 字" />
      <button class="btn" type="submit">留言</button>
    </form>
  `;
}

function personButton(id: string, name: string): string {
  return `<button type="button" class="person-link" data-user="${escapeHTML(id)}">${escapeHTML(name || "未命名")}</button>`;
}

function feedLabel(): string {
  return state.feedUnread ? `最新动态 · ${state.feedUnread}` : "最新动态";
}

function renderFeed() {
  const root = document.querySelector<HTMLElement>("#feed");
  if (!root) return;
  if (!state.feedOpen) {
    root.hidden = true;
    root.innerHTML = "";
    return;
  }
  root.hidden = false;
  const rows = state.feed.length
    ? state.feed
        .map((item) => {
          const when = item.at ? new Date(item.at).toLocaleString() : "";
          return `<li class="${item.read ? "" : "unread"}"><button type="button" class="person-link" data-feed="${escapeHTML(item.id)}">${escapeHTML(item.name)} ${escapeHTML(item.text)}</button> <span class="viewer-note">${escapeHTML(when)}</span>${
            item.summary ? `<p>${escapeHTML(item.summary)}</p>` : ""
          }</li>`;
        })
        .join("")
    : "<li>还没有和你有关的新动态。</li>";
  root.innerHTML = `<h2>最新动态</h2><p class="viewer-note">只显示和你有关的事：谁打开或保存了你的分享、谁往你的链接加了照片、谁赞了你的照片、谁想加好友、好友发来的消息。点一条就标成已读。</p><ul>${rows}</ul>`;
  root.querySelectorAll<HTMLButtonElement>("[data-feed]").forEach((button) => {
    button.addEventListener("click", () => void openFeedItem(button.dataset.feed ?? ""));
  });
}

function friendLabel(): string {
  const unread = state.incoming.length + state.friends.reduce((sum, person) => sum + (person.unread ?? 0), 0);
  return unread ? `好友 · ${unread}` : "好友";
}

function friendActions(id: string): string {
  if (!id || state.account?.user.id === id) return "";
  if (state.friends.some((person) => person.id === id)) {
    return `<button type="button" class="btn btn-primary" data-chat="${escapeHTML(id)}">发消息</button>`;
  }
  if (state.incoming.some((person) => person.id === id)) {
    return `<button type="button" class="btn btn-primary" data-accept="${escapeHTML(id)}">接受</button> <button type="button" class="btn" data-decline="${escapeHTML(id)}">拒绝</button>`;
  }
  if (state.outgoing.some((person) => person.id === id)) {
    return `<button type="button" class="btn" data-cancel="${escapeHTML(id)}">取消申请</button>`;
  }
  return `<button type="button" class="btn" data-add="${escapeHTML(id)}">加好友</button>`;
}

function bindPeople(root: ParentNode) {
  root.querySelectorAll<HTMLButtonElement>("[data-user]").forEach((button) => {
    button.addEventListener("click", () => void openPerson(button.dataset.user ?? ""));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-add]").forEach((button) => {
    button.addEventListener("click", () => void addFriend(button.dataset.add ?? ""));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-accept]").forEach((button) => {
    button.addEventListener("click", () => void decideFriend(button.dataset.accept ?? "", "accept"));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-decline]").forEach((button) => {
    button.addEventListener("click", () => void decideFriend(button.dataset.decline ?? "", "decline"));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-cancel]").forEach((button) => {
    button.addEventListener("click", () => void decideFriend(button.dataset.cancel ?? "", "decline"));
  });
  root.querySelectorAll<HTMLButtonElement>("[data-chat]").forEach((button) => {
    button.addEventListener("click", () => void openChat(button.dataset.chat ?? ""));
  });
}

function renderSocial() {
  const root = document.querySelector<HTMLElement>("#social");
  if (!root) return;
  if (!state.socialOpen) {
    root.hidden = true;
    root.innerHTML = "";
    return;
  }
  root.hidden = false;
  if (state.chatWith) {
    const peer = [...state.friends, ...state.incoming, ...state.outgoing, state.person].find((person) => person?.id === state.chatWith);
    const bubbles = state.messages
      .map((msg) => {
        const mine = msg.senderId === state.account?.user.id;
        return `<p class="bubble ${mine ? "mine" : "theirs"}">${escapeHTML(msg.body)}</p>`;
      })
      .join("");
    root.innerHTML = `
      <div class="social-head"><button class="btn" id="back-friends" type="button">返回好友</button><h2>${escapeHTML(peer?.displayName || "好友")}</h2><button class="btn" id="unfriend" type="button">解除好友</button></div>
      <div class="thread">${bubbles || '<p class="viewer-note">还没有消息。</p>'}</div>
      <form class="composer" id="composer">
        <input name="body" maxlength="1000" value="${escapeHTML(state.messageDraft)}" placeholder="写一句" />
        <button class="btn btn-primary" type="submit">发送</button>
      </form>
      <p class="viewer-note">消息存在服务器上，只有你们两个好友能看。照片仍然是加密的。</p>
    `;
    const thread = root.querySelector<HTMLElement>(".thread");
    if (thread) thread.scrollTop = thread.scrollHeight;
    root.querySelector("#back-friends")?.addEventListener("click", () => {
      state.chatWith = "";
      state.messages = [];
      render();
    });
    root.querySelector("#unfriend")?.addEventListener("click", () => {
      const id = state.chatWith;
      state.chatWith = "";
      state.messages = [];
      void decideFriend(id, "remove");
    });
    const input = root.querySelector<HTMLInputElement>('input[name="body"]');
    input?.addEventListener("input", () => {
      state.messageDraft = input.value;
    });
    root.querySelector<HTMLFormElement>("#composer")?.addEventListener("submit", (event) => {
      event.preventDefault();
      const data = new FormData(event.currentTarget as HTMLFormElement);
      void postMessage(String(data.get("body") ?? ""));
    });
    return;
  }
  const row = (person: SocialUser, extra: string) =>
    `<li><button type="button" class="person-link" data-user="${escapeHTML(person.id)}">${escapeHTML(person.displayName)}</button>${person.unread ? ` · ${person.unread} 条新消息` : ""}${person.lastBody ? ` · ${escapeHTML(person.lastBody)}` : ""} ${extra}</li>`;
  root.innerHTML = `
    <h2>好友</h2>
    <h3>等你接受</h3>
    <ul>${state.incoming.length ? state.incoming.map((person) => row(person, friendActions(person.id))).join("") : "<li>没有新的申请。</li>"}</ul>
    <h3>已发出</h3>
    <ul>${state.outgoing.length ? state.outgoing.map((person) => row(person, friendActions(person.id))).join("") : "<li>没有待对方接受的申请。</li>"}</ul>
    <h3>可以发消息</h3>
    <ul>${state.friends.length ? state.friends.map((person) => row(person, friendActions(person.id))).join("") : "<li>还没有好友。在找人里可以加。</li>"}</ul>
  `;
  bindPeople(root);
}

async function refreshSocial(force: boolean) {
  if (!state.account) return;
  try {
    const lists = await loadFriends(state.account.token);
    state.friends = lists.friends;
    state.incoming = lists.incoming;
    state.outgoing = lists.outgoing;
    if (state.chatWith) state.messages = await loadMessages(state.account.token, state.chatWith);
  } catch (err) {
    if (force) state.accountError = accountMessage(err);
  }
  const typing = document.activeElement instanceof HTMLInputElement || document.activeElement instanceof HTMLTextAreaElement;
  if (state.socialOpen && (force || !typing)) render();
  else {
    const button = document.querySelector("#open-social");
    if (button) button.textContent = friendLabel();
  }
}

async function addFriend(id: string) {
  if (!state.account || !id) return;
  try {
    const status = await requestFriend(state.account.token, id);
    state.accountError = "";
    state.registeredNote = status === "accepted" ? "已经是好友，可以发消息了。" : "申请已发出。对方接受后就能发消息。";
    state.socialOpen = true;
    await refreshSocial(true);
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function decideFriend(id: string, action: "accept" | "decline" | "remove") {
  if (!state.account || !id) return;
  try {
    await respondFriend(state.account.token, id, action);
    state.accountError = "";
    await refreshSocial(true);
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function openChat(id: string) {
  state.chatWith = id;
  state.socialOpen = true;
  state.messageDraft = "";
  state.messages = [];
  await refreshSocial(true);
}

async function postMessage(body: string) {
  if (!state.account || !state.chatWith) return;
  const text = body.trim();
  if (!text) return;
  try {
    const message = await sendMessage(state.account.token, state.chatWith, text);
    state.messages = [...state.messages, message];
    state.messageDraft = "";
    state.accountError = "";
    render();
  } catch (err) {
    state.accountError = err instanceof Error && err.message === "not_friends" ? "先成为好友，才能发消息。" : accountMessage(err);
    render();
  }
}

async function toggleLike(id: string, on: boolean) {
  if (!state.account || !id) return;
  try {
    await setLike(state.account.token, id, on);
    await refreshCards();
    state.accountError = "";
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
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
        `<li><button type="button" class="person-link" data-user="${escapeHTML(person.id)}">${escapeHTML(person.displayName)}</button> · ${escapeHTML(person.city || "未填城市")} · 分享 ${person.sharedCount} · 保存 ${person.savedCount} ${friendActions(person.id)}</li>`,
    )
    .join("");
  const profile = state.person
    ? `<article class="person-card"><h2>${escapeHTML(state.person.displayName)}</h2><p>${escapeHTML(state.person.bio || "还没有介绍")}</p><p>${escapeHTML(state.person.city || "未填城市")} · ${escapeHTML(state.person.contact || "未填联系方式")}</p><p>分享了 ${state.person.sharedCount} 张，保存了 ${state.person.savedCount} 张。</p><p class="friend-actions">${friendActions(state.person.id)}</p></article>`
    : "";
  root.innerHTML = `${profile}<ul>${list}</ul>`;
  bindPeople(root);
}

function tileLabel(photo: Photo): string {
  const card = photo.sha256 ? state.cards[photo.sha256] : undefined;
  if (!card) return photo.sha256 ? "身份证" : "照片";
  const shared = card.sharedBy.length > 0;
  const count = card.holders.length;
  const likes = card.likeCount ? ` · ${card.likeCount} 赞` : "";
  if (shared && count) return `已分享 · ${count} 人有${likes}`;
  if (shared) return `已分享${likes}`;
  if (count) return `${count} 人有${likes}`;
  return likes ? `${card.likeCount} 赞` : "未分享";
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
      <form class="account" id="recover">
        <label>已有恢复码 <input name="code" maxlength="32" placeholder="在原来的设备上生成" ${broken ? "disabled" : ""} /></label>
        <button class="btn" type="submit" ${broken ? "disabled" : ""}>并入原来的名称</button>
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
  document.querySelector<HTMLFormElement>("#recover")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget as HTMLFormElement);
    void useRecovery(String(data.get("code") ?? ""));
  });
}

function renderActivity() {
  const root = document.querySelector<HTMLElement>("#activity");
  if (!root || !state.activity) return;
  const { viewed, downloaded, phones, followers } = state.activity;
  root.innerHTML = `
    <h2>这一组链接</h2>
    ${listBlock("谁查看过", viewed, (person) => `${person.count ?? 1} 次`)}
    ${listBlock("谁下载过", downloaded, (person) => `${person.count ?? 1} 次`)}
    ${listBlock("保存在这些设备里", phones, (person) => (person.client === "desktop" ? "已存进文件夹" : "已存进相册"))}
    ${listBlock("会自动下载新照片", followers ?? [], () => "以后新增的照片也会下载")}
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
  if (client === "desktop") return "电脑";
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
  void refreshSocial(false);
  void refreshFeed(false);
  void loadDevices(state.account.token).then((devices) => {
    state.devices = devices;
    const typing = document.activeElement instanceof HTMLInputElement || document.activeElement instanceof HTMLTextAreaElement;
    if (!typing) render();
  }).catch(() => undefined);
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
    void refreshSocial(false);
    void refreshFeed(false);
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
    state.owner = meta.owner?.id === state.account.user.id;
    state.autoSave = Boolean(meta.autoSave);
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
    await catchUp();
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
  if (code === "not_friends") return "先成为好友，才能发消息。";
  if (code === "bad_code") return "恢复码不对，没有并进去。";
  if (code === "bad_request") return "名称用 1 到 24 个字，介绍、城市和联系方式不要太长。留言是 1 到 200 个字。";
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

async function refreshFeed(force: boolean) {
  if (!state.account) return;
  try {
    const feed = await loadFeed(state.account.token);
    state.feed = feed.items;
    state.feedUnread = feed.unread;
  } catch (err) {
    if (force) state.accountError = accountMessage(err);
  }
  const typing = document.activeElement instanceof HTMLInputElement || document.activeElement instanceof HTMLTextAreaElement;
  if (state.feedOpen && (force || !typing)) render();
  else {
    const button = document.querySelector("#open-feed");
    if (button) button.textContent = feedLabel();
  }
}

async function openFeedItem(id: string) {
  if (!state.account || !id) return;
  await markFeed(state.account.token, id).catch(() => undefined);
  await refreshFeed(true);
}

async function makeRecovery() {
  if (!state.account) return;
  try {
    state.recoveryCode = await createRecovery(state.account.token);
    state.accountError = "";
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function useRecovery(code: string) {
  state.accountError = "";
  try {
    const account = await recover(code);
    state.account = account;
    state.registeredNote = `这台设备已经并入「${account.user.displayName}」。好友、消息和动态是同一份。`;
    state.devices = await loadDevices(account.token).catch(() => []);
    render();
    await openShare();
    void refreshSocial(false);
    void refreshFeed(false);
  } catch (err) {
    state.accountError = accountMessage(err);
    renderRegister();
  }
}

async function postComment(photoID: string, body: string) {
  if (!state.account || !photoID) return;
  try {
    await addComment(state.account.token, photoID, body);
    state.commentDraft = "";
    state.accountError = "";
    await refreshCards();
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function removeComment(photoID: string, commentID: string) {
  if (!state.account || !photoID || !commentID) return;
  try {
    await deleteComment(state.account.token, photoID, commentID);
    await refreshCards();
    render();
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
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
  if (!item) return;
  try {
    const bytes = await originalBytes(item);
    const digest = await sha256(bytes);
    item.sha256 = digest;
    let preview = bytes;
    let type = item.mime || "application/octet-stream";
    if (item.mime === "image/heic" || item.mime === "image/heif") {
      const jpeg = await heicJPEG(bytes);
      if (!jpeg) {
        state.status = `${item.name || "这张 HEIC"} 在浏览器里解不开，可以下载原文件。`;
        await refreshCards();
        render();
        return;
      }
      preview = jpeg;
      type = "image/jpeg";
    }
    item.preview = URL.createObjectURL(new Blob([preview as BlobPart], { type }));
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

function savedKey(): string {
  return `imgmesh-saved:${shareId}`;
}

function loadSaved(): string[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(savedKey()) || "[]") as string[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function storeSaved(ids: string[]) {
  localStorage.setItem(savedKey(), JSON.stringify([...new Set(ids)]));
}

async function setAuto(on: boolean) {
  if (!state.api) return;
  state.autoSave = on;
  state.accountError = "";
  try {
    await state.api.follow(on);
    if (on) await catchUp();
    else {
      state.status = "已关闭自动下载。这条链接里的照片仍可以手动下载。";
      render();
    }
  } catch (err) {
    state.accountError = accountMessage(err);
    render();
  }
}

async function catchUp() {
  if (!state.autoSave || !state.photos.length) return;
  const saved = new Set(loadSaved());
  const fresh = state.photos.filter((photo) => !saved.has(photo.id));
  if (!fresh.length) return;
  state.status = `正在自动下载 ${fresh.length} 张照片`;
  render();
  const names: string[] = [];
  const files: { name: string; data: Uint8Array }[] = [];
  for (const [index, item] of fresh.entries()) {
    const data = await originalBytes(item);
    item.sha256 = await sha256(data);
    const name = uniqueName(item.name, index, names);
    names.push(name);
    files.push({ name, data });
  }
  download(zipStore(files), fresh.length === state.photos.length ? "imgmesh.zip" : "imgmesh-new.zip");
  await rememberSaved(fresh);
  storeSaved([...saved, ...fresh.map((photo) => photo.id)]);
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = `已自动下载 ${fresh.length} 张照片。以后这条链接再增加照片，打开时也会自己下载。`;
  render();
}

async function refreshShare() {
  if (!state.api || !state.account || state.adding || !state.fileKey || !state.shareRaw) return;
  try {
    const meta = await state.api.meta();
    state.autoSave = Boolean(meta.autoSave);
    state.owner = meta.owner?.id === state.account.user.id;
    if (meta.photoCount === state.photos.length) return;
    const listed = await state.api.manifest();
    const manifestKey = await derive(decode(keyParam), state.shareRaw, "imgmesh-manifest-v1");
    const manifestBytes = await decrypt(
      manifestKey,
      decode(listed.nonce),
      aad(state.shareRaw, decode(listed.id)),
      await fetchCiphertext(listed.url),
    );
    const manifest = JSON.parse(new TextDecoder().decode(manifestBytes)) as Manifest;
    if (manifest.v !== 1 || !Array.isArray(manifest.items)) return;
    const previous = new Map(state.photos.map((photo) => [photo.id, photo.preview]));
    state.manifest = manifest;
    state.photos = manifest.items.map((item) => ({ ...item, preview: previous.get(item.id) }));
    state.status = `${manifest.items.length} 张照片。新加的还在原来的链接里。`;
    await refreshCards();
    render();
    await Promise.all(state.photos.map((item, index) => (item.preview ? Promise.resolve() : loadPreview(index, item))));
    render();
    await catchUp();
  } catch {
    /* 刷新失败时保留已经打开的照片 */
  }
}

async function addFiles(files: File[]) {
  if (!files.length || !state.api || !state.fileKey || !state.shareRaw || !state.manifest || state.adding) return;
  if (state.photos.length + files.length > 200) {
    state.status = "一条链接最多 200 张。";
    render();
    return;
  }
  state.adding = true;
  state.status = "正在把照片加进原来的链接";
  render();
  try {
    const items: ManifestItem[] = state.photos.map((photo) => ({
      id: photo.id,
      name: photo.name,
      mime: photo.mime,
      bytes: photo.bytes,
      nonce: photo.nonce,
      sha256: photo.sha256,
      thumb: photo.thumb,
    }));
    const blobs = new Map<string, Uint8Array>();
    const objects: { id: string; kind: string; plaintextBytes: number; ciphertextBytes: number }[] = [];
    const claims: { sha256: string; kind: "shared" }[] = [];
    for (const file of files) {
      const plain = new Uint8Array(await file.arrayBuffer());
      if (plain.byteLength > 50 * 1024 * 1024) throw new Error("too_large");
      const mime = allowedMime(file.type, file.name);
      const objectID = randomBytes(16);
      const nonce = randomBytes(12);
      const objectText = encode(objectID);
      const cipher = await encrypt(state.fileKey, nonce, aad(state.shareRaw, objectID), plain);
      blobs.set(objectText, cipher);
      objects.push({ id: objectText, kind: "original", plaintextBytes: plain.byteLength, ciphertextBytes: cipher.byteLength });
      const digest = await sha256(plain);
      claims.push({ sha256: digest, kind: "shared" });
      let thumb: ManifestItem["thumb"];
      const jpeg = await thumbJPEG(plain, mime);
      if (jpeg) {
        const thumbID = randomBytes(16);
        const thumbNonce = randomBytes(12);
        const thumbText = encode(thumbID);
        const thumbCipher = await encrypt(state.fileKey, thumbNonce, aad(state.shareRaw, thumbID), jpeg);
        blobs.set(thumbText, thumbCipher);
        objects.push({ id: thumbText, kind: "thumb", plaintextBytes: jpeg.byteLength, ciphertextBytes: thumbCipher.byteLength });
        thumb = { id: thumbText, bytes: jpeg.byteLength, nonce: encode(thumbNonce) };
      }
      items.push({ id: objectText, name: file.name, mime, bytes: plain.byteLength, nonce: encode(nonce), sha256: digest, thumb });
    }
    const manifestPlain = new TextEncoder().encode(JSON.stringify({ v: 1, title: state.manifest.title, items }));
    const manifestID = randomBytes(16);
    const manifestNonce = randomBytes(12);
    const manifestText = encode(manifestID);
    const manifestKey = await derive(decode(keyParam), state.shareRaw, "imgmesh-manifest-v1");
    const manifestCipher = await encrypt(manifestKey, manifestNonce, aad(state.shareRaw, manifestID), manifestPlain);
    blobs.set(manifestText, manifestCipher);
    objects.push({ id: manifestText, kind: "manifest", plaintextBytes: manifestPlain.byteLength, ciphertextBytes: manifestCipher.byteLength });
    const plans = await state.api.reserve(objects);
    const completed: { id: string; byteSize: number; parts: { partNumber: number; etag: string }[] }[] = [];
    for (const plan of plans) {
      const payload = blobs.get(plan.id);
      if (!payload) throw new Error("upload_failed");
      const parts: { partNumber: number; etag: string }[] = [];
      if (!plan.ready) {
        let offset = 0;
        const chunk = plan.partSize ?? payload.byteLength;
        for (const part of plan.parts) {
          const end = part.partNumber === 0 ? payload.byteLength : Math.min(offset + Math.max(chunk, 1), payload.byteLength);
          const etag = await putBlob(part.url, payload.slice(offset, end));
          parts.push({ partNumber: part.partNumber, etag });
          if (part.partNumber > 0) offset = end;
        }
      }
      completed.push({ id: plan.id, byteSize: payload.byteLength, parts });
    }
    await state.api.complete(completed, manifestText, encode(manifestNonce));
    await state.api.claim(claims).catch(() => undefined);
    state.adding = false;
    state.status = "已经加进原来的链接。选过自动下载的人再打开时会收到新照片，后来打开的人也可以直接下载。";
    render();
    await refreshShare();
    if (state.photos.length < items.length) {
      state.manifest = { v: 1, title: state.manifest.title, items };
      state.photos = items.map((item) => ({ ...item, preview: state.photos.find((photo) => photo.id === item.id)?.preview }));
      await Promise.all(state.photos.map((item, index) => (item.preview ? Promise.resolve() : loadPreview(index, item))));
      render();
    }
  } catch (err) {
    state.adding = false;
    state.status = err instanceof Error && err.message === "too_large" ? "单张不能超过 50MB。" : "这几张没有加进去，可以再试一次。";
    render();
  }
}

function allowedMime(type: string, name: string): string {
  const lower = type.toLowerCase();
  if (["image/jpeg", "image/png", "image/webp", "image/heic", "image/heif"].includes(lower)) return lower;
  const ext = name.toLowerCase().split(".").pop();
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  if (ext === "png") return "image/png";
  if (ext === "webp") return "image/webp";
  if (ext === "heic") return "image/heic";
  if (ext === "heif") return "image/heif";
  return "image/jpeg";
}

async function thumbJPEG(plain: Uint8Array, mime: string): Promise<Uint8Array | null> {
  let source = plain;
  let sourceType = mime;
  if (mime === "image/heic" || mime === "image/heif") {
    const jpeg = await heicJPEG(plain);
    if (!jpeg) return null;
    source = jpeg;
    sourceType = "image/jpeg";
  }
  try {
    const bitmap = await createImageBitmap(new Blob([source as BlobPart], { type: sourceType }));
    const scale = 480 / Math.max(bitmap.width, bitmap.height);
    const width = Math.max(1, Math.round(bitmap.width * Math.min(1, scale)));
    const height = Math.max(1, Math.round(bitmap.height * Math.min(1, scale)));
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0, width, height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob((value) => resolve(value), "image/jpeg", 0.7));
    bitmap.close();
    if (!blob) return null;
    return new Uint8Array(await blob.arrayBuffer());
  } catch {
    return null;
  }
}

async function putBlob(url: string, data: Uint8Array): Promise<string> {
  const res = await fetch(url, {
    method: "PUT",
    headers: { "Content-Type": "application/octet-stream" },
    body: new Blob([data as BlobPart]),
  });
  if (!res.ok) throw new Error("upload_failed");
  return res.headers.get("ETag") ?? "";
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
  storeSaved(state.photos.map((photo) => photo.id));
  state.autoSave = true;
  await state.api?.follow(true).catch(() => undefined);
  await state.api?.event("download").catch(() => undefined);
  state.activity = (await state.api?.activity()) ?? state.activity;
  state.status = "已开始下载。以后这条链接再增加照片，打开时会自动下载。";
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
