import { Flex } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type { FBCBuild, FBCRun } from "../api/types";
import { external, muted, useReleaseCI, when } from "../pages/ReleaseCI";
import { BuildTime, BuildTitle, FbcCiPage, JobName, RunState } from "./shared";

/** Design B: one table row per run, or per build without runs. */
export default function FbcCiTable() {
	const ci = useReleaseCI();
	const rows = ci.shown.flatMap((b) =>
		b.runs.length > 0
			? b.runs.map((r): { b: FBCBuild; r?: FBCRun } => ({ b, r }))
			: [{ b }],
	);
	return (
		<FbcCiPage ci={ci}>
			<Table variant="compact" aria-label="FBC CI runs">
				<Thead>
					<Tr>
						<Th>Build</Th>
						<Th>Job</Th>
						<Th>Result</Th>
						<Th>Started</Th>
					</Tr>
				</Thead>
				<Tbody>
					{rows.map(({ b, r }) => (
						<Tr key={r?.prow_url ?? b.nvr}>
							<Td dataLabel="Build">
								<Flex
									direction={{ default: "column" }}
									gap={{ default: "gapXs" }}
								>
									<BuildTitle build={b} />
									<BuildTime build={b} />
								</Flex>
							</Td>
							<Td dataLabel="Job">
								{r ? (
									<JobName job={r.job} label={ci.label(r.job)} />
								) : (
									<span style={muted}>No runs</span>
								)}
							</Td>
							<Td dataLabel="Result">
								{/* A div, so the stacked layout does not stretch the Label. */}
								<div>{r && <RunState state={r.state} />}</div>
							</Td>
							<Td dataLabel="Started">
								{r && (
									<a href={r.prow_url} {...external}>
										{when(r.started_at)}
									</a>
								)}
							</Td>
						</Tr>
					))}
				</Tbody>
			</Table>
		</FbcCiPage>
	);
}
