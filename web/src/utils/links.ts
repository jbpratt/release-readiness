/** Build a JIRA browse URL from an issue key. */
export function jiraIssueUrl(key: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/browse/${key}`;
}

/**
 * GitHub commit page of an upstream build source, or null unless the repo is
 * on github.com and the sha is known.
 */
export function upstreamCommitUrl(repo: string, sha: string): string | null {
	const m = repo.match(/^https:\/\/github\.com\/([^/]+\/[^/]+?)(?:\.git)?\/*$/);
	return m && sha ? `https://github.com/${m[1]}/commit/${sha}` : null;
}

/**
 * quay.io UI manifest page for a digest-pinned image reference,
 * e.g. `quay.io/ns/repo@sha256:abc` -> `https://quay.io/repository/ns/repo/manifest/sha256:abc`.
 */
export function quayManifestUrl(image: string): string | null {
	const m = image.match(/^quay\.io\/([^@:]+)(?::[^@]*)?@(sha256:[0-9a-f]+)$/);
	return m ? `https://quay.io/repository/${m[1]}/manifest/${m[2]}` : null;
}

/**
 * Format a release version name for display.
 *
 * `quay-v3.14.6` -> `Quay v3.14.6`
 * `omr-v2.0.10`  -> `OMR v2.0.10`
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
	return name;
}

/** The X.Y of a release version name: `quay-v3.18.1` -> `3.18`. */
export function minorVersion(name: string): string {
	return name
		.replace(/^[a-z]+-v/, "")
		.split(".")
		.slice(0, 2)
		.join(".");
}

/** ART pipeline health view for the Quay tenant (needs an artc2023 browser login). */
export const ART_PIPELINES_HEALTH_URL =
	"https://art-pipelines-ui-art-pipelines-ui.apps.artc2023.pc3z.p1.openshiftapps.com/#/health?namespace=art-quay-tenant";
