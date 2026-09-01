import React from "react";
import ReactDOM from "react-dom/client";

import "@/styles.css";

import { MascotSceneLab } from "./MascotSceneLab";
import "./lab.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <MascotSceneLab />
  </React.StrictMode>,
);
