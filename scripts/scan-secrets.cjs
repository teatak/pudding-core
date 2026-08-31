#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const sensitiveExtensions = /\.(?:cer|key|mobileprovision|p12|pfx|pem)$/i;
const sensitiveNames = /(^|\/)\.env(?:\.|$)/;
const rules = [
  ["AWS access key", /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/g],
  ["GitHub token", /\b(?:github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,})\b/g],
  ["GitLab token", /\bglpat-[A-Za-z0-9_-]{20,}\b/g],
  ["Google API key", /\bAIza[0-9A-Za-z_-]{35}\b/g],
  ["OpenAI or compatible API key", /\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}\b/g],
  ["Slack token", /\bxox[baprs]-[A-Za-z0-9-]{10,}\b/g],
  ["Stripe live secret", /\bsk_live_[A-Za-z0-9]{16,}\b/g],
  ["Private key", /-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----/g],
  ["Credential in URL", /https?:\/\/[^\s/:]+:[^\s/@]+@[^\s/:]+/g],
];
const assignmentPattern = /\b(api[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|password|private[_-]?key)\b\s*[:=]\s*["']([^"'\\r\\n]{16,})["']/gi;
const placeholderPattern = /(?:example|placeholder|dummy|fake|fixture|redacted|test[-_ ]?(?:key|token|secret)|your[-_ ]|<[^>]+>|[$][{]|process\.env)/i;

const tracked = execFileSync("git", ["-c", "core.fsmonitor=false", "ls-files", "-z"], {
  cwd: root,
  encoding: "utf8",
}).split("\0").filter(Boolean);
const findings = [];

for (const relativePath of tracked) {
  if (
    sensitiveExtensions.test(relativePath) ||
    (sensitiveNames.test(relativePath) && relativePath !== ".env.example")
  ) {
    findings.push({ relativePath, line: 1, rule: "sensitive file is tracked" });
    continue;
  }
  const filePath = path.join(root, relativePath);
  const stat = fs.statSync(filePath, { throwIfNoEntry: false });
  if (!stat?.isFile() || stat.size > 5 * 1024 * 1024) {
    continue;
  }
  const buffer = fs.readFileSync(filePath);
  if (buffer.includes(0)) {
    continue;
  }
  const contents = buffer.toString("utf8");
  for (const [rule, pattern] of rules) {
    pattern.lastIndex = 0;
    for (const match of contents.matchAll(pattern)) {
      if (rule === "Credential in URL" && placeholderPattern.test(match[0])) {
        continue;
      }
      findings.push({ relativePath, line: lineNumber(contents, match.index), rule });
    }
  }
  assignmentPattern.lastIndex = 0;
  for (const match of contents.matchAll(assignmentPattern)) {
    const value = match[2];
    if (!placeholderPattern.test(value) && shannonEntropy(value) >= 3.5) {
      findings.push({
        relativePath,
        line: lineNumber(contents, match.index),
        rule: "high-entropy " + match[1],
      });
    }
  }
}

const unique = [
  ...new Map(
    findings.map((finding) => [
      finding.relativePath + ":" + finding.line + ":" + finding.rule,
      finding,
    ]),
  ).values(),
];
if (unique.length > 0) {
  console.error("Potential secrets found. Values are intentionally redacted:");
  for (const finding of unique) {
    console.error(finding.relativePath + ":" + finding.line + ": " + finding.rule);
  }
  process.exit(1);
}
console.log("Secret scan passed: " + tracked.length + " tracked files checked.");

function lineNumber(contents, index) {
  return contents.slice(0, index).split("\n").length;
}

function shannonEntropy(value) {
  const frequencies = new Map();
  for (const character of value) {
    frequencies.set(character, (frequencies.get(character) || 0) + 1);
  }
  let entropy = 0;
  for (const count of frequencies.values()) {
    const probability = count / value.length;
    entropy -= probability * Math.log2(probability);
  }
  return entropy;
}
