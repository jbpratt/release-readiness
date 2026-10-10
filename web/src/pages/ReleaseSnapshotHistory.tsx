import {
	Breadcrumb,
	BreadcrumbItem,
	EmptyState,
	EmptyStateBody,
	HelperText,
	HelperTextItem,
	PageSection,
	Spinner,
	Title,
} from "@patternfly/react-core";
import { Link, useParams } from "react-router-dom";
import { getRelease } from "../api/client";
import BuildAttempts from "../components/BuildAttempts";
import { UnlinkedProwRuns } from "../components/ProwRuns";
import {
	LatestSnapshot,
	SnapshotHistory,
	useLatestSnapshot,
} from "../components/ReleaseSnapshots";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName, minorVersion } from "../utils/links";

/** The Builds page: the stream cards above the snapshot history and CI runs. */
export default function ReleaseSnapshotHistory() {
	const { version } = useParams<{ version: string }>();
	const { data: release, loading } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const latest = useLatestSnapshot(version!, release);

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

	const app = release.konflux_application;
	const minor = minorVersion(release.name);

	return (
		<PageSection>
			<Breadcrumb style={{ marginBottom: "1rem" }}>
				<BreadcrumbItem>
					<Link to="/">Releases</Link>
				</BreadcrumbItem>
				<BreadcrumbItem>
					<Link to={`/releases/${encodeURIComponent(version!)}`}>
						{formatReleaseName(release.name)}
					</Link>
				</BreadcrumbItem>
				<BreadcrumbItem isActive>Builds</BreadcrumbItem>
			</Breadcrumb>
			<Title headingLevel="h1">Builds of {app || "this release"}</Title>
			<HelperText style={{ marginBottom: "1rem" }}>
				<HelperTextItem>
					The stream: every {minor}.z build, the same for each {minor} version.
					Snapshots, ART image builds and CI runs.
				</HelperTextItem>
			</HelperText>
			<LatestSnapshot version={version!} state={latest} />
			<BuildAttempts version={version!} />
			<SnapshotHistory version={version!} konfluxApp={app} />
			<UnlinkedProwRuns version={version!} />
		</PageSection>
	);
}
