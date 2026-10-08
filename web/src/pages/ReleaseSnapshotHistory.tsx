import {
	Breadcrumb,
	BreadcrumbItem,
	EmptyState,
	EmptyStateBody,
	PageSection,
	Spinner,
	Title,
} from "@patternfly/react-core";
import { Link, useParams } from "react-router-dom";
import { getRelease } from "../api/client";
import { UnlinkedProwRuns } from "../components/ProwRuns";
import { SnapshotHistory } from "../components/ReleaseSnapshots";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName } from "../utils/links";

export default function ReleaseSnapshotHistory() {
	const { version } = useParams<{ version: string }>();
	const { data: release, loading } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);

	if (loading && !release) {
		return (
			<PageSection>
				<div style={{ textAlign: "center" }}>
					<Spinner />
				</div>
			</PageSection>
		);
	}

	if (!release) {
		return (
			<PageSection>
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						Release not found
					</Title>
					<EmptyStateBody>
						No data found for release &quot;{version}&quot;.
					</EmptyStateBody>
				</EmptyState>
			</PageSection>
		);
	}

	const displayName = formatReleaseName(release.name);

	return (
		<>
			<PageSection>
				<Breadcrumb>
					<BreadcrumbItem>
						<Link to="/">Releases</Link>
					</BreadcrumbItem>
					<BreadcrumbItem>
						<Link to={`/releases/${encodeURIComponent(version!)}`}>
							{displayName}
						</Link>
					</BreadcrumbItem>
					<BreadcrumbItem isActive>Snapshot history</BreadcrumbItem>
				</Breadcrumb>
			</PageSection>

			<PageSection>
				<Title headingLevel="h1" style={{ marginBottom: "1rem" }}>
					{displayName} snapshot history
				</Title>
				<SnapshotHistory
					version={version!}
					konfluxApp={release.konflux_application}
				/>
				<UnlinkedProwRuns version={version!} />
			</PageSection>
		</>
	);
}
