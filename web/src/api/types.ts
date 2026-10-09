export interface SnapshotImage {
	name: string;
	image: string;
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
	nvr: string;
	build_url: string;
	upstream_repo: string;
	upstream_sha: string;
}

/** One ART image build; component is its NVR name. */
export interface BuildAttempt {
	component: string;
	/** "success", "pending", or a failure such as "build_error". */
	outcome: string;
	started_at: string;
	build_url: string;
}

/** A release's ART image builds started in the span ART history was read without a gap. */
export interface BuildAttempts {
	/** null when the stream's ART history was never read. */
	covered_from: string | null;
	attempts: BuildAttempt[];
}

export interface ReleaseSnapshot {
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
	catalog_bundle_image: string;
	snapshot_bundle_image: string;
}

export interface ReleaseSnapshotPage {
	snapshots: ReleaseSnapshot[];
	has_more: boolean;
}

export interface KonfluxRelease {
	name: string;
	release_plan: string;
	released_status: string;
	released_reason: string;
	/** Task and step of the last managed pipeline attempt of a failed Release. */
	failed_task?: string;
	failed_step?: string;
	created_at: string;
	start_time?: string;
	completion_time?: string;
}

/** A Snapshot annotated art.redhat.com/env=stage, with the newest Release naming it. */
export interface StagedSnapshot {
	name: string;
	created_at: string;
	release: KonfluxRelease | null;
}

export interface StagedSnapshots {
	assembly: string;
	staged_image: StagedSnapshot | null;
	staged_fbc: StagedSnapshot | null;
}

export interface JiraIssue {
	key: string;
	summary: string;
	status: string;
	priority: string;
	fix_version: string;
	assignee: string;
	issue_type: string;
	link: string;
	qa_contact: string;
}

export interface IssueSummary {
	total: number;
	verified: number;
	open: number;
	cves: number;
}

export interface ReleaseVersion {
	name: string;
	release_date?: string;
	released: boolean;
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
	// Newest snapshot in the version's Konflux applications, not bound to this version.
	latest_build?: string;
	shipped: boolean;
	next_in_stream: boolean;
}

export interface DashboardConfig {
	jira_base_url: string;
	jira_project: string;
	jira_enabled: boolean;
}

interface SyncProblem {
	source: string;
	message: string;
	since: string | null;
	last_success: string | null;
}

export interface SyncStatus {
	problems: SyncProblem[];
}

export interface ProwRun {
	job_name: string;
	build_id: string;
	state: string;
	started_at: string | null;
	completed_at: string | null;
	prow_url: string;
	/** missing until the job publishes tested-images.json. */
	artifact_state: "present" | "missing" | "invalid";
	catalog_ref: string;
	fetched_at: string;
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

export interface SelectedBuildComponent {
	name: string;
	image_digest: string;
	upstream_repo: string;
	upstream_sha: string;
	/** "resolved" when ART resolved the exact digest to a full upstream commit. */
	provenance_state: "resolved" | "unknown";
}

/** A staged image Snapshot whose Release through a STAGE ReleasePlan succeeded. */
export interface SelectedBuild {
	version: string;
	stage: string;
	/** The STAGE Release's completion time. */
	selected_at: string;
	release_name: string;
	release_plan: string;
	snapshot_name: string;
	snapshot_created_at: string;
	components: SelectedBuildComponent[];
	rr_url: string;
}

export type SelectedBuildResponse =
	| SelectedBuild
	| { selected_build: null; reason: string };

/** A commit naming a ticket in one component image's upstream range. */
export interface TicketEvidence {
	snapshot: string;
	component: string;
	image_digest: string;
	commit_sha: string;
	commit_url: string;
	pr_url: string;
}

export interface TicketRow {
	key: string;
	link: string;
	summary: string;
	status: string;
	/** The Target Version the ticket was found under; "" when only found in commits. */
	target_version: string;
	concrete_version: string;
	bucket: string;
	evidence_state: "complete" | "none" | "unknown";
	evidence_reason?: string;
	first_seen_snapshot?: string;
	current_snapshot?: string;
	evidence: TicketEvidence[];
	/** Display only: nothing is written to Jira. */
	jira_action: string;
	jira_action_reason?: string;
}

export interface TicketMembership {
	selected_build: SelectedBuild | null;
	reason?: string;
	buckets: {
		in_selected_build: TicketRow[];
		carried_forward: TicketRow[];
		planned_patch_pending: TicketRow[];
		needs_review: TicketRow[];
	};
}

export interface DeltaTicket extends TicketEvidence {
	key: string;
	first_seen_snapshot: string;
}

/** A component whose range in a Snapshot was not compared. */
export interface UnknownSpan {
	component: string;
	image_digest: string;
	reason: string;
}

/** Tickets a STAGE build Snapshot adds over the STAGE build before it. */
export interface TicketDelta {
	snapshot: string;
	baseline: string;
	delta_state: "complete" | "unknown";
	reason?: string;
	added: DeltaTicket[];
	carried: DeltaTicket[];
	unknown: UnknownSpan[];
}
