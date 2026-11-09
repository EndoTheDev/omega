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

The loud unknown-wire gate: after every file's asserted edits, a
capsule-qual sweep (strings and comments blanked) reds on any dot-qual
on a capsule package the strip list does not name. The build fixup
must never mop a wire the strip does not know about - an unlisted call
site reds HERE (the next arc cannot silently run through the fixup's
`undefined:` catch-all and eat its own wire). The sweep runs on EVERY
host file: main.go and loop.go carry host LOCALS named like capsules
(store, cron, logger), so there the gate exempts only the curated
KNOWN_LOCAL_QUALS spellings (qual.method on the local - lexically
identical to a package call); any other capsule qual reds.

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

# The loud unknown-wire gate's two patterns: any dot-qual on a capsule
# package whose import was stripped (the bare scan), and the same qual
# with its method name (the allowlist-aware scan for the two files
# that carry capsule-named host locals). Built once, after MODULES.
_QUAL_ALTS = "|".join(m for m in MODULES if m not in KEEP)
QUALS_RE = re.compile(r"\b(?<![\w.])(" + _QUAL_ALTS + r")\.")
QUAL_FULL_RE = re.compile(r"\b(?<![\w.])(" + _QUAL_ALTS + r")\.(\w+)")

# main.go and loop.go carry host LOCALS named like capsules (store,
# cron, logger): a method call on the local - `store.Close()` - is
# lexically identical to a package call, so per file the gate exempts
# exactly these qual.method spellings on the local value (store.Close
# and store.GetSession are the newAgent local's agent.StoreProvider
# calls; cron.Stop and logger.* are the host's own cleanup calls; the
# tag's loop.go carries only store.Close). A stale
# entry is harmless (the code no longer matches it); a new local named
# like a capsule reds loudly and joins this list - a loud false
# positive is this gate's designed failure mode.
KNOWN_LOCAL_QUALS = {
    "cmd/omega/main.go": {"store.Close", "logger.Close",
                          "logger.Printf", "cron.Stop", "store.GetSession"},
    "cmd/omega/loop.go": {"store.Close"},
}

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

    @staticmethod
    def blank_strings_comments(text):
        """blank string bodies, rune bodies and comments with spaces so
        the capsule-qual scan sees only real code tokens. Every consumed
        char emits exactly one space - except newlines, which ride
        through inside raw strings too - so line AND column offsets hold
        exactly (including inside string bodies); an unterminated
        quote leaves its newline to the next loop pass (no line merge -
        and unreachable on a compiling tree anyway)."""
        out, i, n = [], 0, len(text)
        while i < n:
            c = text[i]
            if c == '"' or c == "'":
                q, i = c, i + 1
                out.append(" ")  # the opening quote
                while i < n:
                    if text[i] == "\\" and i + 1 < n:
                        out.extend((" ", " "))  # the escape pair
                        i += 2
                        continue
                    if text[i] == q or text[i] == "\n":
                        break
                    out.append(" ")  # one space per interior char
                    i += 1
                if i < n and text[i] == q:
                    out.append(" ")  # the closing quote
                    i += 1
                # unterminated (text[i] is the newline or EOF): the
                # byte is not consumed here - the newline rides the
                # next loop iteration, so lines never merge.
            elif c == "`":
                j = text.find("`", i + 1)
                if j < 0:
                    out.append(c)
                    i += 1
                    continue
                # one space per char - except newlines ride through, so
                # the gate's LINE numbers hold across raw strings too
                out.extend(ch if ch == "\n" else " " for ch in text[i:j + 1])
                i = j + 1
            elif text.startswith("//", i):
                j = text.find("\n", i)
                j = n if j < 0 else j
                out.append(" " * (j - i))
                i = j
            elif text.startswith("/*", i):
                j = text.find("*/", i + 2)
                j = n if j < 0 else j + 2
                out.append(" " * (j - i))
                i = j
            else:
                out.append(c)
                i += 1
        return "".join(out)

    def assert_no_capsule_quals(self, desc):
        """The loud unknown-wire gate: red on any capsule package
        qualifier the strip list does not name (a new arc's call site in
        a Host file). The build arbiter alone is NOT loud enough - the
        proof's build fixup eats `undefined: x` lines silently, so an
        uncovered wire MUST red here, before the build ever runs. The
        qual must not ride a selector (lo.store. is a host field, not
        the package). The gate runs on EVERY host file; main.go and
        loop.go carry capsule-named LOCALS, so there only the curated
        KNOWN_LOCAL_QUALS spellings (qual.method on the local value)
        are exempt - anything else reds."""
        allowed = KNOWN_LOCAL_QUALS.get(self.path, ())
        text = self.blank_strings_comments("\n".join(self.lines))
        hits = [(i + 1, ln.strip()[:100]) for i, ln in enumerate(text.split("\n"))
                if QUALS_RE.search(ln)
                and not all(m in allowed for m in
                            (mm.group(1) + "." + mm.group(2)
                             for mm in QUAL_FULL_RE.finditer(ln)))]
        if hits:
            raise Stale(f"strip stale ({desc}): uncovered capsule wire "
                        f"(dot-qual on a capsule package): {hits[:4]}")

    def prune_unused_imports(self, desc):
        """drop every import whose package name the body no longer
        references dot-wise (the replace_func rewrites strand their
        imports); a kept-but-referenced import is safe debris, a
        dropped-and-referenced one is the strand that eats functions"""
        body = "\n".join(self.lines)
        imp = re.compile(r'^\t(?:([A-Za-z_][A-Za-z0-9_]*)\s+)?"([^"]+)"')
        out, dropped = [], []
        for ln in self.lines:
            m = imp.match(ln)
            if m:
                alias = m.group(1) or m.group(2).rsplit("/", 1)[-1]
                if not re.search(r'\b' + re.escape(alias) + r'\.', body):
                    dropped.append(m.group(1) or m.group(2))
                    continue
            out.append(ln)
        if not dropped:
            raise Stale(f"strip stale ({desc}): no unused imports to prune")
        self.lines = out
        print(f"strip: pruned imports ({desc}): {', '.join(dropped)}")

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
    f.del_block("\tfor _, p := range plugins {",
                "main: the setAgents/setFilesWorkspace hook discovery (the vars stay nil, the type assert dies)")
    f.replace_func("func openDefaultSupply(", [
        "// openDefaultSupply is the store seam's HOST fallback. In the",
        "// real build it opens the file db the store capsule's package",
        "// points at (the three-layer sessions law). In the",
        "// every-capsule-removed build the store package is gone - the",
        "// sqlite driver rides it out - so the host fallback is the",
        "// in-memory proof supply (cmd/omega/proofstore.go, written by",
        "// scripts/proof-strip.py): the chat face answers, persistence",
        "// is the store capsule's opinion and rides out with it.",
        "// openDefaultSupply - the host fallback (the in-memory proof supply)",
        "func openDefaultSupply(pctx *agent.Context, logger agent.LoggerProvider) (agent.StoreProvider, error) {",
        "\tif logger != nil {",
        "\t\tlogger.Printf(\"omega: no store capsule loaded - serving the host's in-memory proof supply\")",
        "\t}",
        "\ts := newProofSupply()",
        "\tpctx.ApplyOffer(\"store\", \"host\", s)",
        "\treturn s, nil",
        "}",
    ], "main: openDefaultSupply (the in-memory proof supply)")
    f.replace_func("func wireCaptioner(", [
        "func wireCaptioner(cfg Config, pctx *agent.Context) {",
        "\t// the every-capsule-removed host: the captioner rides the provider",
        "\t// capsule out - the vision knob answers no captioner",
        "}",
    ], "main: wireCaptioner (the captioner is the provider capsule's type)")
    f.drop_line("ctx = agent.WithTurnSink(ctx, continuous.WrapWakeSink(agent.RunTurnSink(ctx)))",
                "main: the wake wrap")
    f.add_after("agent.NewStoreTurnSink(store, sessionID,", [
        "\t\t// the every-capsule-removed host: the wake-report shape rode",
        "\t\t// the continuous capsule out (the shrink wave 2)",
    ], "main: the wake wrap comment")
    # the provider seam's host stub: no provider capsule - the config's
    # model_name still answers the face and the stream carries the
    # stub's error end (failures are stream events, never Go errors).
    # Inserted BEFORE NewFromContext, which binds the need at birth.
    f.add_after("wireCaptioner(cfg, ctx)", [
        "	if ctx.Services().Resolve(\"provider\") == nil {",
        "		ctx.ApplyOffer(\"provider\", \"host\", newProofProvider())",
        "	}",
    ], "main: the provider stub offer")

    # the loud unknown-wire gate runs here too: newAgent's store/logger
    # locals and cron.Stop() are the curated KNOWN_LOCAL_QUALS
    # exemptions; any other capsule qual (e.g. a new arc's call site in
    # buildPlugins or newAgent) reds before the fixup can mop it.
    f.assert_no_capsule_quals("main")
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
        "// modeValid is the host's approvals.mode vocabulary - the strip's",
        "// inlining of approval.ValidMode (the approval capsule rides out).",
        "func modeValid(m string) bool {",
        "\tswitch m {",
        "\tcase \"\", \"off\", \"manual\", \"smart\":",
        "\t\treturn true",
        "\t}",
        "\treturn false",
        "}",
        "",
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
    ], "config: ConfigSection (+ modeValid)")
    f.replace_line("if !approval.ValidMode(c.Approvals.Mode) {",
                   "if !modeValid(c.Approvals.Mode) {", "config: approvals.mode validation")
    # the registry arc (ruling B, card t_7dcd4280): ConfigSection's
    # capsule sections moved into per-capsule helper funcs. The shell
    # helpers stay (compactionSection, webBaseURL, approvalSlot are
    # host-only); the capsule section helpers ride their capsules out.
    f.replace_func("func approvalSection(", [
        "// the every-capsule-removed host: the approvals section rides",
        "// the approval capsule out (approvalSlot stays - it is host-only",
        "// wiring: c.Provider/c.Providers and the agent slot parser).",
    ], "config: approvalSection rides the capsule out")
    f.replace_func("func sessionsSection(", [
        "// the every-capsule-removed host: the store capsule's hygiene",
        "// section rides it out (the hygiene pass is the store capsule's",
        "// opinion; the host's in-memory proof supply keeps no hygiene).",
    ], "config: sessionsSection rides the capsule out")
    f.replace_func("func skillsSection(", [
        "// the every-capsule-removed host: the skills capsule's section",
        "// rides it out (the curator consult slot is capsule wiring).",
    ], "config: skillsSection rides the capsule out")
    f.assert_no_capsule_quals("config")
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
    f.replace_line("if !approval.ValidMode(m) {",
                   "if !modeValid(m) {", "settings: approvals.mode validation")
    # the registry arc's hot-apply tails: the approvals/store/skills
    # resolve blocks type-assert capsule types - they ride their
    # capsules out. In the every-capsule-removed build Resolve is nil
    # for every seam, so the honest shape is the block absent.
    f.del_block('pctx.Services().Resolve("approvals").(*approval.Gate)',
                "settings: hotApply's approvals tail")
    f.del_block('pctx.Services().Resolve("store").(*store.Store)',
                "settings: hotApply's store tail")
    f.del_block('pctx.Services().Resolve("skills").(curatorSlotSetter)',
                "settings: hotApply's skills tail (skillsSection rode the capsule out)")
    f.assert_no_capsule_quals("settings")
    f.assert_pure("settings")
    f.save()

    # ---- agents.go ---------------------------------------------------
    f = Host("cmd/omega/agents.go")
    f.drop_imports("agents imports")
    f.del_block("if err := migrateAgentStores(home); err != nil {",
                "agents: the store migration call")
    f.replace_func("func migrateAgentStores(", [], "agents: migrateAgentStores")
    f.assert_no_capsule_quals("agents")
    f.assert_pure("agents")
    f.save()

    # ---- export.go ---------------------------------------------------
    f = Host("cmd/omega/export.go")
    f.drop_imports("export imports")
    # the export surface is store-shaped end to end: every helper rides
    # the store capsule out with cmdExport.
    f.replace_func("func exportMessages(", [], "export: exportMessages rides the store")
    f.replace_func("func resolveSessionCLI(", [], "export: resolveSessionCLI rides the store")
    f.replace_func("func cmdExport(", [
        "// cmdExport writes the session's messages as JSONL to the output",
        "// path (or stdout with \"-\"). The every-capsule-removed host carries",
        "// no store capsule - the sessions live in its tables.",
        "func cmdExport(configPath string, rest []string) error {",
        "\treturn fmt.Errorf(\"this build carries no store capsule - the export surface lives with it\")",
        "}",
    ], "export: cmdExport")
    f.prune_unused_imports("export")
    f.assert_no_capsule_quals("export")
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
    f.prune_unused_imports("insights")
    f.assert_no_capsule_quals("insights")
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
    f.assert_no_capsule_quals("loop")
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
    f.assert_no_capsule_quals("setup")
    f.assert_pure("setup")
    f.save()

    # ---- home.go -----------------------------------------------------
    f = Host("cmd/omega/home.go")
    f.drop_imports("home imports")
    f.replace_span("// Upgrade path: a legacy home-root trust.yaml",
                   "trust.Bootstrap(home)", [], "home: the trust bootstrap")
    f.assert_no_capsule_quals("home")
    f.assert_pure("home")
    f.save()

    # the host fallback's own store: the in-memory proof supply
    # (openDefaultSupply above resolves it when no capsule offers the
    # "store" seam - cmd/omega/proofstore.go), and the provider seam's
    # host stub (cmd/omega/proofprovider.go)
    write_proofstore()
    write_proofprovider()

    print("strip: every wire site asserted and applied")


PROOFSTORE = '''package main

// The proof supply (scripts/proof-strip.py, card t_b75df168): the
// store seam's HOST fallback in the every-capsule-removed build. The
// store capsule carries the sqlite driver; deleted from the build it
// cannot open a file, so the fallback keeps the sessions in memory:
// the chat face answers, persistence is the store capsule's opinion
// and rides out with it. Same contract as the capsule-owned store -
// linear rows in append order, core.DeriveTree for the active view -
// so the session-existence floor and the transcript face answer
// exactly as they would against a real store.

import (
	"context"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

type proofSupply struct {
	mu        sync.Mutex
	seq       int64
	sessions  map[string]core.Session
	rows      map[string][]core.StoredMessage
}

func newProofSupply() *proofSupply {
	return &proofSupply{
		sessions: map[string]core.Session{},
		rows:     map[string][]core.StoredMessage{},
	}
}

func (p *proofSupply) Open(dsn string) error  { return nil }
func (p *proofSupply) Close() error           { return nil }

func (p *proofSupply) CreateSession(_ context.Context, id, parentID, label string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := core.NowISO()
	p.sessions[id] = core.Session{ID: id, ParentID: parentID, Label: label, CreatedAt: now, UpdatedAt: now}
	return nil
}

func (p *proofSupply) GetSession(_ context.Context, id string) (core.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[id]
	if !ok {
		return core.Session{}, fmt.Errorf("session not found: %s", id)
	}
	return s, nil
}

func (p *proofSupply) ListSessions(context.Context) ([]core.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]core.Session, 0, len(p.sessions))
	for _, s := range p.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (p *proofSupply) DeleteSession(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, id)
	delete(p.rows, id)
	return nil
}

func (p *proofSupply) UpdateSession(_ context.Context, id, label string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	s.Label = label
	s.UpdatedAt = core.NowISO()
	p.sessions[id] = s
	return nil
}

func (p *proofSupply) AppendMessage(_ context.Context, sessionID string, msg core.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := core.NowISO()
	p.seq++
	p.rows[sessionID] = append(p.rows[sessionID],
		core.StoredMessage{ID: p.seq, Message: msg, CreatedAt: now})
	if s, ok := p.sessions[sessionID]; ok {
		s.UpdatedAt = now
		p.sessions[sessionID] = s
	}
	return nil
}

func (p *proofSupply) proofRows(sessionID string) []core.StoredMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]core.StoredMessage(nil), p.rows[sessionID]...)
}

func (p *proofSupply) GetMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	rows := p.proofRows(sessionID)
	out := make([]core.Message, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Message)
	}
	return out, nil
}

func (p *proofSupply) GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	return p.proofRows(sessionID), nil
}

func (p *proofSupply) GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error) {
	return core.DeriveTree(p.proofRows(sessionID)), nil
}

func (p *proofSupply) GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	view, err := p.GetActiveView(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]core.Message, 0, len(view.Path))
	for _, r := range view.Path {
		out = append(out, r.Message)
	}
	return out, nil
}

func (p *proofSupply) GetSessionTree(context.Context) ([]*core.SessionNode, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*core.SessionNode, 0, len(p.sessions))
	for _, s := range p.sessions {
		out = append(out, &core.SessionNode{Session: s})
	}
	return out, nil
}

func (p *proofSupply) GetAncestorMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	return p.GetMessages(ctx, sessionID)
}

func (p *proofSupply) SearchMessages(context.Context, string) ([]core.SearchResult, error) {
	return nil, nil
}

func (p *proofSupply) SearchSessions(context.Context, string) ([]core.SearchResult, error) {
	return nil, nil
}

func (p *proofSupply) ComputeInsights(context.Context, int) (*core.Insights, error) {
	return &core.Insights{}, nil
}

func (p *proofSupply) CountMessages(_ context.Context, sessionID string) (int, error) {
	rows := p.proofRows(sessionID)
	return len(rows), nil
}

func (p *proofSupply) SessionCounts(context.Context) (map[string]core.SessionCounts, error) {
	return map[string]core.SessionCounts{}, nil
}

func (p *proofSupply) AddSessionTokens(_ context.Context, sessionID string, prompt, completion int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	s.PromptTokens += prompt
	s.CompletionTokens += completion
	p.sessions[sessionID] = s
	return nil
}

func (p *proofSupply) SetSessionLastRun(_ context.Context, sessionID string, stats *core.LastRunStats) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	s.LastRun = stats
	p.sessions[sessionID] = s
	return nil
}

func (p *proofSupply) SetSessionPinned(_ context.Context, sessionID string, pinned bool) error {
	return p.mutate(sessionID, func(s *core.Session) { s.Pinned = pinned })
}

func (p *proofSupply) SetSessionRead(_ context.Context, sessionID string) error {
	return p.mutate(sessionID, func(s *core.Session) { s.LastReadAt = core.NowISO() })
}

func (p *proofSupply) SetSessionArchived(_ context.Context, sessionID string, archived bool) error {
	return p.mutate(sessionID, func(s *core.Session) { s.Archived = archived })
}

func (p *proofSupply) SetSessionTitle(_ context.Context, sessionID, title string, final bool) error {
	return p.mutate(sessionID, func(s *core.Session) {
		s.Label = title
		if final {
			s.TitleState = 2
		} else if s.TitleState == 0 {
			s.TitleState = 1
		}
	})
}

func (p *proofSupply) SessionModels(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// SetSessionModel / SetSessionProvider / SetSessionWorkspace are the
// optional context seam (agent.SessionContextStore): the proof supply
// implements it, so the model-picker, the provider-instance pin and
// the draft's workspace intent answer too. The provider-instance pin
// (card t_7dcd4280) resolves per run against the live config - the
// proof build carries no provider capsule, the pin stores a name
// datum like any real store does.
func (p *proofSupply) SetSessionModel(_ context.Context, sessionID, model string) error {
	return p.mutate(sessionID, func(s *core.Session) { s.Model = model })
}

func (p *proofSupply) SetSessionProvider(_ context.Context, sessionID, provider string) error {
	return p.mutate(sessionID, func(s *core.Session) { s.ProviderName = provider })
}

func (p *proofSupply) SetSessionWorkspace(_ context.Context, sessionID, dir string) error {
	return p.mutate(sessionID, func(s *core.Session) { s.Workspace = dir })
}

func (p *proofSupply) mutate(sessionID string, mut func(*core.Session)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	mut(&s)
	p.sessions[sessionID] = s
	return nil
}

var _ agent.StoreProvider = (*proofSupply)(nil)
var _ agent.SessionContextStore = (*proofSupply)(nil)
'''


def write_proofstore():
    with open("cmd/omega/proofstore.go", "w", encoding="utf-8", newline="\n") as f:
        f.write(PROOFSTORE)
    print("strip: cmd/omega/proofstore.go: ok")


PROOFPROVIDER = '''package main

// The stub provider (scripts/proof-strip.py, card t_b75df168): the
// provider seam's HOST stub in the every-capsule-removed build. The
// provider capsule owns the real wire clients; deleted from the build
// the face still answers: the model_name is served, the stream carries
// one error end (the documented Provider contract - failures are
// stream events, never Go errors). What the proof exercises is the
// chat FACE answering; model serving is the capsule's opinion.

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

type proofProvider struct {
	model string
}

func newProofProvider() *proofProvider {
	return &proofProvider{model: "proof-stub"}
}

func (p *proofProvider) Stream(_ context.Context, _ []core.Message, _ []core.ToolSchema) <-chan core.ProviderStreamEvent {
	ch := make(chan core.ProviderStreamEvent, 1)
	ch <- core.StreamEnd{Type: "stream_end", FinishReason: "error",
		Error: "every-capsule-removed build: the provider capsule is absent - the host's stub provider answers"}
	close(ch)
	return ch
}

func (p *proofProvider) ModelName() string        { return p.model }
func (p *proofProvider) SetThinkingLevel(string)  {}

func (p *proofProvider) WithModel(model string) agent.Provider {
	q := *p
	q.model = model
	return &q
}

func (p *proofProvider) WithThinkingLevel(string) agent.Provider {
	q := *p
	return &q
}

func (p *proofProvider) ListModels() ([]string, error) {
	return []string{p.model}, nil
}

func (p *proofProvider) ModelInfo() (core.ModelInfo, error) {
	return core.ModelInfo{ContextWindow: agent.DefaultContextWindow}, nil
}

var _ agent.Provider = (*proofProvider)(nil)
'''


def write_proofprovider():
    with open("cmd/omega/proofprovider.go", "w", encoding="utf-8", newline="\n") as f:
        f.write(PROOFPROVIDER)
    print("strip: cmd/omega/proofprovider.go: ok")


if __name__ == "__main__":
    try:
        strip_host()
    except Stale as e:
        print(f"strip FAIL: {e}", file=sys.stderr)
        sys.exit(1)
