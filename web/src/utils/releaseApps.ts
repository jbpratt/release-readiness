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
