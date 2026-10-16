import {
	Button,
	Content,
	Flex,
	Icon,
	Popover,
	Stack,
	StackItem,
	Tooltip,
} from "@patternfly/react-core";
import { ExclamationTriangleIcon, SyncAltIcon } from "@patternfly/react-icons";
import { useEffect } from "react";
import { getSyncStatus } from "../api/client";
import type { SyncProblem } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { relative } from "../utils/format";
import { freshnessLine, SOURCE_NAMES } from "../utils/releaseDetail";
import { muted } from "./OperatorImages";

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

const exact = (iso: string) =>
	new Date(iso).toLocaleString("en-US", {
		month: "short",
		day: "numeric",
		year: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		timeZoneName: "short",
	});

/**
 * The masthead's sync state on every page: "Updated N ago" (the exact time
 * on focus) and "N sources stale", which opens the problems. Compact, at
 * phone width, it is one icon button with those words as its name.
 */
export default function SyncStatusWarning({ compact }: { compact?: boolean }) {
	const status = useSyncStatus();
	if (!status) return null;
	const line = freshnessLine(status);
	const n = line.problems.length;
	const stale = n > 0 ? `${n} source${n === 1 ? "" : "s"} stale` : null;
	const words = [line.updated, stale].filter(Boolean).join(" · ");
	if (!words) return null;
	const warning = (
		<Icon status="warning" isInline={!compact}>
			<ExclamationTriangleIcon />
		</Icon>
	);

	if (compact) {
		return (
			<Popover
				headerContent={words}
				bodyContent={
					<Stack hasGutter>
						{line.oldest && <StackItem>{exact(line.oldest)}</StackItem>}
						{n > 0 && (
							<StackItem>
								<SyncProblems problems={line.problems} />
							</StackItem>
						)}
					</Stack>
				}
			>
				<Button
					variant="plain"
					aria-label={words}
					icon={n > 0 ? warning : <SyncAltIcon />}
				/>
			</Popover>
		);
	}

	return (
		<Flex
			gap={{ default: "gapMd" }}
			alignItems={{ default: "alignItemsCenter" }}
			flexWrap={{ default: "nowrap" }}
			style={{ fontSize: "var(--pf-t--global--font--size--sm)" }}
		>
			{line.oldest && (
				<Tooltip content={exact(line.oldest)}>
					{/* biome-ignore lint/a11y/noNoninteractiveTabindex: focus shows the exact time. */}
					<span tabIndex={0} style={muted}>
						{line.updated}
					</span>
				</Tooltip>
			)}
			{stale && (
				<Popover
					headerContent="Sync problems"
					bodyContent={<SyncProblems problems={line.problems} />}
				>
					<Button
						variant="link"
						isInline
						size="sm"
						icon={warning}
						style={{ fontSize: "inherit" }}
					>
						{stale}
					</Button>
				</Popover>
			)}
		</Flex>
	);
}
