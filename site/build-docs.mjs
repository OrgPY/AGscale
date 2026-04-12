import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = path.resolve(process.argv[2] || path.join(rootDir, ".pages-dist"));
const docsOutputDir = path.join(outputDir, "docs");
const assetVersion = "20260412-docs-reference";

const curatedDocs = [
  { source: "README.md", slug: "overview", group: "Start Here", summary: "Project overview, resource model, release flow, and CLI entry points." },
  { source: "docs/architecture.md", slug: "architecture", group: "Start Here", summary: "Core concepts, reconciliation contract, validation rules, reliability, and observability." },
  { source: "docs/runbook-openclaw-operator.md", slug: "operator-runbook", group: "Start Here", summary: "Install order, direct Qwen mode, OpenClaw gateway mode, and troubleshooting." },
  { source: "docs/demo-openclaw-full-stack.md", slug: "openclaw-full-stack", group: "Demos", summary: "End-to-end OpenClaw stack with platform resources, agents, workflows, CLI, and TUI." },
  { source: "docs/demo-clawdbot-moltbot.md", slug: "clawdbot-moltbot", group: "Demos", summary: "Two-agent deployment guide with base, OpenClaw, OpenCode, and Moltbot overlays." },
  { source: "docs/demo-openclaw-minimal-qwenplus.md", slug: "minimal-openclaw-qwenplus", group: "Demos", summary: "Minimal OpenClaw Gateway plus qwen-plus deployment path." },
  { source: "docs/demo-qwen-vectorengine-minimal.md", slug: "minimal-qwen-vectorengine", group: "Demos", summary: "Smallest direct Qwen-compatible VectorEngine deployment." },
  { source: "docs/demo-opencode-byovalue.md", slug: "opencode-byo", group: "Demos", summary: "Bring-your-own OpenCode endpoint and model configuration." },
  { source: "docs/team-builder-quickstart.md", slug: "team-builder", group: "Operations", summary: "Create, validate, run, and watch agent teams from the CLI." },
  { source: "docs/fleet-command-center.md", slug: "fleet-command-center", group: "Operations", summary: "Discover local and SSH-hosted agents, persist inventory, and schedule fleet tasks." },
];

let sourceDocs = [...curatedDocs];

const cliCommands = [
  {
    group: "Core",
    commands: [
      { name: "version", usage: "agscale version", purpose: "Print the CLI version.", flags: [] },
      { name: "help", usage: "agscale help", purpose: "Print the top-level command summary.", flags: [] },
      {
        name: "tui",
        usage: "agscale tui [<agent-id-or-name>] [--namespace default] [--inventory path]",
        purpose: "Open the combined cluster/fleet agent selector or jump directly into a target chat-first TUI.",
        flags: ["-n, --namespace: Kubernetes namespace for cluster agents.", "--inventory: fleet inventory path override."],
      },
    ],
  },
  {
    group: "Cluster",
    commands: [
      { name: "cluster doctor", usage: "agscale cluster doctor [-n namespace]", purpose: "Validate namespace reachability and core AGScale CRDs.", flags: ["-n, --namespace: namespace to check."] },
      { name: "cluster get", usage: "agscale cluster get <agents|workflows|runs|skills> [-n namespace]", purpose: "List AGScale resources with compact operational columns.", flags: ["-n, --namespace: namespace to query."] },
      { name: "cluster describe agent", usage: "agscale cluster describe agent <name> [-n namespace]", purpose: "Print runtime, endpoint, budget, URL, and condition details for one Agent.", flags: ["-n, --namespace: namespace to query."] },
      { name: "cluster logs agent", usage: "agscale cluster logs agent <name> [-n namespace] [--tail N]", purpose: "Stream logs from the first matching agent pod.", flags: ["-n, --namespace: namespace to query.", "--tail: number of log lines."] },
      { name: "cluster apply", usage: "agscale cluster apply -f <file> | -k <kustomize-dir> [-n namespace]", purpose: "Apply a manifest file or Kustomize directory through kubectl.", flags: ["-f: manifest file.", "-k: Kustomize directory.", "-n: namespace override."] },
      { name: "cluster tui agent", usage: "agscale cluster tui agent <name> [-n namespace]", purpose: "Open the Kubernetes-backed TUI for one Agent.", flags: ["-n, --namespace: namespace to query."] },
    ],
  },
  {
    group: "Team",
    commands: [
      { name: "team init", usage: "agscale team init <team-name> [-n namespace] [--profile openclaw|openhands|openai] [--out file] [--apply]", purpose: "Run the team-builder wizard and write an AgentWorkflow bundle.", flags: ["-n, --namespace: namespace for generated objects.", "--profile: runtime protocol defaults.", "--out: output YAML path.", "--apply: apply after generation."] },
      { name: "team up", usage: "agscale team up <team-name> [-n namespace] [--profile openclaw|openhands|openai] [--out file]", purpose: "Guided create, apply, and first-run path for a team bundle.", flags: ["-n, --namespace: namespace for generated objects.", "--profile: runtime protocol defaults.", "--out: output YAML path."] },
      { name: "team validate", usage: "agscale team validate -f <bundle.yaml> [-n namespace]", purpose: "Run kubectl client and server dry-runs against a generated bundle.", flags: ["-f: bundle YAML file.", "-n, --namespace: namespace for dry-run."] },
      { name: "team watch", usage: "agscale team watch <run-name> [-n namespace] [--interval-seconds 2] [--timeout-seconds 0]", purpose: "Poll and render an AgentWorkflowRun until completion or timeout.", flags: ["-n, --namespace: namespace to query.", "--interval-seconds: poll interval.", "--timeout-seconds: zero means no timeout."] },
      { name: "team run", usage: "agscale team run <workflow> [-n namespace] [--trigger manual] [--correlation-id id]", purpose: "Create a new AgentWorkflowRun for an AgentWorkflow.", flags: ["-n, --namespace: namespace for the run.", "--trigger: trigger label.", "--correlation-id: external correlation value."] },
      { name: "team status", usage: "agscale team status <run-name> [-n namespace]", purpose: "Print run phase, timing, and per-step result rows.", flags: ["-n, --namespace: namespace to query."] },
      { name: "team graph", usage: "agscale team graph <workflow> [-n namespace]", purpose: "Render workflow steps, protocols, targets, and dependencies.", flags: ["-n, --namespace: namespace to query."] },
      { name: "team command-center", usage: "agscale team command-center <task-name> [-n namespace] [--selector k=v] [--agents a,b] [--exclude-agents x,y] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--entry-agent name] [--protocol http|mcp|a2a] [--out file] [--apply] [--run] [--explain-selection]", purpose: "Select a subset of ready agents and generate an AgentWorkflow for the task.", flags: ["-n, --namespace: namespace to query.", "--selector: label selector.", "--agents: explicit include list.", "--exclude-agents: explicit exclude list.", "--require-skills: required skill refs.", "--max-agents: selected agent limit.", "--topology: dag or sequential.", "--entry-agent: selected entry agent.", "--protocol: http, mcp, or a2a.", "--out: output YAML file.", "--apply: apply generated bundle.", "--run: create first run after apply.", "--explain-selection: print selection decisions."] },
    ],
  },
  {
    group: "Fleet",
    commands: [
      { name: "fleet agent scan", usage: "agscale fleet agent scan [--local] [--hosts host1,host2] [--scan-mode agent-aware|ports] [--signatures openclaw,opencode,...] [--ssh-config ~/.ssh/config] [--ports 18789,8080] [--write]", purpose: "Discover local and SSH-hosted agents and optionally persist inventory.", flags: ["--local: include localhost scan.", "--hosts: comma-separated SSH hosts.", "--scan-mode: agent-aware or ports.", "--signatures: process signatures for agent-aware mode.", "--ssh-config: SSH config path.", "--ports: candidate ports.", "--timeout-seconds: probe timeout.", "--output: table or json.", "--write: persist results.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet agent list", usage: "agscale fleet agent list [--status healthy|degraded|unknown|all] [--protocol http|mcp|all]", purpose: "List persisted fleet agents.", flags: ["--status: health filter.", "--protocol: protocol filter.", "--host: host filter.", "--skills: required skills.", "--output: table or json.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet agent inspect", usage: "agscale fleet agent inspect <agent-id-or-name>", purpose: "Print one fleet agent record as JSON.", flags: ["--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet agent chat", usage: "agscale fleet agent chat <agent-id-or-name> [--message \"hello\"] [--ssh-config ~/.ssh/config]", purpose: "Send one message or open interactive chat to a fleet agent.", flags: ["--message: one-shot prompt.", "--raw: print raw JSON response.", "--ssh-config: SSH config path.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet agent pin", usage: "agscale fleet agent pin <agent-id-or-name> --endpoint <url> [--reachability direct|ssh-loopback] [--protocol http|mcp] [--chat-path /chat] [--model name]", purpose: "Override a discovered agent endpoint and metadata.", flags: ["--endpoint: required explicit base endpoint.", "--reachability: direct or ssh-loopback.", "--protocol: http or mcp.", "--chat-path: chat endpoint path.", "--model: default model metadata.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet agent unpin", usage: "agscale fleet agent unpin <agent-id-or-name>", purpose: "Restore discovered endpoint metadata for an agent.", flags: ["--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet host add", usage: "agscale fleet host add <name> [--address host-or-alias] [--ssh-profile profile] [--labels k=v]", purpose: "Register a host for future SSH fleet scans.", flags: ["--address: SSH hostname, IP, or config alias.", "--ssh-profile: SSH profile override.", "--labels: host labels.", "--state-dir: fleet state directory."] },
      { name: "fleet host list", usage: "agscale fleet host list [--status healthy|degraded|unknown|all]", purpose: "List registered fleet hosts.", flags: ["--status: host health filter.", "--output: table or json.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet host inspect", usage: "agscale fleet host inspect <host-id-or-name>", purpose: "Print one host record as JSON.", flags: ["--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet host label", usage: "agscale fleet host label <host-id-or-name> --set k=v,k2=v2", purpose: "Merge labels onto a registered host.", flags: ["--set: labels to merge.", "--state-dir: fleet state directory."] },
      { name: "fleet host rm", usage: "agscale fleet host rm <host-id-or-name> [--keep-agents]", purpose: "Remove a host and optionally its associated agents.", flags: ["--keep-agents: leave associated agents in inventory.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet host doctor", usage: "agscale fleet host doctor [<host-id-or-name>] [--write]", purpose: "Refresh local or SSH host health.", flags: ["--ssh-config: SSH config path.", "--timeout-seconds: SSH timeout.", "--write: persist refreshed health.", "--output: table or json.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet run dispatch", usage: "agscale fleet run [dispatch] <task-name> [--agents a,b] [--selector k=v] [--require-skills s1,s2] [--max-agents 3] [--topology dag|sequential] [--dry-run]", purpose: "Select eligible fleet agents and execute a task plan.", flags: ["--agents: explicit include list.", "--exclude-agents: explicit exclude list.", "--selector: label selector.", "--require-skills: required skills.", "--max-agents: selected agent limit.", "--topology: dag or sequential.", "--explain-selection: print selection decisions.", "--dry-run: print plan only.", "--timeout-seconds: step invoke timeout.", "--ssh-config: SSH config path.", "--output: table or json.", "--state-dir: fleet state directory.", "--inventory: inventory path override."] },
      { name: "fleet run history", usage: "agscale fleet run history [--limit 20]", purpose: "Print persisted fleet run history.", flags: ["--limit: maximum runs.", "--output: table or json.", "--state-dir: fleet state directory."] },
      { name: "fleet run inspect", usage: "agscale fleet run inspect <run-id>", purpose: "Print one fleet run record as JSON.", flags: ["--state-dir: fleet state directory."] },
    ],
  },
  {
    group: "Templates",
    commands: [
      { name: "opencode template", usage: "agscale opencode template [--namespace default --name shared-opencode --secret opencode-api-key --base-url <url> --model <model>]", purpose: "Print a Secret and LLMEndpoint template for a BYO OpenCode-compatible endpoint.", flags: ["--namespace: namespace for generated objects.", "--name: LLMEndpoint name.", "--secret: Secret name.", "--secret-key: Secret key field.", "--base-url: OpenCode-compatible API base URL.", "--model: default model."] },
    ],
  },
];

function escapeHtml(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function slugify(value) {
  return value
    .toLowerCase()
    .replace(/`([^`]+)`/g, "$1")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80) || "section";
}

function renderInline(value) {
  const codeSpans = [];
  let rendered = value.replace(/`([^`]+)`/g, (_, code) => {
    const token = `@@CODE${codeSpans.length}@@`;
    codeSpans.push(`<code>${escapeHtml(code)}</code>`);
    return token;
  });

  rendered = escapeHtml(rendered)
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, text, href) => {
      const safeHref = escapeHtml(href);
      const safeText = renderInline(text);
      return `<a href="${safeHref}">${safeText}</a>`;
    });

  codeSpans.forEach((code, index) => {
    rendered = rendered.replace(`@@CODE${index}@@`, code);
  });

  return rendered;
}

function renderMarkdown(markdown) {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const html = [];
  const headings = [];
  const usedIds = new Map();
  let paragraph = [];
  let listType = null;
  let inFence = false;
  let fenceLang = "";
  let fenceLines = [];

  function flushParagraph() {
    if (paragraph.length === 0) {
      return;
    }
    html.push(`<p>${renderInline(paragraph.join(" "))}</p>`);
    paragraph = [];
  }

  function closeList() {
    if (!listType) {
      return;
    }
    html.push(`</${listType}>`);
    listType = null;
  }

  function openList(type) {
    if (listType === type) {
      return;
    }
    closeList();
    listType = type;
    html.push(`<${type}>`);
  }

  for (const line of lines) {
    const fenceMatch = line.match(/^```([A-Za-z0-9_-]+)?\s*$/);
    if (fenceMatch) {
      if (inFence) {
        html.push(
          `<pre><code${fenceLang ? ` class="language-${escapeHtml(fenceLang)}"` : ""}>${escapeHtml(fenceLines.join("\n"))}</code></pre>`,
        );
        inFence = false;
        fenceLang = "";
        fenceLines = [];
      } else {
        flushParagraph();
        closeList();
        inFence = true;
        fenceLang = fenceMatch[1] || "";
      }
      continue;
    }

    if (inFence) {
      fenceLines.push(line);
      continue;
    }

    if (!line.trim()) {
      flushParagraph();
      closeList();
      continue;
    }

    const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      flushParagraph();
      closeList();
      const level = headingMatch[1].length;
      const text = headingMatch[2].trim();
      const baseId = slugify(text);
      const previousUses = usedIds.get(baseId) || 0;
      const id = previousUses === 0 ? baseId : `${baseId}-${previousUses + 1}`;
      usedIds.set(baseId, previousUses + 1);
      headings.push({ level, text: text.replace(/`/g, ""), id });
      html.push(`<h${level} id="${id}"><a class="heading-anchor" href="#${id}" aria-label="Link to ${escapeHtml(text)}">#</a>${renderInline(text)}</h${level}>`);
      continue;
    }

    const unorderedMatch = line.match(/^\s*[-*]\s+(.+)$/);
    if (unorderedMatch) {
      flushParagraph();
      openList("ul");
      html.push(`<li>${renderInline(unorderedMatch[1])}</li>`);
      continue;
    }

    const orderedMatch = line.match(/^\s*\d+\.\s+(.+)$/);
    if (orderedMatch) {
      flushParagraph();
      openList("ol");
      html.push(`<li>${renderInline(orderedMatch[1])}</li>`);
      continue;
    }

    paragraph.push(line.trim());
  }

  flushParagraph();
  closeList();

  return { body: html.join("\n"), headings };
}

function cliReferenceMarkdown() {
  const lines = [
    "# AGScale CLI Reference",
    "",
    "Generated from the CLI command surface in `internal/cli`. Commands that touch Kubernetes use your current kubeconfig unless a namespace flag says otherwise. Fleet commands use local state under `~/.agscale/fleet` unless overridden.",
    "",
    "## Install",
    "",
    "```bash",
    "npx @agscale/agscale version",
    "```",
    "",
    "Build from source:",
    "",
    "```bash",
    "make build-cli",
    "./bin/agscale version",
    "```",
    "",
    "## Command Groups",
    "",
  ];

  for (const group of cliCommands) {
    lines.push(`### ${group.group}`, "");
    for (const command of group.commands) {
      lines.push(`#### ${command.name}`, "");
      lines.push(command.purpose, "");
      lines.push("Usage:", "");
      lines.push("```bash", command.usage, "```", "");
      if (command.flags.length > 0) {
        lines.push("Flags:", "");
        for (const flag of command.flags) {
          lines.push(`- ${flag}`);
        }
        lines.push("");
      }
    }
  }

  lines.push(
    "## Deprecated Aliases",
    "",
    "- `agscale doctor` -> `agscale cluster doctor`",
    "- `agscale get` -> `agscale cluster get`",
    "- `agscale describe` -> `agscale cluster describe`",
    "- `agscale logs` -> `agscale cluster logs`",
    "- `agscale apply` -> `agscale cluster apply`",
    "- `agscale tui agent <name>` -> `agscale tui <name>`",
    "",
    "## TUI Slash Commands",
    "",
    "- `/help`: print TUI commands.",
    "- `/refresh`: refresh agent state.",
    "- `/logs`: fetch recent cluster agent logs.",
    "- `/restart`: restart the selected cluster agent pod.",
    "- `/scale <n>`: scale a cluster Agent.",
    "- `/failover`: switch to a configured backup LLM endpoint.",
    "- `/raw`: toggle raw response display.",
    "- `/quit`: exit.",
  );

  return lines.join("\n");
}

function countIndent(line) {
  return line.length - line.trimStart().length;
}

function yamlValue(line) {
  return line.slice(line.indexOf(":") + 1).trim().replace(/^"|"$/g, "");
}

function nearestField(stack, indent) {
  for (let i = stack.length - 1; i >= 0; i--) {
    if (stack[i].indent < indent) {
      return stack[i].field;
    }
  }
  return null;
}

function childParentPath(field) {
  if (!field) {
    return "";
  }
  return field.type === "array" ? `${field.path}[]` : field.path;
}

function extractSchemaSectionFields(lines, sectionName) {
  const sectionIndex = lines.findIndex((line) => line.match(new RegExp(`^\\s{12}${sectionName}:\\s*$`)));
  if (sectionIndex < 0) {
    return [];
  }

  const sectionIndent = countIndent(lines[sectionIndex]);
  const sectionLines = [];
  for (let i = sectionIndex + 1; i < lines.length; i++) {
    const line = lines[i];
    if (line.trim() && countIndent(line) <= sectionIndent) {
      break;
    }
    sectionLines.push(line);
  }

  const fields = [];
  const fieldStack = [];
  const propertiesStack = [];
  const fieldsByPath = new Map();
  let enumTarget = null;
  let requiredTarget = null;

  for (const line of sectionLines) {
    const trim = line.trim();
    if (!trim) {
      continue;
    }
    const indent = countIndent(line);
    while (fieldStack.length > 0 && fieldStack[fieldStack.length - 1].indent >= indent) {
      fieldStack.pop();
    }
    while (propertiesStack.length > 0 && propertiesStack[propertiesStack.length - 1] >= indent) {
      propertiesStack.pop();
    }

    if (enumTarget && indent > enumTarget.indent && trim.startsWith("- ")) {
      enumTarget.field.enum.push(trim.slice(2).trim());
      continue;
    }
    enumTarget = null;

    if (requiredTarget && indent > requiredTarget.indent && trim.startsWith("- ")) {
      const requiredName = trim.slice(2).trim();
      const requiredPath = requiredTarget.parentPath ? `${requiredTarget.parentPath}.${requiredName}` : requiredName;
      const field = fieldsByPath.get(requiredPath);
      if (field) {
        field.required = true;
      }
      continue;
    }
    if (requiredTarget && indent <= requiredTarget.indent) {
      requiredTarget = null;
    }

    const fieldMatch = trim.match(/^([A-Za-z_][A-Za-z0-9_-]*):$/);
    const schemaKeywords = new Set(["properties", "items", "additionalProperties", "required", "enum"]);
    const activePropertiesIndent = propertiesStack[propertiesStack.length - 1];
    if (fieldMatch && !schemaKeywords.has(fieldMatch[1]) && activePropertiesIndent !== undefined && indent > activePropertiesIndent) {
      const parent = childParentPath(fieldStack[fieldStack.length - 1]?.field);
      const field = {
        path: parent ? `${parent}.${fieldMatch[1]}` : fieldMatch[1],
        type: "object",
        format: "",
        enum: [],
        required: false,
        preserved: false,
      };
      fields.push(field);
      fieldsByPath.set(field.path, field);
      fieldStack.push({ indent, field });
      continue;
    }

    const field = nearestField(fieldStack, indent);
    if (field && trim.startsWith("type:")) {
      if (field.type === "object") {
        field.type = yamlValue(trim);
      }
    } else if (field && trim.startsWith("format:")) {
      field.format = yamlValue(trim);
    } else if (field && trim === "enum:") {
      enumTarget = { indent, field };
    } else if (field && trim === "x-kubernetes-preserve-unknown-fields: true") {
      field.preserved = true;
    } else if (trim === "required:") {
      requiredTarget = { indent, parentPath: childParentPath(field) };
    } else if (trim === "properties:") {
      propertiesStack.push(indent);
    }
  }

  return fields;
}

function extractPrinterColumns(lines) {
  const start = lines.findIndex((line) => line.trim() === "additionalPrinterColumns:");
  if (start < 0) {
    return [];
  }
  const columns = [];
  let current = null;

  for (let i = start + 1; i < lines.length; i++) {
    const trim = lines[i].trim();
    if (!trim) {
      continue;
    }
    if (countIndent(lines[i]) <= countIndent(lines[start])) {
      break;
    }
    if (trim.startsWith("- name:")) {
      current = { name: trim.slice("- name:".length).trim(), type: "", jsonPath: "" };
      columns.push(current);
    } else if (current && trim.startsWith("type:")) {
      current.type = yamlValue(trim);
    } else if (current && trim.startsWith("jsonPath:")) {
      current.jsonPath = yamlValue(trim);
    }
  }

  return columns;
}

function parseCRD(text, source) {
  const lines = text.split(/\r?\n/);
  const shortNames = [];
  const shortNameStart = lines.findIndex((line) => line.trim() === "shortNames:");
  if (shortNameStart >= 0) {
    for (let i = shortNameStart + 1; i < lines.length; i++) {
      const trim = lines[i].trim();
      if (countIndent(lines[i]) <= countIndent(lines[shortNameStart])) {
        break;
      }
      if (trim.startsWith("- ")) {
        shortNames.push(trim.slice(2).trim());
      }
    }
  }

  return {
    source,
    name: yamlValue(lines.find((line) => /^  name:/.test(line)) || "name: unknown"),
    group: yamlValue(lines.find((line) => line.trim().startsWith("group:")) || "group: unknown"),
    scope: yamlValue(lines.find((line) => line.trim().startsWith("scope:")) || "scope: unknown"),
    plural: yamlValue(lines.find((line) => line.trim().startsWith("plural:")) || "plural: unknown"),
    singular: yamlValue(lines.find((line) => line.trim().startsWith("singular:")) || "singular: unknown"),
    kind: yamlValue(lines.find((line) => /^    kind:/.test(line)) || "kind: unknown"),
    versions: lines.filter((line) => /^    - name:/.test(line)).map(yamlValue),
    shortNames,
    specFields: extractSchemaSectionFields(lines, "spec"),
    statusFields: extractSchemaSectionFields(lines, "status"),
    printerColumns: extractPrinterColumns(lines),
  };
}

function typeSourceForKind(kind) {
  const overrides = {
    AgentReliabilityPolicy: "api/v1alpha1/reliabilitypolicy_types.go",
  };
  return overrides[kind] || `api/v1alpha1/${kind.toLowerCase()}_types.go`;
}

function renderFieldList(fields, prefix) {
  if (fields.length === 0) {
    return ["- No fields declared in the generated CRD schema."];
  }

  return fields.map((field) => {
    const details = [field.type];
    if (field.format) {
      details.push(`format ${field.format}`);
    }
    if (field.enum.length > 0) {
      details.push(`enum ${field.enum.join(", ")}`);
    }
    if (field.required) {
      details.push("required");
    }
    if (field.preserved) {
      details.push("preserves unknown fields");
    }
    return `- \`${prefix}.${field.path}\` - ${details.join("; ")}`;
  });
}

async function apiReferenceMarkdown() {
  const crdDir = path.join(rootDir, "config/crd/bases");
  const files = (await readdir(crdDir)).filter((file) => file.endsWith(".yaml")).sort();
  const crds = [];

  for (const file of files) {
    const source = `config/crd/bases/${file}`;
    crds.push(parseCRD(await readFile(path.join(rootDir, source), "utf8"), source));
  }

  const lines = [
    "# AGScale API Reference",
    "",
    "Generated from `config/crd/bases/*.yaml`. AGScale resources are namespaced Kubernetes custom resources in API group `agscale.io`.",
    "",
    "Install CRDs:",
    "",
    "```bash",
    "kubectl apply -k crds",
    "kubectl api-resources | rg agscale.io",
    "```",
    "",
    "## Resource Index",
    "",
  ];

  for (const crd of crds) {
    lines.push(`- \`${crd.kind}\` - \`${crd.plural}.${crd.group}\` (${crd.scope})`);
  }

  for (const crd of crds) {
    const typeSource = typeSourceForKind(crd.kind);
    lines.push(
      "",
      `## ${crd.kind}`,
      "",
      `- API versions: \`${crd.versions.join("`, `")}\``,
      `- Resource: \`${crd.plural}\``,
      `- Singular: \`${crd.singular}\``,
      `- Scope: \`${crd.scope}\``,
      `- CRD source: \`${crd.source}\``,
      `- Go type source: \`${typeSource}\``,
    );
    if (crd.shortNames.length > 0) {
      lines.push(`- Short names: \`${crd.shortNames.join("`, `")}\``);
    }
    lines.push("", "### Spec Fields", "", ...renderFieldList(crd.specFields, "spec"));
    lines.push("", "### Status Fields", "", ...renderFieldList(crd.statusFields, "status"));
    if (crd.printerColumns.length > 0) {
      lines.push("", "### Printer Columns", "");
      for (const column of crd.printerColumns) {
        lines.push(`- ${column.name}: \`${column.jsonPath}\` (${column.type})`);
      }
    }
  }

  return lines.join("\n");
}

function pageShell({ title, description, body, activeSlug = "" }) {
  const navLinks = sourceDocs
    .map((doc) => {
      const href = `${doc.slug}.html`;
      const active = doc.slug === activeSlug ? ' aria-current="page"' : "";
      return `<a href="${href}"${active}>${escapeHtml(doc.title)}</a>`;
    })
    .join("\n");

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>${escapeHtml(title)} | AGScale Docs</title>
    <meta name="description" content="${escapeHtml(description)}">
    <link rel="stylesheet" href="../styles.css?v=${assetVersion}">
  </head>
  <body>
    <a class="skip-link" href="#main">Skip to content</a>
    <header class="site-header" aria-label="Primary navigation">
      <a class="brand" href="../index.html" aria-label="AGScale home">
        <span class="brand-mark" aria-hidden="true">AG</span>
        <span>AGScale</span>
      </a>
      <nav class="nav-links">
        <a href="../index.html">Home</a>
        <a href="./index.html">Docs</a>
        <a href="https://github.com/OrgPY/AGscale/releases">Releases</a>
        <a href="https://github.com/OrgPY/AGscale">GitHub</a>
      </nav>
    </header>
    <main id="main" class="docs-shell">
      <aside class="docs-sidebar" aria-label="Documentation navigation">
        <a class="docs-home-link" href="./index.html">Documentation</a>
        <nav>${navLinks}</nav>
      </aside>
      ${body}
    </main>
    <script src="../script.js?v=${assetVersion}"></script>
  </body>
</html>
`;
}

function indexPage() {
  const groups = [...new Set(sourceDocs.map((doc) => doc.group))];
  const groupHtml = groups
    .map((group) => {
      const cards = sourceDocs
        .filter((doc) => doc.group === group)
        .map((doc) => `<a class="doc-card" href="${doc.slug}.html">
          <span>${escapeHtml(group)}</span>
          <strong>${escapeHtml(doc.title)}</strong>
          <p>${escapeHtml(doc.summary)}</p>
        </a>`)
        .join("\n");
      return `<section class="doc-group" aria-labelledby="${slugify(group)}">
        <h2 id="${slugify(group)}">${escapeHtml(group)}</h2>
        <div class="docs-card-grid">${cards}</div>
      </section>`;
    })
    .join("\n");

  const body = `<article class="docs-article docs-index">
    <header class="docs-hero">
      <p class="eyebrow">AGScale documentation</p>
      <h1>Build and operate AI agent fleets on Kubernetes.</h1>
      <p>These docs are generated from the repository Markdown during the Pages deploy, so the public site follows the same runbooks and examples engineers use in the source tree.</p>
      <div class="hero-actions">
        <a class="button button-primary" href="operator-runbook.html">Start with the runbook</a>
        <a class="button button-secondary" href="architecture.html">Read the architecture</a>
      </div>
    </header>
    ${groupHtml}
  </article>`;

  return pageShell({
    title: "Documentation",
    description: "AGScale documentation, runbooks, demos, CLI usage, and operations guides.",
    body,
  });
}

function docPage(doc) {
  const headingLinks = doc.headings
    .filter((heading) => heading.level > 1 && heading.level < 4)
    .slice(0, 18)
    .map((heading) => `<a href="#${heading.id}">${escapeHtml(heading.text)}</a>`)
    .join("\n");

  const toc = headingLinks
    ? `<nav class="doc-toc" aria-label="On this page">
        <span>On this page</span>
        ${headingLinks}
      </nav>`
    : "";
  const footerLink = doc.source
    ? `<a href="https://github.com/OrgPY/AGscale/blob/main/${doc.source}">Edit this page on GitHub</a>`
    : `<a href="https://github.com/OrgPY/AGscale">Generated from repository source</a>`;

  const body = `<article class="docs-article doc-content">
    <div class="doc-meta">
      <a href="./index.html">Docs</a>
      <span>/</span>
      <span>${escapeHtml(doc.group)}</span>
    </div>
    ${toc}
    ${doc.html}
    <footer class="doc-footer">
      ${footerLink}
    </footer>
  </article>`;

  return pageShell({
    title: doc.title,
    description: doc.summary,
    body,
    activeSlug: doc.slug,
  });
}

async function main() {
  await rm(docsOutputDir, { recursive: true, force: true });
  await mkdir(docsOutputDir, { recursive: true });

  const generatedDocs = [
    {
      slug: "cli-reference",
      group: "Reference",
      summary: "Complete AGScale CLI command tree, usage strings, flags, aliases, and TUI commands.",
      content: cliReferenceMarkdown(),
      title: "AGScale CLI Reference",
    },
    {
      slug: "api-reference",
      group: "Reference",
      summary: "Kubernetes CRD reference generated from the AGScale OpenAPI schemas.",
      content: await apiReferenceMarkdown(),
      title: "AGScale API Reference",
    },
  ];

  const sourceFiles = await readdir(path.join(rootDir, "docs"));
  const markdownFiles = sourceFiles.filter((file) => file.endsWith(".md")).sort();
  const knownSources = new Set(sourceDocs.map((doc) => doc.source));
  const uncategorizedDocs = markdownFiles
    .map((file) => `docs/${file}`)
    .filter((source) => !knownSources.has(source))
    .map((source) => ({
      source,
      slug: slugify(path.basename(source, ".md")),
      group: "Reference",
      summary: "Additional AGScale repository documentation.",
    }));
  sourceDocs = [...sourceDocs, ...generatedDocs, ...uncategorizedDocs];

  for (const doc of sourceDocs) {
    const markdown = doc.content || (await readFile(path.join(rootDir, doc.source), "utf8"));
    const { body, headings } = renderMarkdown(markdown);
    const firstHeading = headings.find((heading) => heading.level === 1);
    doc.title = doc.title || firstHeading?.text || doc.slug;
    doc.html = body;
    doc.headings = headings;
  }

  await writeFile(path.join(docsOutputDir, "index.html"), indexPage());

  await Promise.all(
    sourceDocs.map((doc) => writeFile(path.join(docsOutputDir, `${doc.slug}.html`), docPage(doc))),
  );

  await writeFile(
    path.join(docsOutputDir, "manifest.json"),
    JSON.stringify(
      {
        generatedFrom: ["README.md", ...markdownFiles.map((file) => `docs/${file}`)],
        generatedReference: ["cli-reference", "api-reference"],
        pages: sourceDocs.map(({ source, slug, title }) => ({ source: source || "generated", slug, title })),
      },
      null,
      2,
    ),
  );
}

await main();
