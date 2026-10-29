import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tailwindcss from "@tailwindcss/vite";
import tsconfigPaths from "vite-tsconfig-paths";
import basicSsl from "@vitejs/plugin-basic-ssl";
import svgr from 'vite-plugin-svgr';
declare const process: {
  env: {
    KVM_PROXY_URL: string;
    USE_SSL: string;
  };
};

export default defineConfig(({ mode, command }) => {
  const isCloud = mode.indexOf("cloud") !== -1;
  const onDevice = mode === "device";
  const { KVM_PROXY_URL, USE_SSL } = process.env;
  const useSSL = USE_SSL === "true";

  const plugins = [
    tailwindcss(),
    tsconfigPaths(),
    react(),
    svgr({
      svgrOptions: {
        icon: true,
      },
    }),
  ];
  if (useSSL) {
    plugins.push(basicSsl());
  }

  return {
    plugins,
    build: {
      outDir: isCloud ? "dist" : "../static",
      chunkSizeWarningLimit: 4096,
      rollupOptions: {
        onwarn(warning, warn) {
          const msg = typeof warning === "string" ? warning : warning.message;
          if (
            warning &&
            typeof warning === "object" &&
            warning.code === "PLUGIN_WARNING" &&
            // Vite's resolve plugin may log this when a dependency references Node built-ins (e.g. "stream")
            msg.includes('has been externalized for browser compatibility') &&
            msg.includes('Module "stream"')
          ) {
            return;
          }
          warn(warning);
        },
      },
    },
    server: {
      host: "0.0.0.0",
      https: useSSL,
      proxy: KVM_PROXY_URL
        ? {
            "/me": KVM_PROXY_URL,
            "/device": KVM_PROXY_URL,
            "/webrtc": KVM_PROXY_URL,
            "/auth": KVM_PROXY_URL,
            "/storage": KVM_PROXY_URL,
            "/cloud": KVM_PROXY_URL,
            "/developer": KVM_PROXY_URL,
            // REST channel: forceHttp JSON-RPC (/api/rpc) and the MCP
            // management endpoints (/api/mcp/*). Without this the SPA
            // fallback answers with index.html instead of a 404.
            "/api": KVM_PROXY_URL,
            // MJPEG fallback stream; long-lived response, never buffered.
            "/video": KVM_PROXY_URL,
          }
        : undefined,
    },
    base: onDevice && command === "build" ? "/static" : "/",
  };
});
