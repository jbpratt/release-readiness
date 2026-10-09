import { Label, Tooltip } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { Link } from "react-router-dom";
import type { TicketEvidence, TicketRow } from "../api/types";

const external = { target: "_blank", rel: "noopener noreferrer" };

/** A Snapshot, linked to the snapshot history row that opens it. */
export function SnapshotLink({
	version,
	name,
}: {
	version: string;
	name: string;
}) {
	return (
		<Link
			to={`/releases/${encodeURIComponent(version)}/snapshots?snapshot=${encodeURIComponent(name)}`}
		>
			<code>{name}</code>
		</Link>
	);
}

function prNumber(url: string) {
	const n = url.match(/\/pull\/(\d+)/)?.[1];
	return n ? `#${n}` : "PR";
}

/** The commits, and their PRs, naming a ticket in component image ranges. */
export function EvidenceList({ evidence }: { evidence: TicketEvidence[] }) {
	if (!evidence.length) return <>-</>;
	return (
		<>
			{evidence.map((e) => (
				<div key={`${e.snapshot}/${e.component}/${e.commit_sha}`}>
					<Tooltip content={`${e.snapshot} ${e.image_digest}`}>
						<code>{e.component}</code>
					</Tooltip>{" "}
					<a href={e.commit_url} {...external}>
						<code>{e.commit_sha.substring(0, 12)}</code>
					</a>
					{e.pr_url && (
						<>
							{" "}
							<a href={e.pr_url} {...external}>
								{prNumber(e.pr_url)}
							</a>
						</>
					)}
				</div>
			))}
		</>
	);
}

const evidenceLabels = {
	complete: { color: "green", text: "proven" },
	none: { color: "grey", text: "no evidence" },
	unknown: { color: "orange", text: "unknown" },
} as const;

/** Ticket membership rows of one bucket. */
export default function TicketTable({
	rows,
	label,
}: {
	rows: TicketRow[];
	label: string;
}) {
	return (
		<Table variant="compact" aria-label={label}>
			<Thead>
				<Tr>
					<Th>Key</Th>
					<Th>Summary</Th>
					<Th>Status</Th>
					<Th modifier="nowrap">Target Version</Th>
					<Th>Build</Th>
					<Th>Commits</Th>
					<Th>Evidence</Th>
					<Th
						modifier="nowrap"
						info={{ tooltip: "Nothing is written to Jira." }}
					>
						Jira action (display only)
					</Th>
				</Tr>
			</Thead>
			<Tbody>
				{rows.map((r) => {
					const state = evidenceLabels[r.evidence_state];
					return (
						<Tr key={r.key}>
							<Td>
								<a href={r.link} {...external}>
									{r.key}
								</a>
							</Td>
							<Td style={{ whiteSpace: "normal", wordBreak: "break-word" }}>
								{r.summary || "-"}
							</Td>
							<Td>{r.status || "unknown"}</Td>
							<Td>{r.target_version || "unknown"}</Td>
							<Td>
								{r.first_seen_snapshot &&
									r.first_seen_snapshot !== r.current_snapshot && (
										<div>
											first in{" "}
											<SnapshotLink
												version={r.concrete_version}
												name={r.first_seen_snapshot}
											/>
										</div>
									)}
								{r.current_snapshot ? (
									<SnapshotLink
										version={r.concrete_version}
										name={r.current_snapshot}
									/>
								) : (
									"no selected build"
								)}
							</Td>
							<Td>
								<EvidenceList evidence={r.evidence} />
							</Td>
							<Td>
								<Label color={state.color} isCompact>
									{state.text}
								</Label>
								{r.evidence_reason && <div>{r.evidence_reason}</div>}
							</Td>
							<Td>
								<code>{r.jira_action}</code>
								{r.jira_action_reason && <div>{r.jira_action_reason}</div>}
							</Td>
						</Tr>
					);
				})}
			</Tbody>
		</Table>
	);
}
