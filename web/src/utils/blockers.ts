import type {
	DashboardConfig,
	ReleaseCandidate,
	VerdictItem,
} from "../api/types";
import { konfluxUrl } from "./links.ts";
import { bundleBuilt } from "./releaseDetail.ts";

/** A place to act on a blocker: a page in RR, or an external tool. */
export interface BlockerLink {
	text: string;
	href: string;
	external: boolean;
}

/** A verdict item, as what blocks, where, who owns it and how to act. */
export interface Blocker {
	level: VerdictItem["level"];
	code: string;
	what: string;
	/** Why, in the words of the tool that failed; "" without one. */
	why: string;
	operator: string;
	ocps: string[];
	owner: string;
	action: string;
	links: BlockerLink[];
}

/** Blockers of one level, code, operator and why, counted as one. */
export type BlockerGroup = Blocker & { count: number };

// What blocks, by code, in a few words.
const titles: Record<string, string> = {
	not_staged: "Not staged",
	missing_operator: "Operator not staged",
	missing_ocp: "OCP versions not staged",
	csv_mismatch: "FBCs ship the wrong version",
	mixed_builds: "FBCs on different bundles",
	image_mismatch: "Image and FBC shipments differ",
	stage_push_failed: "Image push to stage failed",
	read_failed: "FBC unreadable",
	no_head_bundle: "FBC heads no known bundle",
	stage_release_failed: "Stage release failed",
	unknown_fbc: "Staged FBCs without an ART build",
	stream_ahead: "Newer build not on stage",
};

// The "Next:" step, by code. Draft copy: ART has not reviewed it yet.
const actions: Record<string, string> = {
	not_staged: "Stage the candidate with release-from-fbc",
	missing_operator: "Add the operator's FBCs to the stage shipment",
	missing_ocp: "Add the missing OCP versions to the stage shipment",
	csv_mismatch: "Rebuild the FBCs on this version's bundle",
	image_mismatch: "Ship the bundle the FBCs name",
	stage_push_failed: "Fix the failed task, then re-run the push",
	read_failed: "Check the FBC image can be pulled",
	no_head_bundle: "Check the FBC's channel head",
	stage_release_failed: "Fix the failed task, then re-run the stage release",
	unknown_fbc: "Find the builds of the unnamed FBCs",
	stream_ahead: "Decide whether to stage the newer build",
};

const mixedBuildsAction = (ocps: string[], bundle: string) =>
	`Rebuild OCP ${ocps.join(", ")} on the ${bundle} bundle`;

// Why a stage release failed, by its managed pipeline task.
const failedTasks: Record<string, string> = {
	"verify-conforma": "Enterprise Contract policy failed",
	"apply-mapping": "Mapping config failed",
	"verify-access-to-resources": "Registry or resource access failed",
};

/** The OCP versions an item names: "op OCP 4.17, 4.21: ..." -> 4.17, 4.21. */
function itemOcps(i: VerdictItem): string[] {
	const m = i.text.match(/OCP ([\d.]+(?:, [\d.]+)*)/);
	return m ? m[1].split(", ") : [];
}

/**
 * Each verdict item of d as a blocker with its links to act on it. release
 * is the version's route name, app its Konflux application.
 */
export function blockers(
	d: ReleaseCandidate,
	release: string,
	app: string,
	config: DashboardConfig | undefined,
): Blocker[] {
	const ops = d.fbc_candidate?.operators ?? [];
	return d.verdict.items.map((i) => {
		const ocps = itemOcps(i);
		const o = ops.find((x) => x.operator === i.operator);
		const task = i.text.match(/\(([\w-]+)\)$/)?.[1] ?? "";
		const links: BlockerLink[] = [];
		let action = actions[i.code] ?? "";
		if (i.code === "stage_release_failed" && o) {
			for (const ocp of ocps) {
				const m = [...o.members, ...o.conflicts].find((x) => x.ocp === ocp);
				const href =
					m?.release &&
					konfluxUrl(
						config?.konflux_ui_url ?? "",
						config?.konflux_namespace ?? "",
						app,
						"releases",
						m.release.name,
					);
				if (href)
					links.push({ text: `Release OCP ${ocp}`, href, external: true });
			}
		}
		if (i.code === "mixed_builds" && o) {
			action = mixedBuildsAction(ocps, bundleBuilt(o.head_bundle_nvr));
		}
		if (i.operator && i.code !== "stage_release_failed") {
			links.push({
				text: "View FBCs",
				href: `/releases/${encodeURIComponent(release)}?operator=${encodeURIComponent(i.operator)}`,
				external: false,
			});
		}
		return {
			level: i.level,
			code: i.code,
			what: titles[i.code] ?? i.text,
			why: failedTasks[task] ?? task,
			operator: i.operator,
			ocps,
			owner: i.owner,
			action,
			links,
		};
	});
}

/**
 * Blockers of one level, code, operator and why as one, their OCPs and
 * distinct links joined. Each OCP's stage release can fail on its own task.
 */
export function grouped(bs: Blocker[]): BlockerGroup[] {
	const out = new Map<string, BlockerGroup>();
	for (const b of bs) {
		const key = `${b.level} ${b.code} ${b.operator} ${b.why}`;
		const g = out.get(key);
		if (g) {
			g.count++;
			g.ocps = [...g.ocps, ...b.ocps];
			g.links = [
				...g.links,
				...b.links.filter((l) => !g.links.some((x) => x.href === l.href)),
			];
		} else out.set(key, { ...b, count: 1 });
	}
	return [...out.values()];
}

/** Grouped blockers, and grouped warnings and notes. */
export function splitBlockers(bs: Blocker[]): {
	blocking: BlockerGroup[];
	rest: BlockerGroup[];
} {
	return {
		blocking: grouped(bs.filter((b) => b.level === "blocker")),
		rest: grouped(bs.filter((b) => b.level !== "blocker")),
	};
}

/** Where a blocker is: "container-security-operator · OCP 4.17, 4.21". */
export const where = (b: Blocker) =>
	[b.operator || "Whole release", b.ocps.length && `OCP ${b.ocps.join(", ")}`]
		.filter(Boolean)
		.join(" · ");
