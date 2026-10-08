export interface SnapshotImage {
	name: string;
	image: string;
	git_sha: string;
	git_url: string;
	/** null until the image is found in ART build history. */
	art: ArtBuild | null;
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
}

export interface ReleaseSnapshotPage {
	snapshots: ReleaseSnapshot[];
	has_more: boolean;
}

export interface ReleaseComponent {
	name: string;
	image: string;
	git_sha: string;
	git_url: string;
	application: string;
	snapshot: string;
	created_at: string;
	art: ArtBuild | null;
}

export interface ReleaseComponents {
	release: string;
	as_of?: string;
	components: ReleaseComponent[];
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
	latest_build?: string;
}

export interface DashboardConfig {
	jira_base_url: string;
	jira_project: string;
}
