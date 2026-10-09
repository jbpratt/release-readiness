import type { ReleaseSnapshot } from "../api/types";

export interface SnapshotFilter {
	key: string;
	label: string;
	/** Konflux application to filter by; empty for all of the release's. */
	application: string;
}

/**
 * Application filters for a release's Snapshots, mirroring
 * releaseview.Applications: a quay-X-Y release spans its own application,
 * its FBC application and the shared base images.
 */
export function snapshotFilters(konfluxApp?: string): SnapshotFilter[] {
	const all = { key: "all", label: "All", application: "" };
	if (!konfluxApp || !/^quay-\d+-\d+$/.test(konfluxApp)) return [all];
	return [
		all,
		{ key: "quay", label: "Quay images", application: konfluxApp },
		{ key: "fbc", label: "FBC", application: `fbc-${konfluxApp}` },
		{ key: "base", label: "Base", application: "quay-images-base" },
	];
}

/**
 * The ART image build named by the newest succeeded Konflux Release among
 * snapshots. ART also releases an operator bundle's related images
 * (fbc-ri-stage-*) through the builds' stage plan; those are not a build.
 */
export function latestReleased(snapshots: ReleaseSnapshot[]) {
	let newest: { snapshot: ReleaseSnapshot; at: string } | undefined;
	for (const snapshot of snapshots) {
		if (snapshot.art_kind !== "image") continue;
		for (const r of snapshot.releases ?? []) {
			if (r.released_status === "True" && (!newest || r.created_at > newest.at))
				newest = { snapshot, at: r.created_at };
		}
	}
	return newest?.snapshot;
}
