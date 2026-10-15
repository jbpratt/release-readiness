export interface KonfluxRelease {
	name: string;
	released_status: string;
	released_reason: string;
	/** Task and step of the last managed pipeline attempt of a failed Release. */
	failed_task?: string;
	failed_step?: string;
	completion_time?: string;
	/** The ART job that made the Release, and its errata URL. */
	job_url?: string;
	advisory?: string;
}

/**
 * What decides whether a version can ship: its operator FBCs. candidate is
 * the version's STAGE build, null with reason set when there is none; a
 * shipped version has no reason.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	/**
	 * The version's operator FBCs up for release: as ART staged them for
	 * stage, else as its stream built them. Null while the version has no
	 * FBC built or staged.
	 */
	fbc_candidate: FBCCandidate | null;
	/** The version's operator FBCs as its stream built them. */
	fbc_stream: FBCCandidate | null;
	verdict: Verdict;
	reading: ReadProgress;
}

/**
 * Whether a version can ship, by its FBC candidate: "checking" while FBC
 * reads are pending, "shipped", else "not_ready" with a blocker or "ready".
 * items are its blockers, then warnings, then info.
 */
export interface Verdict {
	state: "checking" | "shipped" | "not_ready" | "ready";
	items: VerdictItem[];
}

/**
 * A blocker, warning or info on operator, "" for the whole release, for
 * owner, "ART", "ART/Konflux", "Quay (release-from-fbc)", "Quay/ART" or "", to
 * act on.
 */
export interface VerdictItem {
	level: "blocker" | "warning" | "info";
	operator: string;
	owner: string;
	code: string;
	text: string;
}

/** The FBC images the render sync reads, by the state of their read. */
export interface ReadProgress {
	read: number;
	failed: number;
	pending: number;
	/** Days of ART build history the backfill has yet to read. */
	history_days: number;
}

/**
 * A version's operator FBCs, one per operator and OCP version slot, from the
 * newest staged FBC Snapshot ("stage_snapshots") or the newest successful ART
 * build ("stream"). operators always holds quay-operator,
 * container-security-operator and quay-bridge-operator. pullspecs,
 * release-from-fbc's fbc-pullspecs, is empty unless state is "complete".
 */
export interface FBCCandidate {
	source: "stage_snapshots" | "stream";
	state: "complete" | "incomplete" | "inconsistent" | "unknown";
	operators: OperatorFBC[];
	pullspecs: string[];
}

/**
 * The tag ART published an operator's FBC for an OCP version under, the
 * multi-arch one whose digest is staged_digest, the FBC RR staged for the
 * slot (basis "staged"). A slot RR staged none of has the newest multi-arch
 * tag instead (basis "newest", staged_digest ""). newer_tag names the newest
 * tag when ART built again after the staged one, "" for none.
 */
export interface FBCTag {
	operator: string;
	ocp: string;
	tag: string;
	/** repository:tag, which release-from-fbc takes as an fbc-pullspec. */
	pullspec: string;
	digest: string;
	staged_digest: string;
	staged_nvr: string;
	basis: "staged" | "newest";
	newer_tag: string;
}

/**
 * A version's operator FBC tags in ART's public repository, one per slot of
 * its candidate (source "candidate"), else of the OCP versions its stream
 * targets ("ocp-build-data"). pullspecs is empty while errors, the slots with
 * no tag, is not; warnings are the slots whose tag is the newest build, not a
 * staged one; info the slots ART built again since RR staged them.
 */
export interface FBCTags {
	repository: string;
	source: "candidate" | "ocp-build-data";
	pullspecs: string[];
	tags: FBCTag[];
	errors: string[];
	warnings: string[];
	info: string[];
}

/**
 * One operator's FBCs, keyed by the bundle heading its newest FBC's release
 * channel ("" for none): members are on that bundle, conflicts are not.
 */
export interface OperatorFBC {
	operator: string;
	head_bundle_nvr: string;
	members: FBCMember[];
	missing_ocp: string[];
	conflicts: FBCMember[];
	/** Whether the head bundle reached stage through a stage Release. */
	fbc_ri_stage: CIState;
	/** The head bundle's relatedImages, by name. */
	images: OperatorImage[];
}

/**
 * A bundle's relatedImage; nvr, commit and build_url (its ART build page) are
 * "" for an image ART did not build. stage is set when a succeeded stage
 * Release holds its digest.
 */
export interface OperatorImage {
	name: string;
	image: string;
	digest: string;
	nvr: string;
	commit: string;
	build_url: string;
	stage: boolean;
	/** null until the image scan sync has read it. */
	scan: ImageScan | null;
}

/**
 * Runs that installed from an FBC: exact from its digest, equivalent from
 * another FBC of its operator and OCP version on the same head bundle.
 */
export interface FBCCI {
	exact: CIJob[];
	equivalent: CIJob[];
}

/**
 * An FBC catalog image; nvr and head_bundle_nvr are "" until resolved. A
 * staged one has its FBC Snapshot's newest Release.
 */
export interface FBCMember {
	ocp: string;
	digest: string;
	pullspec: string;
	nvr: string;
	build_url: string;
	head_bundle_nvr: string;
	release: KonfluxRelease | null;
	/** Set on a slot's FBC. */
	ci: FBCCI | null;
}

/** The version's STAGE build. */
export interface CandidateBuild {
	/** The version's prod Release. */
	prod: KonfluxRelease | null;
}

/**
 * Whether an operator bundle reached the catalog CI installs from, with the
 * per-build stage Release that decides it; "unknown" without a bundle or such
 * Releases.
 */
export interface CIState {
	state: "in" | "failed" | "none" | "unknown";
	release: KonfluxRelease | null;
}

/** A job's runs of a build, newest first. */
export interface CIJob {
	job_name: string;
	runs: CIRun[];
}

/**
 * An ART FBC build of a version with the runs that installed from it, newest
 * first. state is "" unless shipped, the candidate's, or staged.
 */
export interface FBCBuild {
	operator: string;
	ocp: string;
	nvr: string;
	digest: string;
	built_at: string | null;
	state: "" | "candidate" | "staged" | "shipped";
	runs: FBCRun[];
}

export interface FBCRun {
	job: string;
	state: string;
	started_at: string;
	prow_url: string;
}

export interface CIRun {
	state: string;
	started_at: string;
	prow_url: string;
	install_failed?: boolean;
}

/** The CVE scan of an image by its build PipelineRun, whose Konflux page is url. */
export interface ImageScan {
	state: "pending" | "scanned" | "scan_failed" | "not_scanned";
	/** null unless scanned. */
	counts: ScanCounts | null;
	url: string;
}

/**
 * A scan's CVEs by severity, with and without a fix. scanner_arch_findings
 * counts a CVE once per architecture it is found in; unique_cves once.
 */
export interface ScanCounts {
	basis: "scanner_arch_findings" | "unique_cves";
	fixable: SeverityCounts;
	no_fix: SeverityCounts;
}

export interface SeverityCounts {
	critical: number;
	high: number;
	medium: number;
	low: number;
	unknown: number;
}

export interface JiraIssue {
	key: string;
	summary: string;
	issue_type: string;
	/** The Jira status as of the last JIRA sync: "Release Pending". */
	status: string;
	link: string;
}

export interface IssueSummary {
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
	/** "", as is message, when there is nothing to flag. */
	signal: "green" | "yellow" | "red" | "";
	message: string;
}

/** The ART shipment MR of a version in GitLab. */
export interface ShipmentMR {
	iid: number;
	web_url: string;
	state: "opened" | "merged" | "closed";
	draft: boolean;
	stage: "open" | "stage_released" | "prod_released" | "shipped" | "closed";
	pipeline_failed: boolean;
	needs_approval: boolean;
	/** "pointer" when ocp-build-data names the MR, "search" when found by branch. */
	source: "pointer" | "search";
}

export interface ReleaseOverview {
	release: ReleaseVersion;
	issue_summary?: IssueSummary;
	readiness: ReadinessResponse;
	latest_build?: string;
	next_in_stream: boolean;
	shipment_mr: ShipmentMR | null;
}

/** GET /releases/{version}. */
export interface ReleaseDetail extends ReleaseVersion {
	shipment_mr: ShipmentMR | null;
}

export interface DashboardConfig {
	jira_base_url: string;
	jira_project: string;
	jira_enabled: boolean;
	/** "" disables Konflux links. */
	konflux_ui_url: string;
	konflux_namespace: string;
}

export interface SyncProblem {
	source: string;
	message: string;
	since: string | null;
	last_success: string | null;
}

interface SyncSource {
	source: string;
	last_success: string | null;
}

export interface SyncStatus {
	problems: SyncProblem[];
	/** Every tracked source, sorted. */
	sources: SyncSource[];
}
