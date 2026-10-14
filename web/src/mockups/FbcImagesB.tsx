import { OperatorImages } from "../components/OperatorImages";
import { FbcImagesPage } from "./fbcImages";

/** Design B: a panel per operator, its problem images first. */
export default function FbcImagesB() {
	return (
		<FbcImagesPage
			ui={{ card: (d, version) => <OperatorImages d={d} version={version} /> }}
		/>
	);
}
