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

/** The ART pipelines UI's page of the pipeline that builds layered products. */
export function artPipelineUrl(ns: string): string {
	const q = encodeURIComponent(ns);
	return `https://art-pipelines-ui-art-pipelines-ui.apps.artc2023.pc3z.p1.openshiftapps.com/#/pipeline/${q}/build-layered-products?namespace=${q}`;
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
 * Sippy's job runs of the quay-X.Y release of a version since an ISO time;
 * null without an X.Y or a time.
 */
export function sippyRunsUrl(version: string, since: string): string | null {
	const xy = version.match(/(\d+)\.(\d+)/);
	if (!xy || !since) return null;
	const filters = JSON.stringify({
		items: [{ field: "timestamp", operator: ">", value: since }],
	});
	return `https://sippy.dptools.openshift.org/sippy-ng/jobs/quay-${xy[1]}.${xy[2]}/runs?filters=${encodeURIComponent(filters)}`;
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

// The GitHub repo ART builds each image label from (its build record's
// source_repo); a bundle's label is "<operator> bundle".
const SOURCE_REPOS: Record<string, string> = {
	"quay-rhel9": "quay/quay",
	"clair-rhel9": "quay/clair",
	"quay-rhel9-operator": "quay/quay-operator",
	"quay-builder-rhel9": "quay/quay-builder",
	"quay-builder-qemu-rhcos-rhel8": "quay/quay-builder-qemu",
	"quay-container-security-rhel9-operator": "quay/container-security-operator",
	"quay-bridge-rhel9-operator": "quay/quay-bridge-operator",
	"quay-operator bundle": "quay/quay-operator",
	"container-security-operator bundle": "quay/container-security-operator",
	"quay-bridge-operator bundle": "quay/quay-bridge-operator",
};

/** An image's commit in its GitHub repo; null for an unknown label or no sha. */
export function commitUrl(label: string, sha: string): string | null {
	const repo = SOURCE_REPOS[label];
	return repo && sha ? `https://github.com/${repo}/commit/${sha}` : null;
}
