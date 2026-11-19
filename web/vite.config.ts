import { defineConfig } from "vite";

export default defineConfig({
  base: "/s/",
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8097",
    },
  },
});
