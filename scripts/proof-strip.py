#!/usr/bin/env python3
"""proof-strip.py - the host wire strip for scripts/removal-proof.sh
(card t_b75df168, the shell-ring shrink wave 3).

The ULTIMATE REMOVAL PROOF builds the binary with every harness
capsule dir deleted. The host (cmd/omega) imports the capsules and
carries their wires. The binary-purity scrubber (nm-purity.sh) strips
ONE capsule at a time; at all-at-once the cascades over-match host
locals named like capsules (store, logger, trust) and eat function
headers, leaving un-compilable strands. So the all-at-once proof
strips the host EXPLICITLY: every wire site below is named and asserted
(an absent match reds the proof loudly - the strip is stale against
HEAD and fails before a wrong binary is built).

The kept vocabulary: agent/loop + api (the shell ring's own adapters),
datadir (the imported leaf, not a capsule), the host's own Config
types, and the agent need faces (trustFlags/trustGate,
agent.StoreProvider). The build is the arbiter; the fixup loop in
removal-proof.sh mops any strand the build still reports.

Run INSIDE the staged tree (cwd = the tree root):
    python3 proof-strip.py
"""

import re
import sys

MODULES = ["approval", "browser", "compactor", "continuous", "cron",
           "delegate", "files", "kanban", "logging", "mcp", "memory",
           "notifications", "process", "prompt", "provider", "skills",
           "store", "terminal", "tools", "trust", "verdict"]
KEEP = ["datadir"]

IMPORT_RE = re.compile(
    r'^\t(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"github\.com/EndoTheDev/omega/harness/(\w+)"\s*(?://.*)?$')


class Stale(Exception):
    pass


class Host:
    """one staged host file, line-editable with asserted anchors"""

    def __init__(self, path):
        self.path = path
        with open(path, encoding="utf-8", newline="") as f:
            self.lines = f.read().split("\n")

    def find(self, needle, desc, after=-1):
        hits = [i for i, ln in enumerate(self.lines) if needle in ln and i > after]
        if not hits:
            raise Stale(f"strip stale ({desc}): {needle!r} not found")
        if len(hits) > 1:
            raise Stale(f"strip stale ({desc}): {needle!r} matches {len(hits)} times")
        return hits[0]

    def close_of(self, start):
        """the line index where the block opened at start returns to the
        opener level (the head line may open the block itself)"""
        depth = 0
        for i in range(start, len(self.lines)):
            depth += self.lines[i].count("{") - self.lines[i].count("}")
            if depth <= 0 and i > start:
                return i
        raise Stale(f"strip stale: the block at {self.path}:{start + 1} never closes")

    def drop_imports(self, desc):
        out, dropped = [], []
        for ln in self.lines:
            m = IMPORT_RE.match(ln)
            if m and m.group(1) in MODULES and m.group(1) not in KEEP:
                dropped.append(m.group(1))
                continue
            out.append(ln)
        if not dropped:
            raise Stale(f"strip stale ({desc}): no capsule import lines found")
        self.lines = out

    def del_block(self, needle, desc):
        """delete the brace-matched block whose head line carries the needle"""
        h = self.find(needle, desc)
        if "{" not in self.lines[h]:
            # a wrapped head: the opener sits close behind
            for j in range(h, min(h + 8, len(self.lines))):
                if "{" in self.lines[j]:
                    h = j
                    break
            else:
                raise Stale(f"strip stale ({desc}): no opener near the head")
        c = self.close_of(h)
        del self.lines[h:c + 1]

    def del_head_through_block(self, head, anchor, desc):
        """delete head .. the brace-matched close of the FIRST anchor
        after head (the anchor's line opens a block)"""
        h = self.find(head, desc)
        a = self.find(anchor, desc + " (anchor)", after=h)
        c = self.close_of(a)
        del self.lines[h:c + 1]

    def replace_line(self, needle, newline, desc):
        h = self.find(needle, desc)
        self.lines[h] = newline

    def drop_line(self, needle, desc):
        h = self.find(needle, desc)
        del self.lines[h]

    def splice(self, head, extra, newlines, desc):
        """delete head .. head+extra lines, splice newlines in place"""
        h = self.find(head, desc)
        del self.lines[h:h + 1 + extra]
        self.lines[h:h] = newlines

    def add_after(self, needle, newlines, desc):
        h = self.find(needle, desc)
        self.lines[h + 1:h + 1] = newlines

    def replace_span(self, head, tail, newlines, desc):
        """delete head .. tail inclusive, splice newlines in place"""
        h = self.find(head, desc)
        e = self.find(tail, desc + " (tail)", after=h)
        del self.lines[h:e + 1]
        self.lines[h:h] = newlines

    def replace_func(self, header, newlines, desc):
        h = self.find(header, desc)
        c = self.close_of(h)
        self.lines[h:c + 1] = newlines

    def assert_pure(self, desc):
        leaked = [ln.strip() for ln in self.lines
                  if (m := IMPORT_RE.match(ln)) and m.group(1) in MODULES
                  and m.group(1) not in KEEP]
        if leaked:
            raise Stale(f"strip leak ({desc}): {leaked[:4]}")

    def save(self):
        with open(self.path, "w", encoding="utf-8", newline="") as f:
            f.write("\n".join(self.lines))
        print(f"strip: {self.path}: ok")


def strip_host():
    # ---- main.go -----------------------------------------------------
    f = Host("cmd/omega/main.go")
    f.drop_imports("main imports")
    f.del_block("if err := migrateAgentStores(home); err != nil {",
                "main: the boot-order store migration call")
    f.replace_func("func buildPlugins(", [
        "func buildPlugins(_ Config) ([]agent.Plugin, error) {",
        "\t// the every-capsule-removed host: the agent loop + the shell",
        "\t// ring, nothing else - the removal proof's plugin list",
        "\treturn []agent.Plugin{",
        "\t\tloop.NewPlugin(),",
        "\t\tapi.NewPlugin(),",
        "\t}, nil",
        "}",
    ], "main: buildPlugins")
    f.del_head_through_block("\tvar setAgents func([]string)",
                             "for _, p := range plugins {",
                             "main: the setAgents/setFilesWorkspace hook discovery")
    f.del_block("if st2 == nil {", "main: the default-supply call")
    f.replace_func("func openDefaultSupply(", [], "main: openDefaultSupply")
    f.assert_pure("main")
    f.save()

    # ---- config.go ---------------------------------------------------
    f = Host("cmd/omega/config.go")
    f.drop_imports("config imports")
    f.replace_line("continuous.Config",
                   "\t// the every-capsule-removed host: the continuous section rode its capsule out (shrink wave 2)",
                   "config: the Continuous field")
    f.replace_line("\t\t\tRetentionDays:       store.DefaultHygiene().RetentionDays,",
                   "\t\t\tRetentionDays:       90, // the store capsule's DefaultRetentionDays",
                   "config: the retention default")
    f.replace_line("\t\t\tSummarizeAfterHours: store.DefaultHygiene().SummarizeAfterHours,",
                   "\t\t\tSummarizeAfterHours: 24, // the store capsule's DefaultSummarizeAfterHours",
                   "config: the summarize default")
    f.replace_line("\t\t\tNudgeInterval: skills.DefaultNudgeInterval,",
                   "\t\t\tNudgeInterval: 15, // the skills capsule's DefaultNudgeInterval",
                   "config: the nudge default")
    f.replace_func("func (c Config) ConfigSection(", [
        "func (c Config) ConfigSection(name string) (any, bool) {",
        "\t// the every-capsule-removed host: the shell's own sections stay,",
        "\t// every capsule section rides its capsule out",
        "\tswitch name {",
        "\tcase \"http_channel\":",
        "\t\treturn api.Config{Port: c.Server.Port, Bind: c.Server.Bind, RunTimeout: c.Server.RunTimeout, TitleModel: c.TitleModel, WebTools: agentapi.WebToolsConfig{APIKey: c.Provider.APIKey, BaseURL: webBaseURL(c.Web.BaseURL, c.Provider.Host)}}, true",
        "\tcase \"compactor\":",
        "\t\treturn c.Compaction, true",
        "\t}",
        "\treturn nil, false",
        "}",
    ], "config: ConfigSection")
    f.assert_pure("config")
    f.save()

    # ---- settings.go -------------------------------------------------
    f = Host("cmd/omega/settings.go")
    f.drop_imports("settings imports")
    f.del_head_through_block("svc := pctx.Services().Resolve(\"compactor\")",
                             "if c, ok := svc.(*compactor.Compactor); ok {",
                             "settings: hotApply's compactor tail")
    f.del_head_through_block("c, ok := s.pctx.Services().Resolve(\"compactor\")",
                             "if w := c.AutoWindow(); w > 0 {",
                             "settings: the schema's compactor hint")
    f.assert_pure("settings")
    f.save()

    # ---- agents.go ---------------------------------------------------
    f = Host("cmd/omega/agents.go")
    f.drop_imports("agents imports")
    f.del_block("if err := migrateAgentStores(home); err != nil {",
                "agents: the store migration call")
    f.replace_func("func migrateAgentStores(", [], "agents: migrateAgentStores")
    f.assert_pure("agents")
    f.save()

    # ---- export.go ---------------------------------------------------
    f = Host("cmd/omega/export.go")
    f.drop_imports("export imports")
    f.replace_func("func cmdExport(", [
        "// cmdExport writes the session's messages as JSONL to the output",
        "// path (or stdout with \"-\"). The every-capsule-removed host carries",
        "// no store capsule - the sessions live in its tables.",
        "func cmdExport(configPath string, rest []string) error {",
        "\treturn fmt.Errorf(\"this build carries no store capsule - the export surface lives with it\")",
        "}",
    ], "export: cmdExport")
    f.assert_pure("export")
    f.save()

    # ---- insights.go -------------------------------------------------
    f = Host("cmd/omega/insights.go")
    f.drop_imports("insights imports")
    f.replace_func("func cmdInsights(", [
        "// cmdInsights prints the insights report over the last N days. The",
        "// every-capsule-removed host carries no store capsule - the data",
        "// lives with it.",
        "func cmdInsights(configPath string, args []string) error {",
        "\treturn fmt.Errorf(\"this build carries no store capsule - the insights surface lives with it\")",
        "}",
    ], "insights: cmdInsights")
    f.assert_pure("insights")
    f.save()

    # ---- loop.go -----------------------------------------------------
    f = Host("cmd/omega/loop.go")
    f.drop_imports("loop imports")
    f.replace_span("vp, ok := lo.pctx.Services().Resolve(\"verdicts\")",
                   "return handled", [
        "\t// the every-capsule-removed host: the verdict capsule is not in",
        "\t// this build - the inline answer rides it out",
        "\treturn false",
    ], "loop: the inline verdict answer")
    f.assert_pure("loop")
    f.save()

    # ---- setup.go ----------------------------------------------------
    f = Host("cmd/omega/setup.go")
    f.drop_imports("setup imports")
    f.replace_span("prov = provider.NewProvider(typ, modelName, baseURL, apiKey)",
                   "return prov", [
        "\t// the every-capsule-removed host: no provider capsule - the",
        "\t// wizard's live model list reports unavailable (nil lister)",
        "\treturn prov",
    ], "setup: the provider hook")
    f.assert_pure("setup")
    f.save()

    # ---- home.go -----------------------------------------------------
    f = Host("cmd/omega/home.go")
    f.drop_imports("home imports")
    f.replace_span("// Upgrade path: a legacy home-root trust.yaml",
                   "trust.Bootstrap(home)", [], "home: the trust bootstrap")
    f.assert_pure("home")
    f.save()

    print("strip: every wire site asserted and applied")


if __name__ == "__main__":
    try:
        strip_host()
    except Stale as e:
        print(f"strip FAIL: {e}", file=sys.stderr)
        sys.exit(1)
