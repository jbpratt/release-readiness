/** Build a JIRA browse URL from an issue key. */
export function jiraIssueUrl(key: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/browse/${key}`;
}

/** Build a JIRA issue search URL from a JQL query. */
export function jiraSearchUrl(jql: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/issues/?jql=${encodeURIComponent(jql)}`;
}

/**
 * JQL for exactly a version's ticket list: its Target Version, and by key
 * the tickets listed under another fix version.
 */
export function ticketsJql(
	project: string,
	version: string,
	tickets: { key: string; fix_version: string }[],
): string {
	const target = `"Target Version"="${version}"`;
	const keys = tickets
		.filter((t) => t.fix_version !== version)
		.map((t) => t.key)
		.sort((a, b) => a.localeCompare(b, undefined, { numeric: true }));
	return keys.length === 0
		? `project=${project} AND ${target}`
		: `project=${project} AND (${target} OR key in (${keys.join(", ")}))`;
}

/**
 * Konflux UI page of an application, or of one of its Snapshots or Releases;
 * null without a base URL, namespace or application.
 */
export function konfluxUrl(
	base: string,
	ns: string,
	app: string,
	kind?: "snapshots" | "releases",
	name?: string,
): string | null {
	if (!base || !ns || !app) return null;
	const url = `${base.replace(/\/+$/, "")}/ns/${ns}/applications/${app}`;
	return kind && name ? `${url}/${kind}/${name}` : url;
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

/** ART pipeline health view for the Quay tenant (needs an artc2023 browser login). */
export const ART_PIPELINES_HEALTH_URL =
	"https://art-pipelines-ui-art-pipelines-ui.apps.artc2023.pc3z.p1.openshiftapps.com/#/health?namespace=art-quay-tenant";
