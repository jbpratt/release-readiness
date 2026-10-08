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
import { useCachedFetch } from "../hooks/useCachedFetch";
import { relative } from "../utils/format";

const POLL_MS = 60_000;

export default function SyncStatusWarning() {
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

	const problems = data?.problems ?? [];
	if (problems.length === 0) return null;

	const label = `${problems.length} sync problem${problems.length === 1 ? "" : "s"}`;
	return (
		<Popover
			headerContent={label}
			bodyContent={
				<Stack hasGutter>
					{problems.map((p) => (
						<StackItem key={p.source}>
							<Content>
								<Content component="p">
									<strong>{p.source}</strong>: {p.message}
								</Content>
								<Content component="small">
									{p.since && `since ${relative(p.since)} · `}
									last success{" "}
									{p.last_success ? relative(p.last_success) : "never"}
								</Content>
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
			}
		>
			<Button
				variant="plain"
				aria-label={label}
				icon={
					<Icon status="warning">
						<ExclamationTriangleIcon />
					</Icon>
				}
			>
				{problems.length}
			</Button>
		</Popover>
	);
}
