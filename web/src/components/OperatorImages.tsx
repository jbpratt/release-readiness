import {
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	ExpandableSection,
	ExpandableSectionToggle,
	Flex,
	Icon,
	Label,
	type LabelProps,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
	InfoCircleIcon,
} from "@patternfly/react-icons";
import { type ReactNode, useEffect, useState } from "react";
import type {
	KonfluxRelease,
	OperatorFBC,
	OperatorImage,
	VerdictItem,
} from "../api/types";
import type { Blocker } from "../utils/blockers";
import { SHOW_CVES } from "../utils/features";
import { commitUrl } from "../utils/links";
import {
	bundleBuilt,
	type Flag,
	fixableLabels,
	nvrLabel,
	type ReleaseImage,
	releaseImages,
	SEVERITIES,
	scanCell,
	stamp,
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

// Phone width, where the matrix turns on its side.
const narrowQuery = "(max-width: 768px)";

export function useNarrow() {
	const [narrow, setNarrow] = useState(() => matchMedia(narrowQuery).matches);
	useEffect(() => {
		const m = matchMedia(narrowQuery);
		const on = () => setNarrow(m.matches);
		m.addEventListener("change", on);
		return () => m.removeEventListener("change", on);
	}, []);
	return narrow;
}

const small = { fontSize: "var(--pf-t--global--font--size--sm)" };

const advisoryName = (url: string) =>
	url.match(/[A-Z]+-\d{4}:\d+$/)?.[0] ?? "Advisory";

/** The stage Release and advisory, as release facts; the Release's name in its title. */
export function StageFacts({ ri }: { ri: KonfluxRelease }) {
	return (
		<>
			<DescriptionListGroup>
				<DescriptionListTerm>Stage release</DescriptionListTerm>
				<DescriptionListDescription>
					<ExtLink href={ri.job_url || null} title={ri.name} icon>
						{stamp(ri.name)}
					</ExtLink>
				</DescriptionListDescription>
			</DescriptionListGroup>
			{ri.advisory && (
				<DescriptionListGroup>
					<DescriptionListTerm>Advisory</DescriptionListTerm>
					<DescriptionListDescription>
						<ExtLink href={ri.advisory} icon>
							{advisoryName(ri.advisory)}
						</ExtLink>
					</DescriptionListDescription>
				</DescriptionListGroup>
			)}
		</>
	);
}

const icons = ["failed", "warning", undefined, "done"] as const;

const tones = ["danger", "warning", "info", "success"] as const;

const toneIcons = {
	danger: <ExclamationCircleIcon />,
	warning: <ExclamationTriangleIcon />,
	info: <InfoCircleIcon />,
	success: <CheckCircleIcon />,
};

/** An operator's images worst first, its issues worst first, and its one tone and words. */
function summarize(o: OperatorFBC, version: string, issues: Issue[]) {
	const images = releaseImages([o], version);
	const art = images
		.filter((r) => r.image.nvr || r.issues.length > 0)
		.sort((a, b) => worst(a) - worst(b));
	const bad = art.filter((r) => worst(r) < 2);
	const sorted = [...issues].sort(
		(a, b) => ORDER.indexOf(a.item.level) - ORDER.indexOf(b.item.level),
	);
	const top = sorted[0];
	// The worse of its images' and its issues' levels: an image's 0 failed and 1
	// warning, an issue's 0 blocker, 1 warning and 2 info.
	const rank = Math.min(
		...bad.map(worst),
		top ? ORDER.indexOf(top.item.level) : 3,
	);
	const n = (k: number) => `${k} image${k === 1 ? "" : "s"}`;
	const said = top
		? `${LEVELS[top.item.level].word}: ${top.b.what}${bad.length ? `; ${bad.length} of ${n(art.length)} to check` : ""}`
		: bad.length
			? `${bad.length} of ${n(art.length)} to check`
			: `${n(art.length)} built`;
	return {
		images,
		bad,
		good: art.filter((r) => worst(r) >= 2),
		others: images.filter((r) => !art.includes(r)),
		sorted,
		tone: tones[rank],
		said,
	};
}

const sectionId = (operator: string, what: string) => `fbc-${operator}-${what}`;

/**
 * An operator's name as the toggle of its images, with its tone's icon: its
 * worst issue, else its images to check, else how many ART built, in the
 * toggle's tooltip and name.
 */
export function OperatorToggle({
	o,
	version,
	issues,
	open,
	onToggle,
}: {
	o: OperatorFBC;
	/** The version's X.Y.Z. */
	version: string;
	/** The verdict items about the operator. */
	issues: Issue[];
	open: boolean;
	onToggle: (open: boolean) => void;
}) {
	const { tone, said } = summarize(o, version, issues);
	return (
		<>
			<ExpandableSectionToggle
				isExpanded={open}
				onToggle={onToggle}
				contentId={sectionId(o.operator, "images")}
				toggleId={sectionId(o.operator, "toggle")}
				toggleAriaLabel={`${o.operator} images: ${said}`}
				direction="down"
			>
				<span
					style={{
						display: "inline-flex",
						gap: "0.5rem",
						alignItems: "center",
						fontWeight: "var(--pf-t--global--font--weight--body--bold)",
					}}
				>
					{o.operator}
					<Icon status={tone} isInline>
						{toneIcons[tone]}
					</Icon>
				</span>
			</ExpandableSectionToggle>
			<Tooltip
				content={said}
				aria="none"
				aria-live="off"
				triggerRef={() =>
					document.getElementById(
						sectionId(o.operator, "toggle"),
					) as HTMLElement
				}
			/>
		</>
	);
}

/**
 * An operator's images under its matrix row, its toggle OperatorToggle: its
 * own stage Release when operators do not share one, its issues, the images
 * with a problem, then the healthy ones folded behind a count.
 */
export function OperatorPanel({
	o,
	version,
	issues,
	own,
	open,
}: {
	o: OperatorFBC;
	/** The version's X.Y.Z. */
	version: string;
	/** The verdict items about the operator. */
	issues: Issue[];
	own: boolean;
	open: boolean;
}) {
	const s = summarize(o, version, issues);
	const [healthy, setHealthy] = useState(false);
	const ri = o.fbc_ri_stage.release;
	return (
		<ExpandableSection
			isDetached
			isExpanded={open}
			contentId={sectionId(o.operator, "images")}
			toggleId={sectionId(o.operator, "toggle")}
		>
			<div
				style={{
					display: "grid",
					gap: "0.75rem",
					padding: "0.25rem 0 1rem 2rem",
				}}
			>
				{own && ri && (
					<div style={{ ...small, overflowWrap: "anywhere" }}>
						<span style={muted}>Stage release </span>
						<ExtLink href={ri.job_url || null} title={ri.name} icon>
							{stamp(ri.name)}
						</ExtLink>
						{ri.advisory && (
							<>
								<span style={muted}> · Advisory </span>
								<ExtLink href={ri.advisory} icon>
									{advisoryName(ri.advisory)}
								</ExtLink>
							</>
						)}
					</div>
				)}
				{s.sorted.length > 0 && <IssueNotes issues={s.sorted} />}
				{s.bad.length === 0 ? (
					<ImageLines art={s.good} />
				) : (
					<>
						<ImageLines art={s.bad} />
						{s.good.length > 0 && (
							<ExpandableSection
								isExpanded={healthy}
								onToggle={(_e, v) => setHealthy(v)}
								toggleContent={
									<Status status="done">{`${s.good.length} more built`}</Status>
								}
							>
								<ImageLines art={s.good} />
							</ExpandableSection>
						)}
					</>
				)}
				{s.others.length > 0 && (
					<div style={{ ...muted, ...small }}>
						Not built by ART: {s.others.map((r) => r.label).join(", ")}
					</div>
				)}
				{s.images.length === 0 && (
					<div style={{ ...muted, ...small }}>No images rendered yet</div>
				)}
			</div>
		</ExpandableSection>
	);
}

/** One line per image: status and name, when ART built it, its commit, flags. */
function ImageLines({ art }: { art: ReleaseImage[] }) {
	const narrow = useNarrow();
	return (
		<div
			style={
				narrow
					? { display: "grid", gap: "0.5rem" }
					: {
							display: "grid",
							gridTemplateColumns: "repeat(3, max-content) auto",
							justifyContent: "start",
							gap: "0.375rem 1rem",
							alignItems: "baseline",
						}
			}
		>
			{art.map((r) => (
				<ImageLine key={r.image.digest} r={r} narrow={narrow} />
			))}
		</div>
	);
}

function ImageLine({ r, narrow }: { r: ReleaseImage; narrow: boolean }) {
	const icon = icons[worst(r)];
	const href = commitUrl(r.label, r.image.commit);
	const name = icon ? (
		<Status status={icon}>{r.label}</Status>
	) : (
		<span>{r.label}</span>
	);
	const commit = (
		<ExtLink href={href} title={r.image.commit}>
			{/* muted only when unlinked, so a link takes the link colour */}
			<code style={href ? undefined : muted}>{r.image.commit.slice(0, 7)}</code>
		</ExtLink>
	);
	const flags = (
		<span>
			{SHOW_CVES && <CveCell image={r.image} />}
			<Flags flags={r.issues} />
		</span>
	);
	return narrow ? (
		<div style={{ overflowWrap: "anywhere" }}>
			{name}
			<Flex
				gap={{ default: "gapSm" }}
				alignItems={{ default: "alignItemsCenter" }}
				style={{ ...small, marginInlineStart: "1.5rem" }}
			>
				<BuiltCell image={r.image} />
				{commit}
				{flags}
			</Flex>
		</div>
	) : (
		<div style={{ display: "contents" }}>
			{name}
			<span style={small}>
				<BuiltCell image={r.image} />
			</span>
			<span style={small}>{commit}</span>
			{flags}
		</div>
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
function IssueNote({ issue }: { issue: Issue }) {
	const { item, b } = issue;
	return (
		<div style={{ display: "grid", gap: "0.125rem" }}>
			<div>
				<Label isCompact status={LEVELS[item.level].status}>
					{LEVELS[item.level].word}
				</Label>{" "}
				<strong>{b.what}</strong>
				{b.why && ` · ${b.why}`}
			</div>
			<IssueBody issue={issue} />
		</div>
	);
}

/** An issue's detail, owner, next step and links out, under its title. */
function IssueBody({ issue: { item, b } }: { issue: Issue }) {
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
	return (
		<>
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
		</>
	);
}
