import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The build lands inside the Go module, where `make all` embeds it into the
// binary. It holds code only: the writing is fetched from `coroner serve` at
// runtime, so nothing private is ever bundled.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    host: "127.0.0.1",
    // `npm run dev` reads the corpus from a running `coroner serve`.
    // changeOrigin rewrites the Host header to the target, which is what
    // serve's loopback check expects.
    proxy: {
      "/api": { target: "http://127.0.0.1:8484", changeOrigin: true },
    },
  },
});
