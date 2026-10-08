/** Build a JIRA browse URL from an issue key. */
export function jiraIssueUrl(key: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/browse/${key}`;
}

/** `owner/repo` from a git clone URL, or null if it has no path. */
export function gitRepoName(gitUrl: string): string | null {
	const m = gitUrl.match(/^https?:\/\/[^/]+\/(.+?)(?:\.git)?\/*$/);
	return m ? m[1] : null;
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
