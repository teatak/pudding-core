#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const outputDir = path.join(root, "dist", "legal");
const electronDir = path.join(root, "web", "node_modules", "electron");
const electronChromiumLicenses = path.join(electronDir, "dist", "LICENSES.chromium.html");
const entries = new Map();

collectNpmDependencies(path.join(root, "package-lock.json"), root, "npm");
collectNpmDependencies(
  path.join(root, "web", "package-lock.json"),
  path.join(root, "web"),
  "npm-web",
);
collectGoDependencies();
collectElectron();
collectNativeComponents();

fs.rmSync(outputDir, { recursive: true, force: true });
fs.mkdirSync(outputDir, { recursive: true });
fs.copyFileSync(path.join(root, "LICENSE"), path.join(outputDir, "PUDDING-LICENSE.txt"));
ensureElectronLicenses();
fs.copyFileSync(electronChromiumLicenses, path.join(outputDir, "LICENSES.chromium.html"));

const generated = [
  "PUDDING THIRD-PARTY NOTICES",
  "",
  "This file is generated from the exact dependencies installed for the Pudding build.",
  "Each component remains subject to its own license. Chromium notices are provided separately in",
  "LICENSES.chromium.html.",
  "",
];
for (const entry of [...entries.values()].sort((left, right) => left.id.localeCompare(right.id))) {
  generated.push("=".repeat(80));
  generated.push((entry.name + " " + entry.version).trim());
  generated.push("Source: " + entry.source);
  generated.push("Declared license: " + entry.license);
  generated.push("-".repeat(80));
  for (const licenseFile of entry.licenseFiles) {
    generated.push("[" + path.basename(licenseFile) + "]");
    generated.push(fs.readFileSync(licenseFile, "utf8").trimEnd());
    generated.push("");
  }
  if (entry.generatedLicenseText) {
    generated.push("[license text reconstructed from package metadata]");
    generated.push(entry.generatedLicenseText);
    generated.push("");
  }
}
fs.writeFileSync(
  path.join(outputDir, "THIRD_PARTY_NOTICES.txt"),
  generated.join("\n") + "\n",
);
console.log(
  "Generated " + entries.size + " third-party notices in " + path.relative(root, outputDir) + ".",
);

function collectNpmDependencies(lockPath, installRoot, ecosystem) {
  const lock = JSON.parse(fs.readFileSync(lockPath, "utf8"));
  for (const [packagePath, locked] of Object.entries(lock.packages || {})) {
    if (
      !packagePath ||
      !packagePath.includes("node_modules/") ||
      locked.dev === true ||
      locked.link === true
    ) {
      continue;
    }
    const packageDir = path.join(installRoot, packagePath);
    const metadataPath = path.join(packageDir, "package.json");
    if (!fs.existsSync(metadataPath)) {
      if (locked.optional === true) {
        continue;
      }
      throw new Error("installed package is missing for " + packagePath + "; run npm ci");
    }
    const metadata = JSON.parse(fs.readFileSync(metadataPath, "utf8"));
    const name = String(metadata.name || packagePath.split("node_modules/").pop());
    const version = String(metadata.version || locked.version || "");
    const licenseFiles = findLicenseFiles(packageDir);
    addEntry({
      id: ecosystem + ":" + name + "@" + version,
      name,
      version,
      source: repositoryURL(metadata, "https://www.npmjs.com/package/" + encodeURIComponent(name)),
      license: String(metadata.license || locked.license || "SEE LICENSE TEXT"),
      licenseFiles,
      generatedLicenseText: licenseFiles.length === 0 ? fallbackLicenseText(metadata) : "",
    });
  }
}

function collectGoDependencies() {
  const output = execFileSync(
    "go",
    [
      "list",
      "-deps",
      "-tags=sqlite_fts5 webrtcaec",
      "-f",
      "{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}",
      "./cmd/puddingd",
    ],
    { cwd: root, encoding: "utf8" },
  );
  for (const row of new Set(output.split("\n").filter(Boolean))) {
    const [modulePath, version, moduleDir] = row.split("|");
    if (!modulePath || modulePath === "github.com/teatak/pudding-core") {
      continue;
    }
    addEntry({
      id: "go:" + modulePath + "@" + version,
      name: modulePath,
      version,
      source: "https://" + modulePath,
      license: "SEE LICENSE TEXT",
      licenseFiles: findLicenseFiles(moduleDir),
    });
  }
}

function collectElectron() {
  const metadata = JSON.parse(
    fs.readFileSync(path.join(electronDir, "package.json"), "utf8"),
  );
  addEntry({
    id: "electron:" + metadata.version,
    name: "Electron",
    version: metadata.version,
    source: "https://github.com/electron/electron",
    license: metadata.license || "MIT",
    licenseFiles: [path.join(electronDir, "LICENSE")],
  });
}

function ensureElectronLicenses() {
  if (fs.statSync(electronChromiumLicenses, { throwIfNoEntry: false })?.isFile()) {
    return;
  }
  const installer = path.join(electronDir, "install.js");
  if (!fs.statSync(installer, { throwIfNoEntry: false })?.isFile()) {
    throw new Error("Electron is not installed; run npm --prefix web ci");
  }
  execFileSync(process.execPath, [installer], { cwd: electronDir, stdio: "inherit" });
  if (!fs.statSync(electronChromiumLicenses, { throwIfNoEntry: false })?.isFile()) {
    throw new Error("Electron installation did not provide LICENSES.chromium.html");
  }
}

function collectNativeComponents() {
  const components = [
    [
      "Abseil C++",
      "2505.0.0",
      "Apache-2.0",
      "https://github.com/abseil/abseil-cpp",
      "ABSEIL-LICENSE.txt",
    ],
    [
      "ONNX Runtime",
      "1.24.4",
      "MIT",
      "https://github.com/microsoft/onnxruntime",
      "ONNX-RUNTIME-LICENSE.txt",
    ],
    [
      "PortAudio",
      "19.7.0",
      "MIT",
      "https://github.com/PortAudio/portaudio",
      "PORTAUDIO-LICENSE.txt",
    ],
    [
      "WebRTC Audio Processing",
      "1.3",
      "BSD-3-Clause",
      "https://gitlab.freedesktop.org/pulseaudio/webrtc-audio-processing",
      "WEBRTC-AUDIO-PROCESSING-LICENSE.txt",
    ],
  ];
  for (const [name, version, license, source, fileName] of components) {
    addEntry({
      id: "native:" + name + "@" + version,
      name,
      version,
      source,
      license,
      licenseFiles: [path.join(root, "legal", "native", fileName)],
    });
  }
}

function addEntry(entry) {
  if (entries.has(entry.id)) {
    return;
  }
  for (const licenseFile of entry.licenseFiles) {
    if (!fs.statSync(licenseFile, { throwIfNoEntry: false })?.isFile()) {
      throw new Error("license file is missing for " + entry.name + ": " + licenseFile);
    }
  }
  if (entry.licenseFiles.length === 0 && !entry.generatedLicenseText) {
    throw new Error("no license file found for " + entry.name + " " + entry.version);
  }
  entries.set(entry.id, entry);
}

function findLicenseFiles(directory) {
  const stat = fs.statSync(directory, { throwIfNoEntry: false });
  if (!stat?.isDirectory()) {
    return [];
  }
  return fs
    .readdirSync(directory)
    .filter((name) => /^(?:licen[sc]e|copying|notice|copyright)(?:\..*)?$/i.test(name))
    .map((name) => path.join(directory, name))
    .filter((filePath) => fs.statSync(filePath).isFile())
    .sort();
}

function repositoryURL(metadata, fallback) {
  const repository =
    typeof metadata.repository === "string" ? metadata.repository : metadata.repository?.url;
  return String(repository || metadata.homepage || fallback)
    .replace(/^git\+/, "")
    .replace(/\.git$/, "");
}

function fallbackLicenseText(metadata) {
  const license = String(metadata.license || "");
  const author =
    typeof metadata.author === "string"
      ? metadata.author
      : String(metadata.author?.name || metadata.maintainers?.[0]?.name || metadata.name || "contributors");
  const texts = [];
  if (license.includes("MIT")) {
    texts.push(mitLicenseText(author));
  }
  if (license.includes("ISC") && (license.includes("AND") || !license.includes("MIT"))) {
    texts.push(iscLicenseText(author));
  }
  return texts.join("\n\n");
}

function mitLicenseText(author) {
  return [
    "Copyright (c) " + author,
    "",
    "Permission is hereby granted, free of charge, to any person obtaining a copy",
    'of this software and associated documentation files (the "Software"), to deal',
    "in the Software without restriction, including without limitation the rights",
    "to use, copy, modify, merge, publish, distribute, sublicense, and/or sell",
    "copies of the Software, and to permit persons to whom the Software is",
    "furnished to do so, subject to the following conditions:",
    "",
    "The above copyright notice and this permission notice shall be included in all",
    "copies or substantial portions of the Software.",
    "",
    'THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR',
    "IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,",
    "FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE",
    "AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER",
    "LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,",
    "OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE",
    "SOFTWARE.",
  ].join("\n");
}

function iscLicenseText(author) {
  return [
    "Copyright (c) " + author,
    "",
    "Permission to use, copy, modify, and/or distribute this software for any",
    "purpose with or without fee is hereby granted, provided that the above",
    "copyright notice and this permission notice appear in all copies.",
    "",
    'THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES',
    "WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF",
    "MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY",
    "SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER",
    "RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT,",
    "NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE",
    "USE OR PERFORMANCE OF THIS SOFTWARE.",
  ].join("\n");
}
