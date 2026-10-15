import { type FbcRow, runResult, type StatusLine } from "./releaseDetail.ts";

/**
 * What keeps an FBC's cell from green, in the order a cell shows the first
 * of them, with its status and its words in the headline.
 */
export const PROBLEMS = {
	"not built": { status: "failed", gap: "missing" },
	"can't read": { status: "failed", gap: "unreadable" },
	"older bundle": { status: "warning", gap: "listing an older bundle" },
	"stage failed": { status: "failed", gap: "failed to stage" },
	staging: { status: "running", gap: "still staging" },
	"not staged": { status: "warning", gap: "not staged" },
	"CI failed": { status: "failed", gap: "failing CI" },
} satisfies Record<string, { status: StatusLine["status"]; gap: string }>;

export type Problem = keyof typeof PROBLEMS;

const ORDER = Object.keys(PROBLEMS) as Problem[];

/** An FBC of the matrix and the first thing keeping it from green, if any. */
export interface Cell {
	row: FbcRow;
	problem: Problem | null;
}

/**
 * The first thing keeping a row's FBC from green, or null. Without a stage
 * shipment (staged false) nothing is staged, so staging does not count.
 * CI without runs does not either: the verdict does not require it.
 */
export function cellProblem(r: FbcRow, staged: boolean): Problem | null {
	if (!r.fbc) return "not built";
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

const uniq = (xs: string[]) => [...new Set(xs)];
const fbcs = (n: number) => `${n} FBC${n === 1 ? "" : "s"}`;

/**
 * The matrix's headline: one line per problem, in PROBLEMS order, naming its
 * FBCs, and the count of FBCs built (and, with a stage shipment, staged).
 */
export function headline(
	cells: Cell[],
	staged: boolean,
): { gaps: StatusLine[]; count: string } {
	const total = cells.length;
	const done = cells.filter(
		(c) => c.row.fbc && (!staged || c.row.stage?.status === "done"),
	).length;
	const gaps = ORDER.flatMap((p): StatusLine[] => {
		const cs = cells.filter((c) => c.problem === p);
		if (cs.length === 0) return [];
		const { status, gap } = PROBLEMS[p];
		if (cs.length === total && total > 1)
			return [{ status, text: `All ${total} FBCs ${gap}` }];
		const ocps = uniq(cs.map((c) => c.row.ocp));
		const ops = uniq(cs.map((c) => c.row.operator));
		const where =
			ocps.length === 1
				? ` for OCP ${ocps[0]}: ${ops.join(", ")}`
				: ops.length === 1
					? ` for ${ops[0]}: OCP ${ocps.join(", ")}`
					: `: ${cs.map((c) => `${c.row.operator} OCP ${c.row.ocp}`).join(", ")}`;
		return [{ status, text: `${fbcs(cs.length)} ${gap}${where}` }];
	});
	return {
		gaps,
		count: `${done} of ${total} FBCs ${staged ? "built and staged" : "built"}`,
	};
}
