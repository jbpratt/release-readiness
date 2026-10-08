import {
	EmptyState,
	EmptyStateBody,
	Label,
	PageSection,
	Pagination,
	SearchInput,
	Spinner,
	Title,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { listKonfluxReleases } from "../api/client";
import type { KonfluxRelease } from "../api/types";

const PAGE_SIZE = 50;

type LabelColor = "green" | "red" | "yellow" | "blue";

const releasedColors: Record<string, LabelColor> = {
	True: "green",
	False: "red",
	Unknown: "yellow",
};

const reasonColors: Record<string, LabelColor> = {
	Succeeded: "green",
	Failed: "red",
	Progressing: "blue",
};

function ReleasedLabel({ release }: { release: KonfluxRelease }) {
	if (!release.released_status) return <>-</>;
	return (
		<Label
			color={
				reasonColors[release.released_reason] ??
				releasedColors[release.released_status] ??
				"grey"
			}
			isCompact
		>
			{release.released_reason || release.released_status}
		</Label>
	);
}

function formatTime(t?: string): string {
	return t ? new Date(t).toLocaleString() : "-";
}

export default function KonfluxReleases() {
	const [searchParams, setSearchParams] = useSearchParams();
	const application = searchParams.get("application") ?? "";
	const [filter, setFilter] = useState(application);
	const [prevApplication, setPrevApplication] = useState(application);
	const [releases, setReleases] = useState<KonfluxRelease[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [page, setPage] = useState(1);
	const [hasMore, setHasMore] = useState(false);

	// Sync the input when the URL changes, e.g. on back/forward.
	if (application !== prevApplication) {
		setPrevApplication(application);
		setFilter(application);
		setPage(1);
	}

	useEffect(() => {
		let ignore = false;
		setLoading(true);
		setError(null);
		listKonfluxReleases(
			application || undefined,
			PAGE_SIZE + 1,
			(page - 1) * PAGE_SIZE,
		)
			.then((data) => {
				if (ignore) return;
				const rows = data ?? [];
				setHasMore(rows.length > PAGE_SIZE);
				setReleases(rows.slice(0, PAGE_SIZE));
			})
			.catch((err) => {
				if (ignore) return;
				setError(
					err instanceof Error ? err.message : "Failed to load releases",
				);
			})
			.finally(() => {
				if (!ignore) setLoading(false);
			});
		return () => {
			ignore = true;
		};
	}, [application, page]);

	useEffect(() => {
		if (filter === application) return;
		const id = setTimeout(
			() =>
				setSearchParams(filter ? { application: filter } : {}, {
					replace: true,
				}),
			300,
		);
		return () => clearTimeout(id);
	}, [filter, application, setSearchParams]);

	return (
		<PageSection>
			<Title headingLevel="h1" style={{ marginBottom: "1rem" }}>
				Konflux Releases
			</Title>

			<Toolbar>
				<ToolbarContent>
					<ToolbarItem>
						<SearchInput
							placeholder="Filter by application"
							value={filter}
							onChange={(_, v) => setFilter(v)}
							onClear={() => setFilter("")}
						/>
					</ToolbarItem>
				</ToolbarContent>
			</Toolbar>

			{loading ? (
				<div style={{ textAlign: "center" }}>
					<Spinner />
				</div>
			) : error ? (
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						Error loading releases
					</Title>
					<EmptyStateBody>{error}</EmptyStateBody>
				</EmptyState>
			) : releases.length === 0 ? (
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						No releases
					</Title>
					<EmptyStateBody>No Konflux Releases found.</EmptyStateBody>
				</EmptyState>
			) : (
				<>
					<Table variant="compact">
						<Thead>
							<Tr>
								<Th>Name</Th>
								<Th>Application</Th>
								<Th>Snapshot</Th>
								<Th>Release plan</Th>
								<Th>Released</Th>
								<Th>Created</Th>
								<Th>Completed</Th>
							</Tr>
						</Thead>
						<Tbody>
							{releases.map((r) => (
								<Tr key={r.id}>
									<Td>{r.name}</Td>
									<Td>{r.application || "-"}</Td>
									<Td>{r.snapshot}</Td>
									<Td>{r.release_plan}</Td>
									<Td>
										<ReleasedLabel release={r} />
									</Td>
									<Td>{formatTime(r.created_at)}</Td>
									<Td>{formatTime(r.completion_time)}</Td>
								</Tr>
							))}
						</Tbody>
					</Table>
					<Pagination
						itemCount={
							hasMore
								? page * PAGE_SIZE + 1
								: (page - 1) * PAGE_SIZE + releases.length
						}
						perPage={PAGE_SIZE}
						page={page}
						onSetPage={(_, p) => setPage(p)}
						isCompact
						style={{ marginTop: "1rem" }}
					/>
				</>
			)}
		</PageSection>
	);
}
