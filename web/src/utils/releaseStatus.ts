import type { KonfluxRelease } from "../api/types";
import { relative } from "./format.ts";

// Why a failed Release failed, by the managed pipeline task it stopped at.
const failedTasks: Record<string, string> = {
	"apply-mapping": "Release failed: mapping config (ART)",
	"verify-access-to-resources":
		"Release failed: registry/resource access (ART/Konflux)",
	"verify-conforma": "Release failed: Enterprise Contract policy",
};

// A running Release is also Released=False, with reason Progressing.
export function releaseStatus(r: KonfluxRelease) {
	if (r.released_status === "True") {
		return {
			color: "green",
			text: r.completion_time
				? `Released ${relative(r.completion_time)}`
				: "Released",
		} as const;
	}
	if (r.released_reason === "Failed") {
		const known = failedTasks[r.failed_task ?? ""];
		const at = [r.failed_task, r.failed_step].filter(Boolean).join("/");
		return {
			color: "red",
			text: known
				? `${known} (${at})`
				: at
					? `Release failed at ${at}`
					: "Release failed",
		} as const;
	}
	return { color: "blue", text: "Release in progress" } as const;
}
