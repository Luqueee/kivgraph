import ReactDOM from "react-dom/client";

import App from "@/App";
import "@/index.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Kivgraph web root element is missing");
}

ReactDOM.createRoot(root).render(<App />);
