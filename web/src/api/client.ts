import type {
	DashboardConfig,
	FBCBuild,
	FBCTags,
	JiraIssue,
	ReleaseCandidate,
	ReleaseDetail,
	ReleaseOverview,
	SyncStatus,
} from "./types";

const BASE = "/api/v1";

declare global {
	interface Window {
		__RR_FIXTURES__?: Record<string, unknown>;
	}
}

async function fetchJSON<T>(url: string): Promise<T> {
	// A page build answers from fixtures dev/page.py injects: the page runs
	// with no origin and no network.
	if (import.meta.env.MODE === "page") {
		const fixtures = window.__RR_FIXTURES__ ?? {};
		if (!(url in fixtures)) throw new Error("404 Not Found");
		return fixtures[url] as T;
	}
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

export function getRelease(version: string): Promise<ReleaseDetail> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}`);
}

export function listReleaseIssues(version: string): Promise<JiraIssue[]> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/issues`);
}

export function getCandidate(version: string): Promise<ReleaseCandidate> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/candidate`);
}

export function getFBCTags(version: string): Promise<FBCTags> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/fbc-tags`);
}

export function getReleaseCI(version: string): Promise<FBCBuild[]> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/ci`);
}
