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

const POLL_MS = 60_000;

const units: [Intl.RelativeTimeFormatUnit, number][] = [
	["day", 86_400],
	["hour", 3_600],
	["minute", 60],
	["second", 1],
];

function relative(iso: string): string {
	const secs = Math.round((new Date(iso).getTime() - Date.now()) / 1000);
	const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
	const [unit, size] = units.find(([, size]) => Math.abs(secs) >= size) ?? [
		"second",
		1,
	];
	return rtf.format(Math.round(secs / size), unit);
}

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
