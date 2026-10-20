import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Page mode (vite build --mode page) builds one JS chunk, one CSS file and
// inline assets, for dev/page.py to inline into one self-contained HTML file.
export default defineConfig(({ mode }) => ({
	plugins: [react()],
	build: {
		outDir: "dist",
		emptyOutDir: true,
		...(mode === "page" && {
			cssCodeSplit: false,
			assetsInlineLimit: Number.MAX_SAFE_INTEGER,
			chunkSizeWarningLimit: Number.MAX_SAFE_INTEGER,
		}),
		rollupOptions: {
			output:
				mode === "page"
					? { codeSplitting: false }
					: {
							manualChunks(id) {
								if (
									id.includes("node_modules/react/") ||
									id.includes("node_modules/react-dom/") ||
									id.includes("node_modules/react-router-dom/") ||
									id.includes("node_modules/react-router/")
								) {
									return "react";
								}
								if (
									id.includes("node_modules/@patternfly/react-core/") ||
									id.includes("node_modules/@patternfly/react-table/") ||
									id.includes("node_modules/@patternfly/react-icons/")
								) {
									return "patternfly";
								}
							},
						},
		},
	},
	server: {
		allowedHosts: ["workstation"],
		proxy: {
			"/api": {
				target: process.env.RR_API_TARGET ?? "http://localhost:8088",
				changeOrigin: true,
			},
		},
	},
}));
