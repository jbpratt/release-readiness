import type { FBCCandidate, FBCMember } from "../api/types.ts";
import type { Blocker } from "./blockers.ts";
import { type FbcRow, runResult, type StatusLine } from "./releaseDetail.ts";

/** What keeps an FBC's cell from green, with its status. */
export const PROBLEMS = {
	"not built": { status: "failed" },
	"can't read": { status: "failed" },
	"older bundle": { status: "warning" },
	"stage failed": { status: "failed" },
	staging: { status: "running" },
	"not staged": { status: "warning" },
	"CI failed": { status: "failed" },
} satisfies Record<string, { status: StatusLine["status"] }>;

export type Problem = keyof typeof PROBLEMS;

/**
 * An FBC of the matrix, the stream's FBC for its operator and OCP version,
 * and the first thing keeping it from green, if any.
 */
export interface Cell {
	row: FbcRow;
	stream: FBCMember | null;
	problem: Problem | null;
}

/** The stream's FBC for a row's operator and OCP version, or null. */
export function streamFbc(
	stream: FBCCandidate | null,
	r: FbcRow,
): FBCMember | null {
	const o = stream?.operators.find((x) => x.operator === r.operator);
	return (
		[...(o?.members ?? []), ...(o?.conflicts ?? [])].find(
			(m) => m.ocp === r.ocp,
		) ?? null
	);
}

/**
 * The first thing keeping a row's FBC from green, or null. A missing FBC the
 * stream has is not staged, else not built. Without a stage shipment (staged
 * false) nothing is staged, so staging does not count. CI without runs does
 * not either: the verdict does not require it.
 */
export function cellProblem(
	r: FbcRow,
	staged: boolean,
	stream: FBCMember | null = null,
): Problem | null {
	if (!r.fbc) return stream ? "not staged" : "not built";
	if (r.flags.some((f) => f.text === "can't read")) return "can't read";
	if (r.flags.some((f) => f.text.startsWith("holds an older")))
		return "older bundle";
	if (r.stage?.status === "failed") return "stage failed";
	if (r.stage?.status === "running") return "staging";
	if (staged && !r.stage) return "not staged";
	if (r.runs.some((p) => runResult(p.state).status === "failed"))
		return "CI failed";
	return null;
}

/** A candidate's one signal on the release list, worst first. */
export type Tone = "danger" | "warning" | "running" | "info" | "success";

const levelTones = {
	blocker: "danger",
	warning: "warning",
	info: "info",
} satisfies Record<Blocker["level"], Tone>;

/**
 * A candidate as one tone and a few words: the worst of its FBC cells, by
 * the release page's cell rules, and its items; its first blocker or warning
 * and "+N" for more kinds, else how many of its FBCs are green.
 */
export function candidateSummary(
	cells: Cell[],
	bs: Blocker[],
): { tone: Tone; words: string; more: string; all: string } {
	const cellTones = cells.map(({ row, problem }): Tone => {
		const status = problem && PROBLEMS[problem].status;
		const levels = bs
			.filter((b) => b.operator === row.operator && b.ocps.includes(row.ocp))
			.map((b) => b.level);
		if (!row.fbc || status === "failed" || levels.includes("blocker"))
			return "danger";
		if (status === "running") return "running";
		if (status === "warning" || levels.includes("warning")) return "warning";
		return levels.includes("info") ? "info" : "success";
	});
	const tones = [...cellTones, ...bs.map((b) => levelTones[b.level])];
	const tone =
		(["danger", "warning", "running", "info"] as const).find((t) =>
			tones.includes(t),
		) ?? "success";
	const good = `${cellTones.filter((t) => t === "success").length} of ${cells.length} FBCs`;
	const kinds = [
		...new Set(
			[
				...bs.filter((b) => b.level === "blocker"),
				...bs.filter((b) => b.level === "warning"),
			].map((b) => b.what),
		),
	];
	if (kinds.length === 0)
		return { tone, words: good, more: "", all: `${good} good` };
	return {
		tone,
		words: kinds[0],
		more: kinds.length > 1 ? `+${kinds.length - 1}` : "",
		all: kinds.join("; "),
	};
}
