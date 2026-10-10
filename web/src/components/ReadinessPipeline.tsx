import { Alert, Flex, Label, Tooltip } from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	OutlinedCircleIcon,
	OutlinedClockIcon,
} from "@patternfly/react-icons";
import type { Mark, Readiness, Stage } from "../utils/readiness";

const terms: Record<string, string> = {
	Build:
		"The images ART built for this version and pushed to stage, its pre-production push that QE tests. Konflux records them as a snapshot.",
	Catalog:
		"The operator's OperatorHub entry, a file-based catalog (FBC). ART stages one per operator and OCP version.",
	Tickets:
		"Jira tickets whose Target Version is this release, plus the .z tickets a build commit names. Release Pending, Verified, Closed and Done count as verified.",
};

const color = {
	v: "var(--pf-t--global--color--status--success--default)",
	x: "var(--pf-t--global--color--status--danger--default)",
};

// Light fills for the chips.
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

const variant = { green: "success", red: "danger", blue: "info" } as const;

const arrow = "1rem";

/** Arrow-shaped chips colored by state, then one callout: the verdict, the lateness and what is next. */
export default function ReadinessPipeline({ r }: { r: Readiness }) {
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
				variant={variant[r.verdict.color]}
				title={
					<Flex gap={{ default: "gapSm" }}>
						{r.verdict.text}
						{r.pastDue && (
							<Label color="red" isCompact icon={<OutlinedClockIcon />}>
								{r.pastDue}
							</Label>
						)}
					</Flex>
				}
			>
				{r.verdict.message}
			</Alert>
		</>
	);
}
