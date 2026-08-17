// Minimal client-side Markdown renderer for the live report preview. Escapes
// HTML first, then applies a safe CommonMark subset.
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function inline(s: string): string {
  return s
    .replace(/`([^`]+)`/g, '<code class="bg-black px-1 rounded text-vault-gold">$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\*([^*]+)\*/g, "<em>$1</em>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a class="text-vault-red underline" href="$2">$1</a>');
}

export function renderMarkdown(md: string): string {
  const lines = md.replace(/\r\n/g, "\n").split("\n");
  let html = "";
  let inCode = false;
  let inList = false;
  for (const raw of lines) {
    if (raw.startsWith("```")) {
      if (inCode) {
        html += "</code></pre>";
        inCode = false;
      } else {
        html += '<pre class="bg-black rounded p-2 overflow-auto my-2"><code class="terminal-output">';
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      html += escapeHtml(raw) + "\n";
      continue;
    }
    const line = raw.trim();
    if (!line) {
      if (inList) {
        html += "</ul>";
        inList = false;
      }
      continue;
    }
    if (line.startsWith("- ") || line.startsWith("* ")) {
      if (!inList) {
        html += '<ul class="list-disc pl-5 my-2">';
        inList = true;
      }
      html += `<li>${inline(escapeHtml(line.slice(2)))}</li>`;
      continue;
    }
    if (inList) {
      html += "</ul>";
      inList = false;
    }
    if (line.startsWith("### ")) html += `<h3 class="font-display text-base mt-3">${inline(escapeHtml(line.slice(4)))}</h3>`;
    else if (line.startsWith("## ")) html += `<h2 class="font-display text-lg mt-4 text-vault-gold">${inline(escapeHtml(line.slice(3)))}</h2>`;
    else if (line.startsWith("# ")) html += `<h1 class="font-display text-xl mt-4">${inline(escapeHtml(line.slice(2)))}</h1>`;
    else if (line === "---") html += '<hr class="border-vault-slate my-3"/>';
    else html += `<p class="my-1 text-vault-white/80">${inline(escapeHtml(line))}</p>`;
  }
  if (inList) html += "</ul>";
  if (inCode) html += "</code></pre>";
  return html;
}
