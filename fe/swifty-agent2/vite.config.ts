import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import { plugin as a2aPlugin } from "./middleware/a2a.js";

// https://vite.dev/config/
export default defineConfig({
  plugins: [tailwindcss(), a2aPlugin()],

  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8123",
        changeOrigin: true,
        // rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
