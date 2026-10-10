import type {
	BuildAttempts,
	BuildTickets,
	DashboardConfig,
	ProwRunsResponse,
	ReadinessResponse,
	ReleaseOverview,
	ReleaseSnapshot,
	ReleaseSnapshotPage,
	ReleaseVersion,
	SnapshotProwRuns,
	StagedSnapshots,
	SyncStatus,
} from "./types";

const BASE = "/api/v1";

async function fetchJSON<T>(url: string): Promise<T> {
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

export function getBuildTickets(version: string): Promise<BuildTickets> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/build-tickets`,
	);
}

export function getReleaseReadiness(
	version: string,
): Promise<ReadinessResponse> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/readiness`);
}

export function getBuildAttempts(version: string): Promise<BuildAttempts> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/build-attempts`,
	);
}

export function getStaged(version: string): Promise<StagedSnapshots> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/staged`);
}
