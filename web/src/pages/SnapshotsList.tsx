import {
	Breadcrumb,
	BreadcrumbItem,
	EmptyState,
	EmptyStateBody,
	PageSection,
	Pagination,
	Spinner,
	Title,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getRelease, listSnapshots } from "../api/client";
import type { SnapshotRecord } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName } from "../utils/links";

const PAGE_SIZE = 50;

export default function SnapshotsList() {
	const { version } = useParams<{ version: string }>();
	const [snapshots, setSnapshots] = useState<SnapshotRecord[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [page, setPage] = useState(1);
	const [hasMore, setHasMore] = useState(false);

	const { data: release } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);

	const displayName = version ? formatReleaseName(version) : "";

	const fetchPage = useCallback(
		(p: number) => {
			if (!release?.konflux_application) return;
			setLoading(true);
			setError(null);
			listSnapshots(
				release.konflux_application,
				PAGE_SIZE + 1,
				(p - 1) * PAGE_SIZE,
			)
				.then((data) => {
					const rows = data ?? [];
					if (rows.length > PAGE_SIZE) {
						setHasMore(true);
						setSnapshots(rows.slice(0, PAGE_SIZE));
					} else {
						setHasMore(false);
						setSnapshots(rows);
					}
				})
				.catch((err) => {
					setError(
						err instanceof Error ? err.message : "Failed to load snapshots",
					);
				})
				.finally(() => setLoading(false));
		},
		[release?.konflux_application],
	);

	useEffect(() => {
		if (release?.konflux_application) {
			fetchPage(1);
		}
	}, [release?.konflux_application, fetchPage]);

	const onSetPage = (_: unknown, p: number) => {
		setPage(p);
		fetchPage(p);
	};

	return (
		<>
			<PageSection>
				<Breadcrumb>
					<BreadcrumbItem>
						<Link to="/">Releases</Link>
					</BreadcrumbItem>
					{version && (
						<BreadcrumbItem>
							<Link to={`/releases/${encodeURIComponent(version)}`}>
								{displayName}
							</Link>
						</BreadcrumbItem>
					)}
					<BreadcrumbItem isActive>Snapshots</BreadcrumbItem>
				</Breadcrumb>
			</PageSection>

			<PageSection>
				<Title headingLevel="h1" style={{ marginBottom: "1rem" }}>
					Snapshots{version ? ` - ${displayName}` : ""}
				</Title>

				{loading ? (
					<div style={{ textAlign: "center" }}>
						<Spinner />
					</div>
				) : error ? (
					<EmptyState>
						<Title headingLevel="h2" size="lg">
							Error loading snapshots
						</Title>
						<EmptyStateBody>{error}</EmptyStateBody>
					</EmptyState>
				) : snapshots.length === 0 ? (
					<EmptyState>
						<Title headingLevel="h2" size="lg">
							No snapshots
						</Title>
						<EmptyStateBody>
							No snapshots found for this release.
						</EmptyStateBody>
					</EmptyState>
				) : (
					<>
						<Table variant="compact">
							<Thead>
								<Tr>
									<Th>Snapshot</Th>
									<Th>Application</Th>
									<Th>Created</Th>
								</Tr>
							</Thead>
							<Tbody>
								{snapshots.map((s) => (
									<Tr key={s.id}>
										<Td>{s.name}</Td>
										<Td>{s.application}</Td>
										<Td>{new Date(s.created_at).toLocaleString()}</Td>
									</Tr>
								))}
							</Tbody>
						</Table>
						<Pagination
							itemCount={
								hasMore
									? page * PAGE_SIZE + 1
									: (page - 1) * PAGE_SIZE + snapshots.length
							}
							perPage={PAGE_SIZE}
							page={page}
							onSetPage={onSetPage}
							isCompact
							style={{ marginTop: "1rem" }}
						/>
					</>
				)}
			</PageSection>
		</>
	);
}
