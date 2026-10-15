export default defineNuxtConfig({
  // omega webui: static SPA, served by the omega binary in production.
  // In dev, nitro devProxy forwards the API root paths to a locally
  // running omega instance (omega serve on :8099).
  compatibilityDate: "2026-10-13",
  ssr: false,
  // full types for the editor (tsconfig.json extends .nuxt/tsconfig.json),
  // but no type-check in the build loop - it stays fast
  typescript: { strict: true, typeCheck: false },
  css: ["~/assets/css/main.css"],
  nitro: {
    devProxy: {
      "/health": { target: "http://127.0.0.1:8099/health" },
      "/models": { target: "http://127.0.0.1:8099/models" },
      "/chat": { target: "http://127.0.0.1:8099/chat" },
      "/sessions": { target: "http://127.0.0.1:8099/sessions" },
    },
    output: {
      publicDir: "dist",
    },
  },
  app: {
    head: {
      title: "omega",
      meta: [
        { charset: "utf-8" },
        {
          name: "viewport",
          content:
            "width=device-width, initial-scale=1, interactive-widget=resizes-content",
        },
      ],
    },
  },
});
