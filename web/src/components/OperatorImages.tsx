import {
	Card,
	CardBody,
	CardTitle,
	Flex,
	Gallery,
	Label,
} from "@patternfly/react-core";
import type { ReactNode } from "react";
import type {
	OperatorFBC,
	OperatorImage,
	ReleaseCandidate,
} from "../api/types";
import {
	bundleBuilt,
	candidateOperators,
	type Flag,
	fixableLabels,
	nvrLabel,
	releaseImages,
	SEVERITIES,
	scanCell,
	worst,
} from "../utils/releaseDetail";
import { Status } from "./Status";

export const external = { target: "_blank", rel: "noopener noreferrer" };
export const muted = { color: "var(--pf-t--global--text--color--subtle)" };

/** A link opening in a new tab, or plain text without an href. */
export function ExtLink({
	href,
	title,
	children,
}: {
	href: string | null;
	title?: string;
	children: ReactNode;
}) {
	return href ? (
		<a href={href} title={title} {...external}>
			{children}
		</a>
	) : (
		<span title={title}>{children}</span>
	);
}

/** An FBC row's flags, otherBundle's linking its FBC's build. */
export function Flags({ flags }: { flags: Flag[] }) {
	return flags.map((f) => (
		<Label
			key={f.text}
			isCompact
			color={f.color}
			render={
				f.href
					? ({ className, content, componentRef }) => (
							<a
								className={className}
								href={f.href ?? undefined}
								ref={componentRef}
								title={f.title}
								{...external}
							>
								{content}
							</a>
						)
					: undefined
			}
		>
			<span title={f.title}>{f.text}</span>
		</Label>
	));
}

/** When ART built an image, linking its build; external ones are labeled. */
export function BuiltCell({ image: i }: { image: OperatorImage }) {
	return i.nvr ? (
		<ExtLink href={i.build_url || null} title={i.nvr}>
			{bundleBuilt(i.nvr) || nvrLabel(i.nvr)}
		</ExtLink>
	) : (
		<span style={muted}>not built by ART</span>
	);
}

/** An ART image's fixable CVEs by severity, or its scan's state. */
export function CveCell({ image: i }: { image: OperatorImage }) {
	if (!i.nvr) return <span style={muted}>–</span>;
	const counts = i.scan?.counts;
	if (!counts) return <span style={muted}>{scanCell(i.scan).text}</span>;
	const labels = fixableLabels(counts);
	const noFix = SEVERITIES.filter((s) => counts.no_fix[s] > 0)
		.map((s) => `${counts.no_fix[s]} ${s}`)
		.join(", ");
	const title = `No fix: ${noFix || "none"}`;
	return labels.length === 0 ? (
		<span style={muted} title={title}>
			none fixable
		</span>
	) : (
		labels.map((l) => (
			<Label key={l.text} isCompact color={l.color} title={title}>
				{l.text}
			</Label>
		))
	);
}

/** A card per operator shown, its head bundle's images, problems first. */
export function OperatorImages({
	d,
	version,
}: {
	d: ReleaseCandidate;
	/** The version's X.Y.Z. */
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

const divider = "1px solid var(--pf-t--global--border--color--default)";

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
						style={{ padding: "0.375rem 0", borderTop: divider }}
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
							borderTop: divider,
						}}
					>
						Not built by ART: {others.map((r) => r.label).join(", ")}
					</div>
				)}
			</CardBody>
		</Card>
	);
}
