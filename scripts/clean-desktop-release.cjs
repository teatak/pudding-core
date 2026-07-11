const fs = require("node:fs");
const path = require("node:path");

const outputDir = path.resolve(__dirname, "..", "dist", "release");
fs.rmSync(outputDir, { force: true, recursive: true });
console.log(`Cleaned desktop release output: ${outputDir}`);
