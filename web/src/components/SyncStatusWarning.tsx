import {
	Button,
	Content,
	Icon,
	Popover,
	Stack,
	StackItem,
} from "@patternfly/react-core";
import { ExclamationTriangleIcon } from "@patternfly/react-icons";
import { useEffect } from "react";
import { getSyncStatus } from "../api/client";
import type { SyncProblem } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { SHOW_CVES } from "../utils/features";
import { relative } from "../utils/format";
import { SOURCE_NAMES } from "../utils/releaseDetail";

const POLL_MS = 60_000;

/** The sync status, polled; every caller shares the one cache entry. */
export function useSyncStatus() {
	// The TTL sits under the poll period so every tick refetches.
	const { data, refetch } = useCachedFetch(
		"sync-status",
		getSyncStatus,
		POLL_MS / 2,
	);

	useEffect(() => {
		const id = setInterval(refetch, POLL_MS);
		return () => clearInterval(id);
	}, [refetch]);
	return data;
}

// What each sync feeds, so a problem says what it may have left out of date.
const AFFECTS: Record<string, string> = {
	konflux: "snapshots, releases, stage/prod status",
	"art-builds": "builds",
	"art-history": "builds",
	"fbc-render": "FBC bundles",
	catalog: "shipped versions",
	jira: "tickets",
	"image-scans": "CVE scans",
	prow: "CI runs",
	"shipment-mrs": "shipment MRs",
};

/**
 * Each problem: its message, since when it fails, its last success, and what
 * it affects.
 */
export function SyncProblems({ problems }: { problems: SyncProblem[] }) {
	return (
		<Stack hasGutter>
			{problems.map((p) => (
				<StackItem key={p.source}>
					<Content>
						<Content component="p">
							<strong>{SOURCE_NAMES[p.source] ?? p.source}</strong>: {p.message}
						</Content>
						<Content component="small">
							{p.since && `since ${relative(p.since)} · `}
							last success {p.last_success ? relative(p.last_success) : "never"}
						</Content>
						{AFFECTS[p.source] && (
							<Content component="p">Affects: {AFFECTS[p.source]}</Content>
						)}
						{/* Failing sources carry since; stale ones do not. */}
						{!p.since && (
							<Content component="p">
								Builds or snapshots from the stale period may not be visible
								yet.
							</Content>
						)}
					</Content>
				</StackItem>
			))}
		</Stack>
	);
}

export default function SyncStatusWarning() {
	// A CVE scan problem would point at displays that are hidden.
	const problems = (useSyncStatus()?.problems ?? []).filter(
		(p) => SHOW_CVES || p.source !== "image-scans",
	);
	if (problems.length === 0) return null;

	const label = `${problems.length} sync problem${problems.length === 1 ? "" : "s"}`;
	return (
		<Popover
			headerContent={label}
			bodyContent={<SyncProblems problems={problems} />}
		>
			<Button
				variant="plain"
				icon={
					<Icon status="warning">
						<ExclamationTriangleIcon />
					</Icon>
				}
			>
				{label}
			</Button>
		</Popover>
	);
}
