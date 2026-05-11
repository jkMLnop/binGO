import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const backend = process.env.BINGO_SERVER_URL || "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: backend,
        changeOrigin: true,
      },
      "/admin": {
        target: backend,
        changeOrigin: true,
      },
      "/ws": {
        target: backend.replace(/^http/, "ws"),
        ws: true,
        changeOrigin: true,
      },
    },
  },
});
