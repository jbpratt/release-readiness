import { Tooltip } from "@patternfly/react-core";
import { ExternalLinkAltIcon, InProgressIcon } from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type {
	ArtBuild,
	PendingArtBuild,
	SnapshotImage,
	SnapshotProwRuns,
} from "../api/types";
import { quayManifestUrl, upstreamCommitUrl } from "../utils/links";
import GitShaLink from "./GitShaLink";
import { ProwRunBadge } from "./ProwRuns";

const external = { target: "_blank", rel: "noopener noreferrer" };

const linkIcon = (
	<ExternalLinkAltIcon style={{ fontSize: "0.75em", marginLeft: "0.25rem" }} />
);

function NoArtRecord() {
	return (
		<Tooltip content="No ART build record found">
			<span>-</span>
		</Tooltip>
	);
}

/** Upstream commit the image was built from, per its ART build record. */
function UpstreamCommitLink({ art }: { art: ArtBuild | null }) {
	if (!art) return <NoArtRecord />;
	const url = upstreamCommitUrl(art.upstream_repo, art.upstream_sha);
	if (!url) return <>-</>;
	return (
		<Tooltip content={`${art.upstream_repo} ${art.upstream_sha}`}>
			<a href={url} {...external}>
				<code style={{ fontSize: "0.85em" }}>
					{art.upstream_sha.substring(0, 12)}
				</code>
			</a>
		</Tooltip>
	);
}

/** The image's ART build record; that page links its logs and pipeline run. */
function ArtBuildLink({ art }: { art: ArtBuild | null }) {
	if (!art) return <NoArtRecord />;
	return (
		<Tooltip content="Opens ART build history: the build record, its logs and pipeline run">
			<a href={art.build_url} {...external}>
				ART build{linkIcon}
			</a>
		</Tooltip>
	);
}

/** A newer ART build of the component, not yet in this Snapshot. */
function PendingArtBuildLink({ pending }: { pending: PendingArtBuild | null }) {
	if (!pending) return null;
	return (
		<div style={{ fontSize: "0.85em" }}>
			<Tooltip
				content={`Started ${new Date(pending.started_at).toLocaleString()}; opens its ART build record`}
			>
				<a href={pending.build_url} {...external}>
					<InProgressIcon style={{ marginRight: "0.25rem" }} />
					Newer build in progress from{" "}
					<code>{pending.upstream_sha.substring(0, 12)}</code>
					{linkIcon}
				</a>
			</Tooltip>
		</div>
	);
}

/** Digest-pinned image reference, linked to its quay.io manifest page. */
function ImageDigestLink({ image }: { image: string }) {
	const url = quayManifestUrl(image);
	const digest = image.split("@")[1];
	const text = (
		<code style={{ fontSize: "0.85em" }}>
			{digest ? digest.replace(/^(sha256:.{12}).*/, "$1") : image}
		</code>
	);
	return (
		<Tooltip content={url ? `Opens on quay.io: ${image}` : image}>
			{url ? (
				<a href={url} {...external}>
					{text}
					{linkIcon}
				</a>
			) : (
				text
			)}
		</Tooltip>
	);
}

/** The component images exactly as one Snapshot records them. */
export default function SnapshotComponentsTable({
	components,
	prowRuns,
}: {
	components: SnapshotImage[];
	/** Shown only once a CI job of the release has synced. */
	prowRuns?: SnapshotProwRuns;
}) {
	const ci = prowRuns?.last_successful_sync ? prowRuns : undefined;
	return (
		<Table variant="compact" aria-label="Snapshot components">
			<Thead>
				<Tr>
					<Th width={30}>Component</Th>
					<Th width={20}>Image</Th>
					<Th
						width={15}
						modifier="nowrap"
						info={{ tooltip: "Commit in the public upstream repo" }}
					>
						Upstream
					</Th>
					<Th width={15}>ART</Th>
					<Th
						width={20}
						modifier="nowrap"
						info={{
							tooltip:
								"Commit in ART's openshift-priv rebase fork that was actually built; mirrors Upstream",
						}}
					>
						Build repo
					</Th>
					{ci && <Th modifier="fitContent">Periodic CI</Th>}
				</Tr>
			</Thead>
			<Tbody>
				{components.map((c) => (
					<Tr key={c.name}>
						<Td>{c.name}</Td>
						<Td>
							<ImageDigestLink image={c.image} />
						</Td>
						<Td>
							<UpstreamCommitLink art={c.art} />
						</Td>
						<Td>
							<ArtBuildLink art={c.art} />
							<PendingArtBuildLink pending={c.pending_art_build} />
						</Td>
						<Td>
							<GitShaLink sha={c.git_sha} gitUrl={c.git_url} />
						</Td>
						{ci && (
							<Td modifier="fitContent">
								<ProwRunBadge
									component={c.name}
									image={c.image}
									runs={ci.components[c.name] ?? []}
									sync={ci}
								/>
							</Td>
						)}
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}
