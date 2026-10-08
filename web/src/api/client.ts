import type {
	DashboardConfig,
	IssueSummary,
	JiraIssue,
	KonfluxRelease,
	ProwRunsResponse,
	ReadinessResponse,
	ReleaseOverview,
	ReleaseSnapshot,
	ReleaseSnapshotPage,
	ReleaseVersion,
	SnapshotProwRuns,
	SyncStatus,
} from "./types";

const BASE = "/api/v1";

export async function fetchJSON<T>(url: string): Promise<T> {
	const res = await fetch(url);
	if (!res.ok) {
		throw new Error(`${res.status} ${res.statusText}`);
	}
	return res.json() as Promise<T>;
}

export function getConfig(): Promise<DashboardConfig> {
	return fetchJSON(`${BASE}/config`);
}

export function getSyncStatus(): Promise<SyncStatus> {
	return fetchJSON(`${BASE}/sync-status`);
}

export function listKonfluxReleases(
	application?: string,
	limit = 50,
	offset = 0,
): Promise<KonfluxRelease[]> {
	const params = new URLSearchParams();
	if (application) params.set("application", application);
	params.set("limit", String(limit));
	params.set("offset", String(offset));
	return fetchJSON(`${BASE}/konflux-releases?${params}`);
}

// --- Release-centric API ---

export function listReleasesOverview(): Promise<ReleaseOverview[]> {
	return fetchJSON(`${BASE}/releases/overview`);
}

export function getRelease(version: string): Promise<ReleaseVersion> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}`);
}

export function listReleaseSnapshots(
	version: string,
	opts: {
		application?: string;
		withRelease?: boolean;
		limit: number;
		offset: number;
	},
): Promise<ReleaseSnapshotPage> {
	const params = new URLSearchParams();
	if (opts.application) params.set("application", opts.application);
	if (opts.withRelease) params.set("with_release", "true");
	params.set("limit", String(opts.limit));
	params.set("offset", String(opts.offset));
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/snapshots?${params}`,
	);
}

export function getSnapshotProwRuns(
	version: string,
	name: string,
): Promise<SnapshotProwRuns> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/snapshots/${encodeURIComponent(name)}/prow-runs`,
	);
}

/** The release application's runs that matched no Snapshot image. */
export function listUnlinkedProwRuns(
	version: string,
): Promise<ProwRunsResponse> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/prow-runs?unlinked=true&limit=200`,
	);
}

export function getReleaseSnapshot(
	version: string,
	name: string,
): Promise<ReleaseSnapshot> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/snapshots/${encodeURIComponent(name)}`,
	);
}

export function listReleaseIssues(
	version: string,
	filters?: { label?: string; status?: string; type?: string },
): Promise<JiraIssue[]> {
	const params = new URLSearchParams();
	if (filters?.label) params.set("label", filters.label);
	if (filters?.status) params.set("status", filters.status);
	if (filters?.type) params.set("type", filters.type);
	const qs = params.toString();
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/issues${qs ? `?${qs}` : ""}`,
	);
}

export function getReleaseIssueSummary(version: string): Promise<IssueSummary> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/issues/summary`,
	);
}

export function getReleaseReadiness(
	version: string,
): Promise<ReadinessResponse> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/readiness`);
}
