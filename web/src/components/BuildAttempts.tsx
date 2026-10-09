import {
	Card,
	CardBody,
	CardTitle,
	Label,
	Spinner,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { getBuildAttempts } from "../api/client";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { summarizeBuilds } from "../utils/buildAttempts";
import { relative } from "../utils/format";

/** Each component's ART image builds in the span ART history was read without a gap. */
export default function BuildAttempts({ version }: { version: string }) {
	const { data, error } = useCachedFetch(`buildAttempts:${version}`, () =>
		getBuildAttempts(version),
	);
	const since = data?.covered_from
		? new Date(data.covered_from).toLocaleDateString()
		: null;
	const rows = summarizeBuilds(data?.attempts ?? []);

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				ART image builds{since && ` started since ${since}`}
			</CardTitle>
			<CardBody>
				{error ? (
					error.message
				) : !data ? (
					<Spinner size="md" />
				) : !since ? (
					"ART build history has not been read for this stream: failures and last successful builds are unknown."
				) : rows.length === 0 ? (
					`No image builds of this version started since ${since}.`
				) : (
					<Table variant="compact" aria-label="ART image builds">
						<Thead>
							<Tr>
								<Th>Component</Th>
								<Th>Builds</Th>
								<Th>Failed</Th>
								<Th>Retries</Th>
								<Th>Last successful build</Th>
							</Tr>
						</Thead>
						<Tbody>
							{rows.map((c) => (
								<Tr key={c.component}>
									<Td dataLabel="Component">{c.component}</Td>
									<Td dataLabel="Builds">{c.attempts}</Td>
									<Td dataLabel="Failed">
										{c.failed > 0 ? (
											<Label color="red" isCompact>
												{c.failed}
											</Label>
										) : (
											0
										)}
									</Td>
									<Td dataLabel="Retries">{c.retries}</Td>
									<Td dataLabel="Last successful build">
										{c.lastSuccess ? (
											<a
												href={c.lastSuccess.build_url}
												target="_blank"
												rel="noopener noreferrer"
											>
												{relative(c.lastSuccess.started_at)}
											</a>
										) : (
											`unknown (none since ${since})`
										)}
									</Td>
								</Tr>
							))}
						</Tbody>
					</Table>
				)}
			</CardBody>
		</Card>
	);
}
