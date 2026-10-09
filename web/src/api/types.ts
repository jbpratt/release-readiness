export interface SnapshotImage {
	name: string;
	image: string;
	git_sha: string;
	git_url: string;
	/** null until the image is found in ART build history. */
	art: ArtBuild | null;
	/** A newer ART build still running from another upstream commit. */
	pending_art_build: PendingArtBuild | null;
}

export interface PendingArtBuild {
	build_url: string;
	upstream_sha: string;
	started_at: string;
}

export interface ArtBuild {
	build_url: string;
	logs_url: string;
	pipeline_url: string;
	upstream_repo: string;
	upstream_sha: string;
}

export interface ReleaseSnapshot {
	application: string;
	name: string;
	created_at: string;
	component_count: number;
	/** A Konflux Release names this Snapshot, but it is no longer stored. */
	missing?: boolean;
	releases?: KonfluxRelease[];
	components?: SnapshotImage[];
	/** Set on a Quay snapshot detail carrying the quay-operator bundle. */
	fbc_catalog?: FBCCatalog;
}

/** Whether the newest quay-operator FBC catalog references the snapshot's bundle. */
export interface FBCCatalog {
	status: "current" | "behind" | "unknown";
	catalog_snapshot: string;
	catalog_image: string;
	catalog_bundle_image: string;
	snapshot_bundle_image: string;
}

export interface ReleaseSnapshotPage {
	snapshots: ReleaseSnapshot[];
	has_more: boolean;
}

export interface KonfluxRelease {
	id: number;
	name: string;
	application: string;
	snapshot: string;
	release_plan: string;
	target: string;
	released_status: string;
	released_reason: string;
	/** Task and step of the last managed pipeline attempt of a failed Release. */
	failed_task?: string;
	failed_step?: string;
	created_at: string;
	start_time?: string;
	completion_time?: string;
}

export interface JiraIssue {
	key: string;
	summary: string;
	status: string;
	priority: string;
	labels: string;
	fix_version: string;
	assignee: string;
	issue_type: string;
	resolution: string;
	link: string;
	qa_contact: string;
	updated_at: string;
}

export interface IssueSummary {
	total: number;
	verified: number;
	open: number;
	cves: number;
	bugs: number;
}

export interface ReleaseVersion {
	name: string;
	description: string;
	release_date?: string;
	released: boolean;
	archived: boolean;
	release_ticket_key?: string;
	release_ticket_assignee?: string;
	konflux_application?: string;
	due_date?: string;
}

export interface ReadinessResponse {
	signal: "green" | "yellow" | "red";
	message: string;
}

export interface ReleaseOverview {
	release: ReleaseVersion;
	issue_summary?: IssueSummary;
	readiness: ReadinessResponse;
	component_count: number;
	// Newest snapshot in the version's Konflux applications, not bound to this version.
	latest_build?: string;
	shipped: boolean;
	shipped_source: "catalog" | "jira" | "";
	next_in_stream: boolean;
}

export interface DashboardConfig {
	jira_base_url: string;
	jira_project: string;
	jira_enabled: boolean;
}

export interface SyncProblem {
	source: string;
	message: string;
	since: string | null;
	last_success: string | null;
	last_error_at: string | null;
}

export interface SyncStatus {
	problems: SyncProblem[];
}

export interface ProwImage {
	role: string;
	source: string;
	requested_ref: string;
	digest: string;
	image_id: string;
}

export interface ProwRun {
	job_name: string;
	build_id: string;
	kind: "periodic" | "rehearsal";
	application: string;
	state: string;
	started_at: string | null;
	completed_at: string | null;
	prow_url: string;
	/** missing until the job publishes tested-images.json. */
	artifact_state: "present" | "missing" | "invalid";
	catalog_ref: string;
	fetched_at: string;
	images: ProwImage[];
}

interface ProwSync {
	last_successful_sync: string | null;
	stale: boolean;
}

export interface ProwRunsResponse extends ProwSync {
	runs: ProwRun[];
}

/** Runs that tested each component's exact image, newest first. */
export interface SnapshotProwRuns extends ProwSync {
	components: Record<string, ProwRun[]>;
}
