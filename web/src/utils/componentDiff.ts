import type { SnapshotImage } from "../api/types.ts";

export const componentFields = {
	nvr: "NVR",
	digest: "digest",
	upstream: "upstream SHA",
} as const;

export type ComponentField = keyof typeof componentFields;

/** A component's compared values; "" when unknown. */
function componentValues(c: SnapshotImage): Record<ComponentField, string> {
	return {
		nvr: c.art?.nvr ?? "",
		digest: c.image.split("@")[1] ?? "",
		upstream: c.art?.upstream_sha ?? "",
	};
}

/**
 * Fields of each current component that differ from the released Snapshot's,
 * keyed by component name; unchanged components are omitted. A component the
 * released Snapshot lacks differs in every known field. A field unknown on
 * either side is not counted: a missing ART record is no evidence of change.
 */
export function changedFields(
	current: SnapshotImage[],
	released: SnapshotImage[],
): Map<string, ComponentField[]> {
	const shipped = new Map(released.map((c) => [c.name, componentValues(c)]));
	const changed = new Map<string, ComponentField[]>();
	for (const c of current) {
		const now = componentValues(c);
		const was = shipped.get(c.name);
		const fields = (Object.keys(now) as ComponentField[]).filter(
			(f) => now[f] && (!was || (was[f] && now[f] !== was[f])),
		);
		if (fields.length) changed.set(c.name, fields);
	}
	return changed;
}
