import type { FBCCandidate, FBCMember } from "../api/types.ts";
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
