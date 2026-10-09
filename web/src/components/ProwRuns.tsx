import {
	Button,
	Card,
	CardBody,
	Content,
	ExpandableSection,
	Label,
	Modal,
	ModalBody,
	ModalHeader,
	Spinner,
	Tooltip,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useState } from "react";
import { listUnlinkedProwRuns } from "../api/client";
import type { ProwRun, ProwRunsResponse } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { relative } from "../utils/format";
import StatusLabel from "./StatusLabel";

const external = { target: "_blank", rel: "noopener noreferrer" };

function runDate(run: ProwRun): string | null {
	return run.completed_at ?? run.started_at;
}

function When({ iso }: { iso: string | null }) {
	if (!iso) return <>-</>;
	return (
		<Tooltip content={new Date(iso).toLocaleString()}>
			<span>{relative(iso)}</span>
		</Tooltip>
	);
}

/** When the run's jobs were last listed, flagged once two polls were missed. */
function ProwSyncNote({
	sync,
}: {
	sync: Pick<ProwRunsResponse, "last_successful_sync" | "stale">;
}) {
	return (
		<Content component="small">
			Data last checked{" "}
			{sync.last_successful_sync
				? relative(sync.last_successful_sync)
				: "never"}{" "}
			{sync.stale && (
				<Label color="orange" isCompact>
					Stale
				</Label>
			)}
		</Content>
	);
}

/** The matched digest, else the catalog; unknown when there is no artifact. */
function TestedImage({ run, digest }: { run: ProwRun; digest?: string }) {
	if (run.artifact_state === "missing") {
		return (
			<Tooltip content="The run has not published tested-images.json, so the images it tested are unknown.">
				<Label color="grey" isCompact>
					No artifact
				</Label>
			</Tooltip>
		);
	}
	if (run.artifact_state === "invalid") {
		return (
			<Label color="orange" isCompact>
				Invalid artifact
			</Label>
		);
	}
	const shown = digest ?? run.catalog_ref.split("@")[1] ?? "";
	return (
		<code style={{ fontSize: "0.85em" }} title={digest ?? run.catalog_ref}>
			{shown.replace(/^(sha256:.{12}).*/, "$1") || "-"}
		</code>
	);
}

/** Run history: date, job, result, tested digest and Prow link. */
function ProwRunsTable({ runs, digest }: { runs: ProwRun[]; digest?: string }) {
	return (
		<Table variant="compact" aria-label="Prow runs">
			<Thead>
				<Tr>
					<Th>Date</Th>
					<Th>Job</Th>
					<Th>Result</Th>
					<Th>{digest ? "Tested digest" : "Tested catalog"}</Th>
					<Th screenReaderText="Prow link" />
				</Tr>
			</Thead>
			<Tbody>
				{runs.map((r) => (
					<Tr key={`${r.job_name}/${r.build_id}`}>
						<Td>
							<When iso={runDate(r)} />
						</Td>
						<Td modifier="breakWord">{r.job_name}</Td>
						<Td>
							<StatusLabel status={r.state || "unknown"} />
						</Td>
						<Td>
							<TestedImage run={r} digest={digest} />
						</Td>
						<Td>
							{r.prow_url && (
								<a href={r.prow_url} {...external}>
									Prow
								</a>
							)}
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

/** Newest run that tested this exact image, opening its run history. */
export function ProwRunBadge({
	component,
	image,
	runs,
	sync,
}: {
	component: string;
	image: string;
	runs: ProwRun[];
	sync: Pick<ProwRunsResponse, "last_successful_sync" | "stale">;
}) {
	const [open, setOpen] = useState(false);
	const run = runs[0];
	if (!run) {
		return (
			<span style={{ color: "var(--pf-t--global--text--color--subtle)" }}>
				No exact run match
			</span>
		);
	}
	const titleId = `prow-runs-${component}`;
	return (
		<>
			<Button variant="link" isInline onClick={() => setOpen(true)}>
				<span style={{ whiteSpace: "nowrap" }}>
					<StatusLabel status={run.state || "unknown"} />{" "}
					{relative(runDate(run) ?? run.fetched_at)}
				</span>
			</Button>
			<Modal
				variant="large"
				isOpen={open}
				onClose={() => setOpen(false)}
				aria-labelledby={titleId}
			>
				<ModalHeader
					title={`CI runs that tested ${component}`}
					labelId={titleId}
					description={<ProwSyncNote sync={sync} />}
				/>
				<ModalBody>
					<ProwRunsTable runs={runs} digest={image.split("@")[1]} />
				</ModalBody>
			</Modal>
		</>
	);
}

/** The release application's runs that tested no image of its Snapshots. */
export function UnlinkedProwRuns({ version }: { version: string }) {
	const [expanded, setExpanded] = useState(false);
	const { data, loading, error } = useCachedFetch(
		`unlinkedProwRuns:${version}`,
		() => listUnlinkedProwRuns(version),
	);
	// No configured job has ever synced for this release.
	if (!data?.last_successful_sync && !data?.runs.length) {
		if (loading) return <Spinner size="md" />;
		if (error) return <Content component="p">{error.message}</Content>;
		return null;
	}
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardBody>
				<ExpandableSection
					toggleText={`Unlinked CI runs (${data.runs.length})`}
					isExpanded={expanded}
					onToggle={(_, v) => setExpanded(v)}
				>
					<Content component="p">
						Runs of this release's jobs that match no Snapshot image on (role,
						digest), including runs that have not published tested-images.json.{" "}
						<ProwSyncNote sync={data} />
					</Content>
					{data.runs.length === 0 ? (
						<Content component="p">No unlinked runs.</Content>
					) : (
						<ProwRunsTable runs={data.runs} />
					)}
				</ExpandableSection>
			</CardBody>
		</Card>
	);
}
