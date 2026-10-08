import type {
	DashboardConfig,
	IssueSummary,
	JiraIssue,
	ReadinessResponse,
	ReleaseOverview,
	ReleaseVersion,
	SnapshotRecord,
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

export function listSnapshots(
	application?: string,
	limit = 50,
	offset = 0,
): Promise<SnapshotRecord[]> {
	const params = new URLSearchParams();
	if (application) params.set("application", application);
	params.set("limit", String(limit));
	params.set("offset", String(offset));
	return fetchJSON(`${BASE}/snapshots?${params}`);
}

// --- Release-centric API ---

export function listReleasesOverview(): Promise<ReleaseOverview[]> {
	return fetchJSON(`${BASE}/releases/overview`);
}

export function getRelease(version: string): Promise<ReleaseVersion> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}`);
}

export function getReleaseSnapshot(version: string): Promise<SnapshotRecord> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/snapshot`);
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
