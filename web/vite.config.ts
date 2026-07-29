import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// バックエンドの既定リッスンアドレス。make dev は両者を同時に起動するため、
// dev サーバーからは同一オリジンに見えるようプロキシしておく。
const API_TARGET = "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: API_TARGET },
      "/healthz": { target: API_TARGET },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
