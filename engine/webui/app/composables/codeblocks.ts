// omega webui: post-render enhancement for fenced code blocks. The
// markdown renderer emits plain <pre><code>; this pass wraps each block
// in a header container (language + line count + copy + collapse) after
// a render lands in the DOM. Idempotent: already-wrapped blocks are
// skipped, so it can run on every streaming render tick without piling
// up wrappers. Collapse state is index-keyed and owned by the caller,
// because v-html replacement tears the wrapped DOM down on each tick.

// "language-go" / "hljs language-go" -> "go"; anything else -> "code".
export function codeLang(className: string): string {
  const m = /language-([\w-]+)/.exec(className);
  return m ? m[1] : "code";
}

// Line count for the header; trailing newlines don't count, empty is 0.
export function codeLines(text: string): number {
  const trimmed = text.replace(/\n+$/, "");
  return trimmed === "" ? 0 : trimmed.split("\n").length;
}

export function decorateCodeblocks(
  root: HTMLElement | null,
  collapsed: Set<number>,
): void {
  if (!root) return;
  const pres = root.querySelectorAll("pre");
  let index = 0;
  for (let i = 0; i < pres.length; i++) {
    const pre = pres[i] as HTMLElement;
    const code = pre.firstElementChild as HTMLElement | null;
    if (pre.parentElement?.classList.contains("codeblock")) {
      index++;
      continue;
    }
    const text = code?.textContent ?? "";

    const box = document.createElement("div");
    box.className = "codeblock";
    if (collapsed.has(index)) box.classList.add("collapsed");

    const head = document.createElement("div");
    head.className = "codeblock-head";
    const label = document.createElement("span");
    label.className = "codeblock-lang";
    label.textContent = codeLang(code?.className ?? "");
    const count = document.createElement("span");
    count.className = "codeblock-lines";
    count.textContent = `${codeLines(text)} lines`;
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "codeblock-copy";
    copy.textContent = "copy";
    head.append(label, count, copy);

    pre.replaceWith(box);
    box.append(head, pre);

    // code.textContent is the raw source - highlight spans, escapes and
    // any html:false fallout never reach the clipboard.
    copy.addEventListener("click", (ev) => {
      ev.stopPropagation();
      void navigator.clipboard.writeText(code?.textContent ?? "");
      copy.textContent = "copied";
      window.setTimeout(() => {
        copy.textContent = "copy";
      }, 1200);
    });
    head.addEventListener("click", () => {
      box.classList.toggle("collapsed");
      if (box.classList.contains("collapsed")) collapsed.add(index);
      else collapsed.delete(index);
    });
    index++;
  }
}