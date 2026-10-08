import type { KonfluxRelease } from "../api/types";
import { relative } from "./format.ts";

// Why a failed Release failed, by the managed pipeline task it stopped at.
const failedTasks: Record<string, { text: string; detail: string }> = {
	"apply-mapping": {
		text: "Release failed: mapping config (ART)",
		detail:
			"Component-to-registry mapping config error. Owner: ART (ReleasePlanAdmission mapping).",
	},
	"verify-access-to-resources": {
		text: "Release failed: registry/resource access (ART/Konflux)",
		detail:
			"Could not verify access to the target registry or resources. Owner: ART/Konflux infra.",
	},
	"verify-conforma": {
		text: "Release failed: Enterprise Contract policy",
		detail:
			"Blocked by the Enterprise Contract (Conforma) policy gate. Owner: Quay (fix the flagged image or metadata).",
	},
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
			text: known?.text ?? (at ? `Release failed at ${at}` : "Release failed"),
			detail: known?.detail,
		} as const;
	}
	return { color: "blue", text: "Release in progress" } as const;
}
