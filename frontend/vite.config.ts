import fs from "node:fs";
import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Membaca APP_ADDR dari .env root biar proxy dev otomatis ngikut port Go lokal.
function resolveApiTarget() {
  if (process.env.VITE_API_TARGET) {
    return process.env.VITE_API_TARGET;
  }
  const envPath = path.resolve(import.meta.dirname, "../.env");
  if (fs.existsSync(envPath)) {
    const match = fs.readFileSync(envPath, "utf8").match(/^APP_ADDR\s*=\s*(.+)$/m);
    const addr = match?.[1]?.trim().replace(/^["']|["']$/g, "");
    if (addr) {
      if (addr.startsWith(":")) return `http://127.0.0.1${addr}`;
      if (addr.startsWith("http://") || addr.startsWith("https://")) return addr;
      return `http://${addr.replace(/^0\.0\.0\.0:/, "127.0.0.1:")}`;
    }
  }
  return "http://127.0.0.1:8080";
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: resolveApiTarget(),
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: "../web/dist",
    emptyOutDir: true,
  },
});
