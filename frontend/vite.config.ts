import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [react(), tailwindcss(), wails("./bindings")],
  build: {
    chunkSizeWarningLimit: 500,
    rollupOptions: {
      output: {
        codeSplitting: true,
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (id.includes("react-syntax-highlighter")) {
            // Split the large highlighter into per-subpath chunks so no single
            // chunk exceeds the warning limit.
            const m = id.match(/react-syntax-highlighter\/dist\/esm\/([^/]+)/)
            return m ? `syntax-${m[1]}` : "vendor-syntax"
          }
          if (id.includes("framer-motion")) return "vendor-motion";
          if (id.includes("react-markdown") || id.includes("remark-gfm")) return "vendor-markdown";
          if (id.includes("@tanstack/react-query-devtools")) return "vendor-query-devtools";
          if (id.includes("@tanstack/react-query")) return "vendor-query";
          if (id.includes("@wailsio/runtime")) return "vendor-wails";
          if (id.includes("react-icons")) return "vendor-icons";
          if (id.includes("react-dom") || id.includes("/react/")) return "vendor-react";
        },
      },
    },
  },
});