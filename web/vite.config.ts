import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";

const rootFavicon = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../favicon.svg");

export default defineConfig({
  base: "/s/",
  plugins: [
    {
      name: "site-favicon",
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          if (req.url?.split("?")[0] !== "/favicon.svg") {
            next();
            return;
          }
          res.setHeader("Content-Type", "image/svg+xml");
          fs.createReadStream(rootFavicon).pipe(res);
        });
      },
    },
  ],
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8097",
    },
  },
});
