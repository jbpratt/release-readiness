import {
	EmptyState,
	EmptyStateBody,
	Flex,
	Label,
	PageSection,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import type { ReactNode } from "react";
import type { FBCBuild } from "../api/types";
import {
	CIBreadcrumb,
	CIFilters,
	muted,
	states,
	type useReleaseCI,
	when,
} from "../pages/ReleaseCI";

const runStates: Record<string, [string, "green" | "red" | "blue" | "grey"]> = {
	success: ["Passed", "green"],
	failure: ["Failed", "red"],
	error: ["Error", "red"],
	pending: ["Running", "blue"],
	aborted: ["Aborted", "grey"],
};

export function RunState({ state }: { state: string }) {
	const [text, color] = runStates[state] ?? [state, "grey"];
	return (
		<Label isCompact color={color}>
			{text}
		</Label>
	);
}

/**
 * When the FBC was built, from built_at or else the NVR's UTC timestamp, with
 * the full NVR on hover: the time is what tells two builds apart.
 */
export function BuildTime({ build }: { build: FBCBuild }) {
	const ts = build.nvr.match(/-(\d{4})(\d\d)(\d\d)(\d\d)(\d\d)\d\d\./);
	const at =
		build.built_at ??
		(ts && `${ts[1]}-${ts[2]}-${ts[3]}T${ts[4]}:${ts[5]}:00Z`);
	return (
		<Tooltip content={build.nvr}>
			<span style={{ ...muted, textDecoration: "underline dotted" }}>
				Built {at ? when(at) : "unknown"}
			</span>
		</Tooltip>
	);
}

export function JobName({ job, label }: { job: string; label: string }) {
	return (
		<Tooltip content={job}>
			<span style={{ overflowWrap: "anywhere" }}>{label}</span>
		</Tooltip>
	);
}

export function BuildTitle({ build }: { build: FBCBuild }) {
	return (
		<Flex
			gap={{ default: "gapSm" }}
			alignItems={{ default: "alignItemsCenter" }}
		>
			<span>
				{build.operator} · OCP {build.ocp}
			</span>
			{build.state && (
				<Label isCompact color={states[build.state][1]}>
					{states[build.state][0]}
				</Label>
			)}
		</Flex>
	);
}

/**
 * The page around a mockup's build list: breadcrumb, heading, filters, and
 * the load, error and empty states.
 */
export function FbcCiPage({
	ci,
	children,
}: {
	ci: ReturnType<typeof useReleaseCI>;
	children: ReactNode;
}) {
	const { version, data, error, shown, all } = ci;
	const filtered = ci.operator || ci.ocp;
	return (
		<PageSection>
			<CIBreadcrumb version={version} />
			<Title headingLevel="h1" size="xl" style={{ marginBottom: 8 }}>
				FBC CI history
			</Title>
			<CIFilters
				ci={ci}
				allLabel="Include builds with no runs"
				style={{ marginBottom: 24 }}
			/>
			{error ? (
				<div>Failed to load CI history: {error.message}</div>
			) : !data ? (
				<Spinner size="lg" />
			) : shown.length === 0 ? (
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						No FBC builds to show
					</Title>
					<EmptyStateBody>
						{filtered
							? "No build matches these filters."
							: all
								? "This version has no FBC builds."
								: `None of this version's ${data.length} FBC builds has a CI run yet. Include builds with no runs to list them.`}
					</EmptyStateBody>
				</EmptyState>
			) : (
				children
			)}
		</PageSection>
	);
}
