import type { ReactNode } from "react";
import { Route, Routes } from "react-router-dom";
import FbcImagesA from "./FbcImagesA";
import FbcImagesB from "./FbcImagesB";
import FbcImagesC from "./FbcImagesC";

/** Design mockups on live data, served under /mockups/ in dev builds only. */
const mockups: { path: string; element: ReactNode }[] = [
	{ path: "fbc-images-a/:version", element: <FbcImagesA /> },
	{ path: "fbc-images-b/:version", element: <FbcImagesB /> },
	{ path: "fbc-images-c/:version", element: <FbcImagesC /> },
];

export default function Mockups() {
	return (
		<Routes>
			{mockups.map((m) => (
				<Route key={m.path} path={m.path} element={m.element} />
			))}
		</Routes>
	);
}
