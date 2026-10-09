/** Build a JIRA browse URL from an issue key. */
export function jiraIssueUrl(key: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/browse/${key}`;
}

/**
 * Derive a GitHub commit URL from a Konflux component name and SHA.
 *
 * Component names follow the pattern `{org}-{repo}-v{major}-{minor}`,
 * e.g. `quay-quay-v3-14` -> `https://github.com/quay/quay/commit/{sha}`.
 */
export function githubCommitUrl(component: string, sha: string): string | null {
	const stripped = component.replace(/-v\d+-\d+$/, "");
	const idx = stripped.indexOf("-");
	if (idx <= 0 || idx >= stripped.length - 1) return null;
	const org = stripped.substring(0, idx);
	const repo = stripped.substring(idx + 1);
	return `https://github.com/${org}/${repo}/commit/${sha}`;
}

/** Prepend https:// to a quay.io image reference if missing. */
export function quayImageUrl(imageUrl: string): string | null {
	if (!imageUrl.startsWith("quay.io/")) return null;
	return `https://${imageUrl.split("@")[0]}`;
}

/**
 * Format a release version name for display.
 *
 * `quay-v3.14.6` -> `Quay v3.14.6`
 * `omr-v2.0.10`  -> `OMR v2.0.10`
 * `3.14.6`       -> `Quay v3.14.6` (bare version)
 */
export function formatReleaseName(name: string): string {
	// Pattern: product-vX.Y.Z
	const match = name.match(/^([a-zA-Z]+)-v(.+)$/);
	if (match) {
		const product = match[1];
		const version = match[2];
		const label =
			product.toLowerCase() === "quay" ? "Quay" : product.toUpperCase();
		return `${label} v${version}`;
	}
	// Bare version number (e.g. "3.14.6")
	if (/^\d/.test(name)) {
		return `Quay v${name}`;
	}
	return name;
}

/** ART pipeline health view for the Quay tenant (needs an artc2023 browser login). */
export const ART_PIPELINES_HEALTH_URL =
	"https://art-pipelines-ui-art-pipelines-ui.apps.artc2023.pc3z.p1.openshiftapps.com/#/health?namespace=art-quay-tenant";
