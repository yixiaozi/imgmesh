const { app, BrowserWindow, dialog, ipcMain, session, clipboard } = require("electron");
const fs = require("fs");
const path = require("path");

const distIndex = path.join(__dirname, "..", "dist", "index.html");

function allowAPI() {
  session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
    const headers = details.responseHeaders || {};
    headers["access-control-allow-origin"] = ["*"];
    headers["access-control-allow-headers"] = ["Authorization, Content-Type, X-Imgmesh-Access"];
    headers["access-control-allow-methods"] = ["GET, POST, PUT, DELETE, OPTIONS"];
    headers["access-control-expose-headers"] = ["ETag"];
    callback({ responseHeaders: headers });
  });
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1120,
    height: 760,
    minWidth: 760,
    minHeight: 560,
    title: "imgmesh",
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  if (process.env.IMGMESH_DEV) {
    win.loadURL(process.env.IMGMESH_DEV);
  } else {
    win.loadFile(distIndex);
  }
}

app.whenReady().then(() => {
  allowAPI();
  ipcMain.handle("save-files", async (_event, files) => {
    const picked = await dialog.showOpenDialog({
      title: "选择保存照片的文件夹",
      properties: ["openDirectory", "createDirectory"],
    });
    if (picked.canceled || !picked.filePaths[0]) return "";
    const dir = picked.filePaths[0];
    const used = new Set();
    for (const [index, file] of files.entries()) {
      const raw = path.basename(String(file.name || `photo-${index + 1}.jpg`)).replace(/[\\/]/g, "");
      let name = raw || `photo-${index + 1}.jpg`;
      if (used.has(name)) name = `${index + 1}-${name}`;
      used.add(name);
      fs.writeFileSync(path.join(dir, name), Buffer.from(file.data));
    }
    return dir;
  });
  ipcMain.handle("copy", async (_event, text) => {
    clipboard.writeText(String(text || ""));
  });
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
