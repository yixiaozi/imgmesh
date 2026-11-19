const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("imgmesh", {
  apiBase: (process.env.IMGMESH_API || "https://imgmesh.com").replace(/\/$/, ""),
  os: process.platform,
  saveFiles: (files) => ipcRenderer.invoke("save-files", files),
  copy: (text) => ipcRenderer.invoke("copy", text),
});
