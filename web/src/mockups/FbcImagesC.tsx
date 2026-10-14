import {
	Button,
	DrawerActions,
	DrawerCloseButton,
	DrawerHead,
	DrawerPanelBody,
	Flex,
	Label,
	type LabelProps,
	Title,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useSearchParams } from "react-router-dom";
import type {
	ImageCompareRow,
	ImageRef,
	OperatorFBC,
	ReleaseCandidate,
} from "../api/types";
import { Flags, muted } from "../components/OperatorImages";
import {
	bundleBuilt,
	candidateOperators,
	nvrLabel,
	type ReleaseImage,
	releaseImages,
	worst,
} from "../utils/releaseDetail";
import { FbcImagesPage } from "./fbcImages";

/**
 * Design C: an images summary on each FBC row holding its operator's head
 * bundle, opening the bundle's images against the previous release in a
 * drawer (?images=<operator>).
 */
export default function FbcImagesC() {
	const [params, setParams] = useSearchParams();
	const shown = params.get("images");
	const show = (operator: string | null) =>
		setParams(
			(prev) => {
				const next = new URLSearchParams(prev);
				if (operator) next.set("images", operator);
				else next.delete("images");
				return next;
			},
			{ replace: true },
		);
	const find = (d: ReleaseCandidate, operator: string) =>
		candidateOperators(d).find((o) => o.operator === operator);
	return (
		<FbcImagesPage
			ui={{
				cell: (r, d, version) => {
					const o = r.images.length > 0 && find(d, r.operator);
					return o ? (
						<ImagesSummary
							images={releaseImages([o], version)}
							onOpen={() => show(o.operator)}
						/>
					) : (
						<span style={muted}>–</span>
					);
				},
				panel: (d, version) => {
					const o = shown && find(d, shown);
					return (
						o && (
							<ImagesPanel o={o} version={version} onClose={() => show(null)} />
						)
					);
				},
			}}
		/>
	);
}

/** How many images, and the worst issue's count, opening them. */
function ImagesSummary({
	images,
	onOpen,
}: {
	images: ReleaseImage[];
	onOpen: () => void;
}) {
	const red = images.filter((r) => worst(r) === 0).length;
	const orange = images.filter((r) => worst(r) === 1).length;
	return (
		<Flex
			gap={{ default: "gapSm" }}
			alignItems={{ default: "alignItemsCenter" }}
			flexWrap={{ default: "nowrap" }}
		>
			<Button variant="link" isInline onClick={onOpen}>
				{images.length} images
			</Button>
			{red > 0 && (
				<Label isCompact color="red">
					{red} to fix
				</Label>
			)}
			{orange > 0 && (
				<Label isCompact color="orange">
					{orange} to check
				</Label>
			)}
		</Flex>
	);
}

const changeColors: Record<ImageCompareRow["change"], LabelProps["color"]> = {
	same: "grey",
	rebuild: "teal",
	"new source": "blue",
	"new build": "blue",
	added: "green",
	removed: "grey",
	renamed: "orange",
};

/** When ART built an image, by its NVR; "no NVR" when unresolved or not ART's. */
function built(ref: ImageRef | null) {
	if (!ref) return <span style={muted}>–</span>;
	if (!ref.nvr)
		return (
			<span style={muted} title={ref.repository}>
				no NVR
			</span>
		);
	return (
		<span title={ref.nvr}>{bundleBuilt(ref.nvr) || nvrLabel(ref.nvr)}</span>
	);
}

/** An operator's head bundle images, against each bundle it is compared to. */
function ImagesPanel({
	o,
	version,
	onClose,
}: {
	o: OperatorFBC;
	version: string;
	onClose: () => void;
}) {
	const images = releaseImages([o], version);
	const issues = (ref: ImageRef | null) =>
		images.find((r) => r.image.digest === ref?.digest)?.issues ?? [];
	return (
		<>
			<DrawerHead>
				<Title headingLevel="h2" size="lg">
					{o.operator} images
				</Title>
				<div style={muted}>
					Head bundle built {bundleBuilt(o.head_bundle_nvr) || "unknown"} UTC
				</div>
				<DrawerActions>
					<DrawerCloseButton onClick={onClose} />
				</DrawerActions>
			</DrawerHead>
			<DrawerPanelBody>
				{o.compare.length === 0 && "No previous release to compare with."}
				{o.compare.map((c) => (
					<Table
						key={c.kind}
						variant="compact"
						aria-label={`${o.operator} images against ${c.label}`}
					>
						<Thead>
							<Tr>
								<Th>Image</Th>
								<Th>Change</Th>
								<Th>
									{c.kind === "previous" ? `In ${c.label}` : "This candidate"}
								</Th>
								<Th>
									{c.kind === "previous" ? `In ${version}` : `Newer ${c.label}`}
								</Th>
								<Th>Issues</Th>
							</Tr>
						</Thead>
						<Tbody>
							{c.rows.map((r) => (
								<Tr key={`${r.name} ${r.from?.digest} ${r.to?.digest}`}>
									<Td dataLabel="Image">{r.name || "bundle"}</Td>
									<Td dataLabel="Change">
										<Label isCompact color={changeColors[r.change]}>
											{r.change}
										</Label>
									</Td>
									<Td dataLabel={`In ${c.label}`} modifier="nowrap">
										{built(r.from)}
									</Td>
									<Td dataLabel={`In ${version}`} modifier="nowrap">
										{built(r.to)}
									</Td>
									<Td dataLabel="Issues">
										<Flags flags={issues(r.to)} />
									</Td>
								</Tr>
							))}
						</Tbody>
					</Table>
				))}
			</DrawerPanelBody>
		</>
	);
}
