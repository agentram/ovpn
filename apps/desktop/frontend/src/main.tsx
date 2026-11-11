import { createRoot } from "react-dom/client";
import { App } from "./desk/App";
import "./style.css";
import "./desk/style.css";

createRoot(document.getElementById("root")!).render(<App />);
