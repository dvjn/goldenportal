import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite-plus";

// build identifies this build. index.html carries it, and the server sends
// it back on every web API response, so an open tab can tell it's stale.
const build = Date.now().toString(36);

export default defineConfig({
  plugins: [
    {
      name: "build-id",
      apply: "build",
      transformIndexHtml: () => [
        { tag: "meta", attrs: { name: "gp-build", content: build }, injectTo: "head" },
      ],
    },
    tanstackRouter({
      target: "react",
      autoCodeSplitting: !process.env.VITEST,
      routeFileIgnorePattern: "\\.test\\.",
      quoteStyle: "double",
      semicolons: true,
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    port: 47273,
    strictPort: true,
    proxy: {
      "/api": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
      "/scim": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
      "/favicon.ico": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
      // OAuth for MCP clients; /oauth/authorize is the consent page, so it stays here.
      "/.well-known": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
      "^/oauth/(token|register|revoke)$": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
      "/mcp": {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? "http://localhost:47200",
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["lcov", "text-summary"],
      reportsDirectory: "coverage",
      include: ["src/**"],
      exclude: ["src/routeTree.gen.ts", "src/test/**", "**/*.test.*"],
    },
  },
  fmt: {},
  lint: { options: { typeAware: true, typeCheck: true } },
});
