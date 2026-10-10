import { Label } from "@patternfly/react-core";
import { Fragment } from "react";
import { Link, useSearchParams } from "react-router-dom";

const VERSIONS = ["quay-v3.18.1", "quay-v3.17.6", "quay-v3.17.5"];

// The open questions and their options; the first option is the default.
const QUESTIONS = {
	readiness: {
		title: "Readiness",
		options: {
			strip: "Stage tiles with counts.",
			bar: "A segmented progress bar.",
			pipeline: "Chevron stages and a blocker callout.",
		},
	},
	q4: {
		title: "Q4 The Shipped stage",
		options: {
			B: "From the Red Hat catalog and Jira's released flag.",
			none: "None; the verdict still says Shipped.",
		},
	},
};

type Question = keyof typeof QUESTIONS;
export type Picks = Record<Question, string>;

/** The URL's ?readiness= and ?q4= picks; a missing or unknown one is the default. */
export function usePicks(): Picks {
	const [params] = useSearchParams();
	const pick = (q: Question) => {
		const options = Object.keys(QUESTIONS[q].options);
		const value = params.get(q) ?? "";
		return options.includes(value) ? value : options[0];
	};
	return { readiness: pick("readiness"), q4: pick("q4") };
}

export const mockupPath = (version: string, picks: Picks, builds = false) =>
	`/mockups/releases/${encodeURIComponent(version)}${builds ? "/snapshots" : ""}?${new URLSearchParams(picks)}`;

/** Mockup chrome: the version, the locked picks and the open questions, as links. */
export default function Switcher({
	version,
	picks,
	builds = false,
}: {
	version: string;
	picks: Picks;
	/** On the Builds page mockup, which the version page's Builds button opens. */
	builds?: boolean;
}) {
	const live = `/releases/${encodeURIComponent(version)}${builds ? "/snapshots" : ""}`;
	return (
		<div
			style={{
				border:
					"2px dashed var(--pf-t--global--border--color--status--warning--default)",
				borderRadius: 6,
				padding: "0.5rem 0.75rem",
				fontSize: "var(--pf-t--global--font--size--sm)",
				lineHeight: 1.6,
			}}
		>
			<div>
				<Label color="orange" isCompact>
					Mockup
				</Label>{" "}
				Proposed release page on live data, round 2. Version:{" "}
				{VERSIONS.map((v) => (
					<Fragment key={v}>
						<Option to={mockupPath(v, picks, builds)} selected={v === version}>
							{v}
						</Option>{" "}
					</Fragment>
				))}
				· <Link to={live}>Live page</Link>
			</div>
			<div>
				Locked: Q1-A Builds button, Q2 blocker and Past due date, Q3-B per
				operator, Q5-A .z tickets count.
			</div>
			{(Object.keys(QUESTIONS) as Question[]).map((q) => (
				<div key={q}>
					<strong>{QUESTIONS[q].title}:</strong>{" "}
					{Object.entries(QUESTIONS[q].options).map(([option, text]) => (
						<Fragment key={option}>
							<Option
								to={mockupPath(version, { ...picks, [q]: option })}
								selected={picks[q] === option}
							>
								<strong>{option}</strong> {text}
							</Option>{" "}
						</Fragment>
					))}
				</div>
			))}
		</div>
	);
}

function Option({
	to,
	selected,
	children,
}: {
	to: string;
	selected: boolean;
	children: React.ReactNode;
}) {
	return (
		<Link
			to={to}
			aria-current={selected ? "page" : undefined}
			style={{
				padding: "0 0.3rem",
				borderRadius: 4,
				...(selected && {
					color: "var(--pf-t--global--text--color--on-brand--default)",
					background: "var(--pf-t--global--color--brand--default)",
				}),
			}}
		>
			{children}
		</Link>
	);
}
