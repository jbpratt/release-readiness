import { Alert, Flex, Label, Tooltip } from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	InfoCircleIcon,
	LongArrowAltRightIcon,
	OutlinedCircleIcon,
	OutlinedClockIcon,
} from "@patternfly/react-icons";
import { Fragment } from "react";
import type { Mark, Readiness, Stage } from "./readiness";

const terms: Record<string, string> = {
	Build:
		"The images ART built for this version and pushed to stage, its pre-production push that QE tests. Konflux records them as a snapshot.",
	Catalog:
		"The operator's OperatorHub entry, a file-based catalog (FBC). ART stages one per operator and OCP version.",
	Tickets:
		"Jira tickets whose Target Version is this release, plus the .z tickets a build commit names. Release Pending, Verified, Closed and Done count as verified.",
	Shipped: "The Red Hat catalog and Jira's released flag.",
};

const color: Record<Mark, string> = {
	v: "var(--pf-t--global--color--status--success--default)",
	x: "var(--pf-t--global--color--status--danger--default)",
	o: "var(--pf-t--global--border--color--default)",
};

// Light fills for the pipeline's chips.
const tint: Record<Mark, string> = {
	v: "var(--pf-t--global--color--nonstatus--green--default)",
	x: "var(--pf-t--global--color--nonstatus--red--default)",
	o: "var(--pf-t--global--color--nonstatus--gray--default)",
};

const icons: Record<Mark, React.ReactNode> = {
	v: <CheckCircleIcon title="Done" style={{ color: color.v }} />,
	x: <ExclamationCircleIcon title="Blocking" style={{ color: color.x }} />,
	o: (
		<OutlinedCircleIcon
			title="Not done"
			style={{ color: "var(--pf-t--global--icon--color--subtle)" }}
		/>
	),
};

const subtle = {
	color: "var(--pf-t--global--text--color--subtle)",
	fontSize: "var(--pf-t--global--font--size--sm)",
};

/** The stage name, with its plain-language term as a tooltip. */
function Term({ label }: { label: string }) {
	return (
		<Tooltip content={terms[label]}>
			<span style={{ fontWeight: 600, textDecoration: "underline dotted" }}>
				{label}
			</span>
		</Tooltip>
	);
}

const pastDue = (r: Readiness, isCompact = false) =>
	r.pastDue && (
		<Label color="red" isCompact={isCompact} icon={<OutlinedClockIcon />}>
			{r.pastDue}
		</Label>
	);

// The verdict's icon, text color and callout, by its color.
const tones = {
	green: {
		icon: icons.v,
		color: "var(--pf-t--global--text--color--status--success--default)",
		variant: "success",
	},
	red: {
		icon: icons.x,
		color: "var(--pf-t--global--text--color--status--danger--default)",
		variant: "danger",
	},
	blue: {
		icon: (
			<InfoCircleIcon
				style={{
					color: "var(--pf-t--global--icon--color--status--info--default)",
				}}
			/>
		),
		color: "var(--pf-t--global--text--color--status--info--default)",
		variant: "info",
	},
} as const;

/** The verdict as a headline, then the lateness while it applies. */
function Verdict({ r }: { r: Readiness }) {
	const { icon, color } = tones[r.verdict.color];
	return (
		<>
			<span
				style={{
					color,
					fontSize: "var(--pf-t--global--font--size--lg)",
					fontWeight: 600,
				}}
			>
				{icon} {r.verdict.text}
			</span>
			{pastDue(r)}
		</>
	);
}

/** A dot per operator catalog: filled when staged, hollow when not. */
function Dots({ stage }: { stage: Stage }) {
	return stage.operators?.map((op) => (
		<Tooltip key={op.label} content={`${op.label}: ${op.state}`}>
			<span
				style={{
					display: "inline-block",
					width: 10,
					height: 10,
					marginLeft: 4,
					borderRadius: "50%",
					border: `2px solid ${op.mark === "o" ? "var(--pf-t--global--icon--color--subtle)" : color[op.mark]}`,
					background: op.mark === "o" ? "transparent" : color[op.mark],
				}}
			/>
		</Tooltip>
	));
}

/** Stage tiles, left to right: state, then when or why. A blocking tile is outlined red and says what is next. */
export function Strip({ r }: { r: Readiness }) {
	return (
		<>
			<Flex
				gap={{ default: "gapSm" }}
				alignItems={{ default: "alignItemsCenter" }}
				style={{ marginBottom: "0.75rem" }}
			>
				<Verdict r={r} />
			</Flex>
			<div style={{ display: "flex", alignItems: "stretch", gap: "0.5rem" }}>
				{r.stages.map((s, i) => (
					<Fragment key={s.label}>
						{i > 0 && (
							<LongArrowAltRightIcon
								style={{ alignSelf: "center", flex: "none", ...subtle }}
							/>
						)}
						<div
							style={{
								flex: "1 1 0",
								padding: "0.5rem 0.75rem",
								borderRadius: 8,
								border:
									s.mark === "x"
										? `2px solid ${color.x}`
										: "1px solid var(--pf-t--global--border--color--default)",
								borderTop: `6px solid ${color[s.mark]}`,
							}}
						>
							<div>
								{icons[s.mark]} <Term label={s.label} />
							</div>
							<div
								style={{
									fontSize: "var(--pf-t--global--font--size--xl)",
									fontWeight: 600,
									...(s.mark === "o" && { color: subtle.color }),
								}}
							>
								{s.state}
							</div>
							<div style={subtle}>{s.detail}</div>
							{s.mark === "x" && (
								<div style={{ marginTop: "0.5rem" }}>
									<strong>Next:</strong> {s.next}
								</div>
							)}
						</div>
					</Fragment>
				))}
			</div>
		</>
	);
}

/** One segmented bar, a segment per stage and per operator catalog; Tickets fills by its verified share. */
export function Bar({ r }: { r: Readiness }) {
	const fill = ({ mark, verified }: Stage) =>
		verified
			? `linear-gradient(to right, ${color.v} ${(100 * verified[0]) / (verified[1] || 1)}%, ${color[mark]} 0)`
			: color[mark];
	return (
		<>
			<div
				style={{
					display: "grid",
					gridTemplateColumns: `repeat(${r.stages.length}, 1fr)`,
					columnGap: 4,
					rowGap: "0.25rem",
				}}
			>
				{r.stages.map((s) => (
					<div key={s.label} style={{ display: "flex", gap: 4 }}>
						{(s.operators ?? [s]).map((part) => (
							<Tooltip
								key={part.label}
								content={`${part.label}: ${part.state}`}
							>
								<div
									style={{
										flex: 1,
										height: 12,
										borderRadius: 3,
										background: fill(part),
									}}
								/>
							</Tooltip>
						))}
					</div>
				))}
				{r.stages.map((s) => (
					<div key={s.label}>
						<Term label={s.label} /> <span style={subtle}>{s.state}</span>
					</div>
				))}
			</div>
			<Flex
				gap={{ default: "gapSm" }}
				alignItems={{ default: "alignItemsCenter" }}
				style={{ marginTop: "0.75rem" }}
			>
				<Verdict r={r} />
				<span>{r.message}</span>
			</Flex>
		</>
	);
}

const arrow = "1rem";

/** Arrow-shaped chips colored by state, then one callout: the verdict, the lateness and what is next. */
export function Pipeline({ r }: { r: Readiness }) {
	return (
		<>
			<div style={{ display: "flex", marginBottom: "0.75rem" }}>
				{r.stages.map((s, i) => (
					<div
						key={s.label}
						style={{
							flex: "1 1 0",
							marginLeft: i > 0 ? `calc(${arrow} * -0.5)` : 0,
							padding: `0.5rem calc(${arrow} + 0.5rem)`,
							background: tint[s.mark],
							clipPath: `polygon(0 0, calc(100% - ${arrow}) 0, 100% 50%, calc(100% - ${arrow}) 100%, 0 100%${i > 0 ? `, ${arrow} 50%` : ""})`,
						}}
					>
						<div>
							{icons[s.mark]} <Term label={s.label} />
							<Dots stage={s} />
						</div>
						<div>{s.state}</div>
					</div>
				))}
			</div>
			<Alert
				isInline
				variant={tones[r.verdict.color].variant}
				title={
					<Flex gap={{ default: "gapSm" }}>
						{r.verdict.text}
						{pastDue(r, true)}
					</Flex>
				}
			>
				{r.message}
			</Alert>
		</>
	);
}
