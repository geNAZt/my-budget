import tailwindcss from "@tailwindcss/vite";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  build: {
    sourcemap: true,
    minify: false,
  },
  server: {
    watch: {
      usePolling: true,
    },
    host: true,
    allowedHosts: ["budget.genazt.me", "vm-host.lan", "localhost"],
  },
});
