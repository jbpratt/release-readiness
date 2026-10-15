import {
	Card,
	CardBody,
	CardTitle,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	Flex,
	Grid,
	GridItem,
	Icon,
	Label,
	type LabelProps,
	Title,
} from "@patternfly/react-core";
import { ExternalLinkAltIcon } from "@patternfly/react-icons";
import type { ReactNode } from "react";
import type {
	OperatorFBC,
	OperatorImage,
	ReleaseCandidate,
	VerdictItem,
} from "../api/types";
import type { Blocker } from "../utils/blockers";
import { SHOW_CVES } from "../utils/features";
import { commitUrl } from "../utils/links";
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

/**
 * A link opening in a new tab, or plain text without an href; with icon, the
 * external icon kept on its last word as it wraps.
 */
export function ExtLink({
	href,
	title,
	icon,
	children,
}: {
	href: string | null;
	title?: string;
	icon?: boolean;
	children: ReactNode;
}) {
	return href ? (
		<a href={href} title={title} {...external}>
			{children}
			{icon && (
				<>
					{"\u00a0"}
					<Icon isInline>
						<ExternalLinkAltIcon />
					</Icon>
				</>
			)}
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
			status={f.status}
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

/**
 * A card per operator shown, its head bundle's images, problems first, led by
 * the verdict items about the operator.
 */
export function OperatorImages({
	d,
	version,
	issues,
}: {
	d: ReleaseCandidate;
	/** The version's X.Y.Z. */
	version: string;
	/** The verdict items each operator's card shows. */
	issues: (operator: string) => Issue[];
}) {
	const operators = candidateOperators(d);
	if (operators.length === 0) return null;
	return (
		<>
			<Title headingLevel="h2" size="lg" style={{ marginBottom: "0.5rem" }}>
				Images in the candidate's bundles
			</Title>
			<Grid hasGutter md={6} style={{ marginBottom: "1rem" }}>
				{operators.map((o) => (
					<GridItem key={o.operator}>
						<OperatorPanel
							o={o}
							version={version}
							issues={issues(o.operator)}
						/>
					</GridItem>
				))}
			</Grid>
		</>
	);
}

const icons = ["failed", "warning", undefined, "done"] as const;

const divider = "1px solid var(--pf-t--global--border--color--default)";

/**
 * An operator's stage Release and advisory, the values their links; its
 * verdict items; then its head bundle images: ART's by worst issue, then the
 * others on one line.
 */
function OperatorPanel({
	o,
	version,
	issues,
}: {
	o: OperatorFBC;
	version: string;
	issues: Issue[];
}) {
	const images = releaseImages([o], version);
	const art = images
		.filter((r) => r.image.nvr || r.issues.length > 0)
		.sort((a, b) => worst(a) - worst(b));
	const others = images.filter((r) => !art.includes(r));
	const problems = art.filter((r) => worst(r) < 2);
	const ri = o.fbc_ri_stage.release;
	return (
		<Card isCompact isFullHeight>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<span>{o.operator}</span>
					{problems.length > 0 && (
						<Label
							isCompact
							status={
								problems.some((r) => worst(r) === 0) ? "danger" : "warning"
							}
						>
							{problems.length} to check
						</Label>
					)}
				</Flex>
			</CardTitle>
			<CardBody>
				{ri && (
					<DescriptionList
						isCompact
						isHorizontal
						horizontalTermWidthModifier={{ default: "6rem" }}
						style={{
							fontSize: "var(--pf-t--global--font--size--sm)",
							paddingBottom: "0.5rem",
						}}
					>
						<DescriptionListGroup>
							<DescriptionListTerm>Stage release</DescriptionListTerm>
							<DescriptionListDescription style={{ overflowWrap: "anywhere" }}>
								<ExtLink href={ri.job_url || null} title="ART job" icon>
									{ri.name}
								</ExtLink>
							</DescriptionListDescription>
						</DescriptionListGroup>
						{ri.advisory && (
							<DescriptionListGroup>
								<DescriptionListTerm>Advisory</DescriptionListTerm>
								<DescriptionListDescription>
									<ExtLink href={ri.advisory} icon>
										{ri.advisory.match(/[A-Z]+-\d{4}:\d+$/)?.[0] ?? "Advisory"}
									</ExtLink>
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
					</DescriptionList>
				)}
				{issues.length > 0 && (
					<div style={{ paddingBottom: "0.5rem" }}>
						<IssueNotes issues={issues} />
					</div>
				)}
				{art.map((r) => {
					const icon = icons[worst(r)];
					const label = <strong>{r.label}</strong>;
					const href = commitUrl(r.label, r.image.commit);
					return (
						<div
							key={r.image.digest}
							style={{ padding: "0.375rem 0", borderTop: divider }}
						>
							{icon ? <Status status={icon}>{label}</Status> : label}
							<Flex
								gap={{ default: "gapSm" }}
								alignItems={{ default: "alignItemsCenter" }}
								style={{ fontSize: "0.85rem", marginTop: "0.125rem" }}
							>
								<BuiltCell image={r.image} />
								<ExtLink href={href} title={r.image.commit}>
									{/* muted only when unlinked, so a link takes the link colour */}
									<code style={href ? undefined : muted}>
										{r.image.commit.slice(0, 7)}
									</code>
								</ExtLink>
								{SHOW_CVES && <CveCell image={r.image} />}
								<Flags flags={r.issues} />
							</Flex>
						</div>
					);
				})}
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
				{images.length === 0 && (
					<div style={{ ...muted, fontSize: "0.85rem" }}>
						No images rendered yet
					</div>
				)}
			</CardBody>
		</Card>
	);
}

/** A verdict item and its blocker words, shown where it applies. */
export interface Issue {
	item: VerdictItem;
	b: Blocker;
}

export const LEVELS: Record<
	VerdictItem["level"],
	{ status: LabelProps["status"]; word: string }
> = {
	blocker: { status: "danger", word: "Blocker" },
	warning: { status: "warning", word: "Warning" },
	info: { status: "info", word: "Note" },
};

export const ORDER = Object.keys(LEVELS) as VerdictItem["level"][];

/** Issues stacked, the most severe first. */
export function IssueNotes({ issues }: { issues: Issue[] }) {
	const sorted = [...issues].sort(
		(a, b) => ORDER.indexOf(a.item.level) - ORDER.indexOf(b.item.level),
	);
	return (
		<div style={{ display: "grid", gap: "0.5rem" }}>
			{sorted.map((i) => (
				<IssueNote key={`${i.item.code} ${i.item.text}`} issue={i} />
			))}
		</div>
	);
}

/** An issue: its level, what, the detail, owner, next step and links out. */
function IssueNote({ issue: { item, b } }: { issue: Issue }) {
	const prefix =
		item.operator &&
		[`${item.operator}: `, `${item.operator} `].find((p) =>
			item.text.startsWith(p),
		);
	const text = prefix ? item.text.slice(prefix.length) : item.text;
	// "newer build not on stage, 6 images changed" under its title is "6 images changed".
	const detail = text.toLowerCase().startsWith(b.what.toLowerCase())
		? text.slice(b.what.length).replace(/^[,:]?\s*/, "")
		: text;
	const links = b.links.filter((l) => l.external);
	const small = { fontSize: "var(--pf-t--global--font--size--sm)" };
	return (
		<div style={{ display: "grid", gap: "0.125rem" }}>
			<div>
				<Label isCompact status={LEVELS[item.level].status}>
					{LEVELS[item.level].word}
				</Label>{" "}
				<strong>{b.what}</strong>
				{b.why && ` · ${b.why}`}
			</div>
			<div style={{ ...muted, ...small, overflowWrap: "anywhere" }}>
				{[detail, b.owner && `Owner: ${b.owner}`].filter(Boolean).join(" · ")}
			</div>
			{(b.action || links.length > 0) && (
				<div style={small}>
					{b.action && <>Next: {b.action}</>}
					{links.map((l, i) => (
						<span key={l.href}>
							{(b.action || i > 0) && " · "}
							<ExtLink href={l.href}>{l.text}</ExtLink>
						</span>
					))}
				</div>
			)}
		</div>
	);
}
