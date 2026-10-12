/** Build a JIRA browse URL from an issue key. */
export function jiraIssueUrl(key: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/browse/${key}`;
}

/** Build a JIRA issue search URL from a JQL query. */
export function jiraSearchUrl(jql: string, baseUrl: string): string {
	return `${baseUrl.replace(/\/+$/, "")}/issues/?jql=${encodeURIComponent(jql)}`;
}

/** JQL for a version's ticket list: the tickets of its Target Version. */
export function ticketsJql(project: string, version: string): string {
	return `project=${project} AND "Target Version"="${version}"`;
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
 * Sippy's page of a periodic job in the quay-X.Y release of a version
 * (3.18.2 or quay-v3.18.2); null without an X.Y.
 */
export function sippyJobUrl(version: string, job: string): string | null {
	const xy = version.match(/(\d+)\.(\d+)/);
	if (!xy) return null;
	const filters = JSON.stringify({
		items: [{ columnField: "name", operatorValue: "equals", value: job }],
	});
	return `https://sippy.dptools.openshift.org/sippy-ng/jobs/quay-${xy[1]}.${xy[2]}?filters=${encodeURIComponent(filters)}`;
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
