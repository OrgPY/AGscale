import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = path.resolve(process.argv[2] || path.join(rootDir, ".pages-dist"));
const docsOutputDir = path.join(outputDir, "docs");

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
    <link rel="stylesheet" href="../styles.css">
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
    <script src="../script.js"></script>
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

  const body = `<article class="docs-article doc-content">
    <div class="doc-meta">
      <a href="./index.html">Docs</a>
      <span>/</span>
      <span>${escapeHtml(doc.group)}</span>
    </div>
    ${toc}
    ${doc.html}
    <footer class="doc-footer">
      <a href="https://github.com/OrgPY/AGscale/blob/main/${doc.source}">Edit this page on GitHub</a>
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
  sourceDocs = [...sourceDocs, ...uncategorizedDocs];

  for (const doc of sourceDocs) {
    const markdown = await readFile(path.join(rootDir, doc.source), "utf8");
    const { body, headings } = renderMarkdown(markdown);
    const firstHeading = headings.find((heading) => heading.level === 1);
    doc.title = firstHeading?.text || doc.slug;
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
        pages: sourceDocs.map(({ source, slug, title }) => ({ source, slug, title })),
      },
      null,
      2,
    ),
  );
}

await main();
