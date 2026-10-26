// The app's page: the theme, notifications and the app, mounted once.
import { Notifications } from "@mantine/notifications";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Provider } from "../theme/Provider";
import { App } from "./App";

createRoot(document.getElementById("root")!).render(<StrictMode><Provider><Notifications position="bottom-center" /><App /></Provider></StrictMode>);
