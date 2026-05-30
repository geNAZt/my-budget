import adapter from "@sveltejs/adapter-node";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

/** @type {import('@sveltejs/kit').Config} */
const config = {
  compilerOptions: {
    // Force runes mode for the project, except for libraries. Can be removed in svelte 6.
    runes: ({ filename }) =>
      filename.split(/[/\\]/).includes("node_modules") ? undefined : true,
  },
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({
      // Output directory for the built server
      out: "build",
      // Listen on all interfaces inside Docker
      host: "0.0.0.0",
      port: 3000,
    }),
  },
};

export default config;
