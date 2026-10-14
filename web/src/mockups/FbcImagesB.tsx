import {
	Card,
	CardBody,
	CardTitle,
	Flex,
	Gallery,
	Label,
} from "@patternfly/react-core";
import type { OperatorFBC, ReleaseCandidate } from "../api/types";
import { Status } from "../components/Status";
import { bundleBuilt } from "../utils/releaseDetail";
import {
	BuiltCell,
	CveCell,
	candidateOperators,
	FbcImagesPage,
	Flags,
	muted,
	releaseImages,
	worst,
} from "./fbcImages";

/** Design B: a panel per operator, its problem images first. */
export default function FbcImagesB() {
	return (
		<FbcImagesPage
			ui={{ card: (d, version) => <OperatorPanels d={d} version={version} /> }}
		/>
	);
}

function OperatorPanels({
	d,
	version,
}: {
	d: ReleaseCandidate;
	version: string;
}) {
	return (
		<Gallery
			hasGutter
			minWidths={{ default: "100%", lg: "24rem" }}
			style={{ marginBottom: "1rem" }}
		>
			{candidateOperators(d).map((o) => (
				<OperatorPanel key={o.operator} o={o} version={version} />
			))}
		</Gallery>
	);
}

const icons = ["failed", "warning", "running", "done"] as const;

/**
 * An operator's head bundle images: ART's by worst issue, then the others
 * on one line.
 */
function OperatorPanel({ o, version }: { o: OperatorFBC; version: string }) {
	const images = releaseImages([o], version);
	const art = images
		.filter((r) => r.image.nvr || r.issues.length > 0)
		.sort((a, b) => worst(a) - worst(b));
	const others = images.filter((r) => !art.includes(r));
	const problems = art.filter((r) => worst(r) < 2).length;
	return (
		<Card isCompact>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<span>{o.operator}</span>
					{problems > 0 ? (
						<Label isCompact color="red">
							{problems} to check
						</Label>
					) : (
						<Label isCompact color="green">
							All {art.length} ART images OK
						</Label>
					)}
				</Flex>
				<div style={{ ...muted, fontSize: "0.85rem", fontWeight: "normal" }}>
					Bundle built {bundleBuilt(o.head_bundle_nvr) || "unknown"} UTC
				</div>
			</CardTitle>
			<CardBody>
				{art.map((r) => (
					<div
						key={r.image.digest}
						style={{
							padding: "0.375rem 0",
							borderTop:
								"1px solid var(--pf-t--global--border--color--default)",
						}}
					>
						<Status status={icons[worst(r)]}>
							<strong>{r.label}</strong>
						</Status>
						<Flex
							gap={{ default: "gapSm" }}
							alignItems={{ default: "alignItemsCenter" }}
							style={{ fontSize: "0.85rem", marginTop: "0.125rem" }}
						>
							<BuiltCell image={r.image} />
							<code style={muted} title={r.image.commit}>
								{r.image.commit.slice(0, 7)}
							</code>
							<CveCell image={r.image} />
							<Flags flags={r.issues} />
						</Flex>
					</div>
				))}
				{others.length > 0 && (
					<div
						style={{
							...muted,
							fontSize: "0.85rem",
							paddingTop: "0.375rem",
							borderTop:
								"1px solid var(--pf-t--global--border--color--default)",
						}}
					>
						Not built by ART: {others.map((r) => r.label).join(", ")}
					</div>
				)}
			</CardBody>
		</Card>
	);
}
