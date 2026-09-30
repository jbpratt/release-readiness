import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
	plugins: [react()],
	build: {
		outDir: "dist",
		emptyOutDir: true,
		rollupOptions: {
			output: {
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
				target: "http://localhost:8088",
				changeOrigin: true,
			},
		},
	},
});
