#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

const [inputDirectory, outputFile] = process.argv.slice(2);
if (!inputDirectory || !outputFile) {
  console.error("usage: node scripts/build-icns.mjs <png-directory> <output.icns>");
  process.exit(1);
}

const representations = [
  ["icp4", "icon_16x16.png"],
  ["icp5", "icon_32x32.png"],
  ["icp6", "icon_64x64.png"],
  ["ic07", "icon_128x128.png"],
  ["ic08", "icon_256x256.png"],
  ["ic09", "icon_512x512.png"],
  ["ic10", "icon_1024x1024.png"],
];

const elements = representations.map(([type, filename]) => {
  const png = readFileSync(path.join(inputDirectory, filename));
  const element = Buffer.allocUnsafe(8 + png.length);
  element.write(type, 0, 4, "ascii");
  element.writeUInt32BE(element.length, 4);
  png.copy(element, 8);
  return element;
});

const totalLength = 8 + elements.reduce((sum, element) => sum + element.length, 0);
const header = Buffer.allocUnsafe(8);
header.write("icns", 0, 4, "ascii");
header.writeUInt32BE(totalLength, 4);

writeFileSync(outputFile, Buffer.concat([header, ...elements], totalLength));
