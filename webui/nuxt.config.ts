export default defineNuxtConfig({
  // omega webui: static SPA, served by the omega binary in production.
  // In dev, nitro devProxy forwards the API root paths to the locally
  // running dev omega on :8098 (the installed instance owns 8099).
  compatibilityDate: "2026-10-13",
  ssr: false,
  // full types for the editor (tsconfig.json extends .nuxt/tsconfig.json),
  // but no type-check in the build loop - it stays fast
  typescript: { strict: true, typeCheck: false },
  css: ["~/assets/css/main.css"],
  nitro: {
    devProxy: {
      "/health": { target: "http://127.0.0.1:8098/health" },
      "/models": { target: "http://127.0.0.1:8098/models" },
      "/chat": { target: "http://127.0.0.1:8098/chat" },
      "/sessions": { target: "http://127.0.0.1:8098/sessions" },
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
