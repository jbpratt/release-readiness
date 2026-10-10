import type {
	BuildTickets,
	DashboardConfig,
	ReleaseCandidate,
	ReleaseOverview,
	ReleaseVersion,
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

export function getBuildTickets(version: string): Promise<BuildTickets> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/build-tickets`,
	);
}

export function getCandidate(version: string): Promise<ReleaseCandidate> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/candidate`);
}
