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
	/** The version's FBC catalogs, staged ones first. */
	catalogs: CandidateCatalog[] | null;
	/**
	 * With a STAGE build, the newest image of the version's builds of each
	 * component it leaves out.
	 */
	unshipped_builds: UnshippedBuild[] | null;
	/**
	 * The version's operator FBCs up for release: as ART staged them for
	 * stage, else as its stream built them. Null while the version has no
	 * FBC built or staged.
	 */
	fbc_candidate: FBCCandidate | null;
	/** The version's operator FBCs as its stream built them. */
	fbc_stream: FBCCandidate | null;
	/**
	 * By operator and OCP version, the CI of the FBC candidate's FBC of each
	 * slot. Advisory: the verdict never reads it.
	 */
	ci_history: CISlot[] | null;
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
 * container-security-operator and quay-bridge-operator; unknown holds the
 * staged catalogs no NVR names, which fill no slot. pullspecs, release-from-fbc's
 * fbc-pullspecs, is empty unless state is "complete".
 */
export interface FBCCandidate {
	source: "stage_snapshots" | "stream";
	state: "complete" | "incomplete" | "inconsistent" | "unknown";
	errors: string[];
	operators: OperatorFBC[];
	unknown: FBCMember[];
	pullspecs: string[];
}

/**
 * One operator's FBCs, keyed by the bundle heading its newest FBC's release
 * channel ("" for none): members are on that bundle, conflicts are not.
 */
export interface OperatorFBC {
	operator: string;
	head_bundle_nvr: string;
	channel: string;
	head_csv: string;
	coverage: { members: number; slots: number };
	members: FBCMember[];
	missing_ocp: string[];
	conflicts: FBCMember[];
	/** Whether the head bundle reached stage through a stage Release. */
	fbc_ri_stage: CIState;
	image_check: ImageCheck;
	/** The head bundle's relatedImages, by name. */
	images: OperatorImage[];
	/**
	 * The images against the previous entry's and, for a candidate, against
	 * the stream's newer head bundle.
	 */
	compare: ImageCompare[];
	stage: StageSummary;
}

/** How many of an operator's members' newest stage Release succeeded. */
export interface StageSummary {
	staged: number;
	total: number;
}

/**
 * A bundle's relatedImage; nvr, commit and build_url (its ART build page) are
 * "" for an image ART did not build. stage and prod are set when a succeeded
 * Release there holds its digest.
 */
export interface OperatorImage {
	name: string;
	image: string;
	digest: string;
	nvr: string;
	commit: string;
	build_url: string;
	stage: boolean;
	prod: boolean;
	/** null until the image scan sync has read it. */
	scan: ImageScan | null;
}

/**
 * Two bundles' relatedImages: "previous" from the previous entry of label's
 * version, "newer" to the stream's head bundle of NVR label.
 */
export interface ImageCompare {
	kind: "previous" | "newer";
	label: string;
	rows: ImageCompareRow[];
}

/**
 * A relatedImage's from and to, null where absent, named as to names it;
 * "renamed" when they are in different repositories.
 */
export interface ImageCompareRow {
	name: string;
	from: ImageRef | null;
	to: ImageRef | null;
	change:
		| "renamed"
		| "same"
		| "rebuild"
		| "new source"
		| "new build"
		| "added"
		| "removed";
}

/** A compared image; build_url is its ART build page, "" for an image ART did not build. */
export interface ImageRef {
	repository: string;
	nvr: string;
	commit: string;
	digest: string;
	build_url: string;
}

/**
 * The head bundle against the image Snapshot's bundle of the operator: its
 * STAGE build, else its newest build. "absent" when either NVR is unknown.
 */
export interface ImageCheck {
	state: "matches" | "differs" | "absent";
	snapshot: string;
	bundle_nvr: string;
}

/**
 * An operator's OCP slot with the CI of the FBC candidate's FBC of it, null
 * without one.
 */
export interface CISlot {
	operator: string;
	ocp: string;
	ci: FBCCI | null;
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
 * staged one has its FBC Snapshot and that Snapshot's newest Release.
 */
export interface FBCMember {
	ocp: string;
	digest: string;
	pullspec: string;
	nvr: string;
	build_url: string;
	head_bundle_nvr: string;
	snapshot: string;
	release: KonfluxRelease | null;
	/** Set on a slot's FBC; null on an unknown catalog. */
	ci: FBCCI | null;
	/**
	 * In a candidate's slot, how many staged FBC Snapshots holding a catalog
	 * of it last released successfully.
	 */
	staged: number;
}

/** An image of a component the candidate leaves out, and its stream build. */
export interface UnshippedBuild extends CandidateComponent {
	snapshot: string;
}

/**
 * An FBC catalog image of the version, the Snapshot holding it and its ART
 * build; nvr and ocp are "" until ART resolves it. A staged catalog is one of
 * an FBC Snapshot ART staged for stage, with that Snapshot's newest Release,
 * null before one, and its ReleasePlan; a built one is the newest catalog of
 * an operator and OCP version the FBC stream built and ART did not stage.
 */
export interface CandidateCatalog {
	state: "staged" | "built";
	snapshot: string;
	component: string;
	/** The catalog's pullspec, repo@sha256:... */
	pullspec: string;
	nvr: string;
	ocp: string;
	release: KonfluxRelease | null;
	release_plan: string;
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
	sippy_url: string;
}

export interface CIRun {
	state: string;
	started_at: string;
	prow_url: string;
	install_failed?: boolean;
}

/** A build image; nvr and build_url are "" until ART build history has it. */
export interface CandidateComponent {
	name: string;
	/** The image's pullspec, repo@sha256:... */
	image: string;
	nvr: string;
	build_url: string;
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
	head_pipeline_status: string;
	merged_at?: string;
	updated_at: string;
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
