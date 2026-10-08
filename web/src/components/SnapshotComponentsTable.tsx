import { Tooltip } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type { ArtBuild, SnapshotImage } from "../api/types";
import { quayManifestUrl, upstreamCommitUrl } from "../utils/links";
import GitShaLink from "./GitShaLink";

const external = { target: "_blank", rel: "noopener noreferrer" };

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
		<a href={url} {...external}>
			<code style={{ fontSize: "0.85em" }} title={art.upstream_sha}>
				{art.upstream_sha.substring(0, 12)}
			</code>
		</a>
	);
}

/** The image's ART build record; that page links its logs and pipeline run. */
function ArtBuildLink({ art }: { art: ArtBuild | null }) {
	if (!art) return <NoArtRecord />;
	return (
		<a href={art.build_url} {...external}>
			ART build
		</a>
	);
}

/** Digest-pinned image reference, linked to its quay.io manifest page. */
function ImageDigestLink({ image }: { image: string }) {
	const url = quayManifestUrl(image);
	const digest = image.split("@")[1];
	const text = (
		<code style={{ fontSize: "0.85em" }} title={image}>
			{digest ? digest.replace(/^(sha256:.{12}).*/, "$1") : image}
		</code>
	);
	return url ? (
		<a href={url} target="_blank" rel="noopener noreferrer">
			{text}
		</a>
	) : (
		text
	);
}

/** The component images exactly as one Snapshot records them. */
export default function SnapshotComponentsTable({
	components,
}: {
	components: SnapshotImage[];
}) {
	return (
		<Table variant="compact" aria-label="Snapshot components">
			<Thead>
				<Tr>
					<Th width={30}>Component</Th>
					<Th width={20}>Image</Th>
					<Th width={15}>Upstream</Th>
					<Th width={15}>ART</Th>
					<Th width={20}>Build repo</Th>
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
						</Td>
						<Td>
							<GitShaLink sha={c.git_sha} gitUrl={c.git_url} />
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}
