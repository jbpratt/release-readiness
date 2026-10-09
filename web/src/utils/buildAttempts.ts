import type { BuildAttempt } from "../api/types";

export interface ComponentBuilds {
	component: string;
	attempts: number;
	failed: number;
	/** Attempts started right after a failed one. */
	retries: number;
	/** null when no build succeeded in the covered span: older is unknown. */
	lastSuccess: BuildAttempt | null;
}

const isFailure = (a: BuildAttempt) =>
	a.outcome !== "success" && a.outcome !== "pending";

/** Each component's attempts, by component name. */
export function summarizeBuilds(attempts: BuildAttempt[]): ComponentBuilds[] {
	const byComponent = new Map<string, ComponentBuilds>();
	const oldestFirst = [...attempts].sort((a, b) =>
		a.started_at.localeCompare(b.started_at),
	);
	const previous = new Map<string, BuildAttempt>();
	for (const a of oldestFirst) {
		let c = byComponent.get(a.component);
		if (!c) {
			c = {
				component: a.component,
				attempts: 0,
				failed: 0,
				retries: 0,
				lastSuccess: null,
			};
			byComponent.set(a.component, c);
		}
		const prev = previous.get(a.component);
		c.attempts++;
		if (isFailure(a)) c.failed++;
		if (prev && isFailure(prev)) c.retries++;
		if (a.outcome === "success") c.lastSuccess = a;
		previous.set(a.component, a);
	}
	return [...byComponent.values()].sort((a, b) =>
		a.component.localeCompare(b.component),
	);
}
