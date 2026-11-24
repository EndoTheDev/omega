#!/usr/bin/env bash
# menu-pin.sh - the row-menu contract: a menu's rendered labels and the recipe
# that asserts them move together, or this reds.
#
# A row menu is authored twice: the builder under harness/*/ui/*Menu.ts (and the
# statusbar's statusStats.ts) decides the labels, and a journey recipe asserts the
# sequence the browser renders by reading `.context-menu-label` and joining on "|".
# The node pins (webui/test/rowmenus.test.ts and friends) only guard the first
# half, so a deliberate menu change used to leave its browser half stale until the
# next nightly, where a recipe's `die` reports only the FIRST stale assert.
#
# This pin reads each builder for the row shape a recipe actually drives (the
# shape is declared in SURFACES), evaluates the label literals and ternaries in
# source order, drops separators (they carry no `.context-menu-label`), and
# compares the joined sequence against the assertion the recipe makes. A witness
# is the recipe's own assertion (`seq` compares the joined sequence, `count` the
# row count, `last` the final row) or `click`: the label the recipe clicks, which
# is how the statusbar's stat labels are witnessed. A shape no recipe drives is
# not pinned, so a conditional verb that grows on a row nobody asserts stays free.
#
# It is static: no browser, no node, no lane. A construct it does not model (a
# computed label, an unbound condition field, a `for` inside a builder) is exit 2,
# never a silent green - the pin must be extended before the menu moves again.
#
# Usage:
#   scripts/menu-pin.sh                # the real tree
#   scripts/menu-pin.sh --root <dir>   # a fixture tree with the same layout
#
# Exit: 0 clean, 1 a menu moved and its recipe did not, 2 cannot judge.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# winbtw seam: the pin is python3, and a native python cannot stat an MSYS
# /d/... path - hand it the drive-letter form when the host has one.
if command -v cygpath >/dev/null 2>&1; then
  ROOT="$(cygpath -m "$ROOT" 2>/dev/null || printf '%s' "$ROOT")"
fi

exec python3 - "$ROOT" "$@" <<'PY'
import os
import re
import sys

# --- the table: one entry per row shape a recipe drives ----------------------
# `shape` binds the builder's record parameter by name, so a builder that grows a
# field reads it here instead of guessing. `witnesses` is (recipe, kind, selector):
# an assert_eq message phrase for seq/count/last, a rendered label for click.
SURFACES = [
    {
        "surface": "session row (idle, unarchived)",
        "builder": "harness/store/ui/sessionMenu.ts",
        "function": "sessionMenuItems",
        "shape": {"session": {"id": "s1", "running": False, "archived": False}},
        "witnesses": [
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "seq",
             "an idle session row menus"),
            ("scripts/journeys/recipes/119-wire-only-menus.sh", "seq",
             "the session row menus Copy id after Export"),
        ],
    },
    {
        "surface": "agent row (locked default, asleep, not deletable)",
        "builder": "harness/continuous/ui/agentMenu.ts",
        "function": "agentMenuItems",
        "shape": {"agent": {"name": "main", "deletable": False, "awake": False}},
        "witnesses": [
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "seq",
             "the locked default agent offers New agent"),
        ],
    },
    {
        "surface": "cron row (enabled, not agent-owned, not running)",
        "builder": "harness/cron/ui/cronMenu.ts",
        "function": "cronMenuItems",
        "shape": {"job": {"name": "j1", "enabled": True, "agent_owned": False,
                          "running": False}},
        "witnesses": [
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "seq",
             "an enabled cron row menus Run now"),
        ],
    },
    {
        "surface": "cron row (paused, not agent-owned, not running)",
        "builder": "harness/cron/ui/cronMenu.ts",
        "function": "cronMenuItems",
        "shape": {"job": {"name": "j1", "enabled": False, "agent_owned": False,
                          "running": False}},
        "witnesses": [
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "seq",
             "a paused cron row menus Resume"),
        ],
    },
    {
        "surface": "skill row (enabled, toggleable)",
        "builder": "harness/skills/ui/skillMenu.ts",
        "function": "skillMenuItems",
        "shape": {"skill": {"disabled": False, "toggleable": True}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "a skills row menus Open"),
        ],
    },
    {
        "surface": "skill row (disabled, toggleable)",
        "builder": "harness/skills/ui/skillMenu.ts",
        "function": "skillMenuItems",
        "shape": {"skill": {"disabled": True, "toggleable": True}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "a disabled skill row menus Enable"),
        ],
    },
    {
        "surface": "tool row (on, toggleable)",
        "builder": "harness/tools/ui/toolMenu.ts",
        "function": "toolMenuItems",
        "shape": {"tool": {"disabled": False, "toggleable": True}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "a tool row menus its one verb"),
        ],
    },
    {
        "surface": "tool row (off, toggleable)",
        "builder": "harness/tools/ui/toolMenu.ts",
        "function": "toolMenuItems",
        "shape": {"tool": {"disabled": True, "toggleable": True}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "the disabled tool row menus Enable"),
        ],
    },
    {
        "surface": "mcp server row (enabled)",
        "builder": "harness/mcp/ui/mcpMenu.ts",
        "function": "mcpMenuItems",
        "shape": {"server": {"enabled": True}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "an MCP server row menus Open"),
        ],
    },
    {
        "surface": "file row (a file, chat bound, unarmed)",
        "builder": "harness/files/ui/fileMenu.ts",
        "function": "fileMenuItems",
        "shape": {"entry": {"isDir": False, "chatBound": True, "armedDelete": False}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "a file row menus the tree verbs"),
        ],
    },
    {
        "surface": "notification record (feed not empty, not clearing)",
        "builder": "harness/notifications/ui/notificationMenu.ts",
        "function": "notificationMenuItems",
        "shape": {"record": {"id": 1}, "feed": {"count": 1, "clearing": False}},
        "witnesses": [
            ("scripts/journeys/recipes/120-capsule-row-menus.sh", "seq",
             "a notification row menus its own Clear"),
        ],
    },
    {
        "surface": "terminal row (a selection carried)",
        "builder": "harness/terminal/ui/terminalMenu.ts",
        "function": "terminalMenuItems",
        "shape": {"deps": {"hasSelection": True}},
        "witnesses": [
            ("scripts/journeys/recipes/118-long-tail-context-menu.sh", "seq",
             "the terminal menus Copy, Paste, Clear"),
        ],
    },
    {
        "surface": "statusbar stat list (nothing hidden)",
        "builder": "webui/app/components/shell/statusStats.ts",
        "function": "statMenuItems",
        "shape": {"hidden": []},
        "witnesses": [
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "count",
             "the bar lists its eight stats plus the Reset row"),
            ("scripts/journeys/recipes/117-right-click-surfaces.sh", "click",
             "Message count"),
            ("scripts/journeys/recipes/119-wire-only-menus.sh", "last",
             "Reset is the LAST row of the stat menu"),
        ],
    },
]


class PinError(Exception):
    def __init__(self, message, code=2):
        super().__init__(message)
        self.code = code


def die(message, code=2):
    raise PinError(message, code)


# --- a tokenizer for the builder dialect -----------------------------------
PUNCT2 = ("=>", "===", "!==", "==", "!=", "&&", "||")
SINGLE = "{}[](),:;?.!+-<>="


def unescape(raw):
    out = []
    i = 0
    while i < len(raw):
        if raw[i] == "\\" and i + 1 < len(raw):
            nxt = raw[i + 1]
            out.append({"n": "\n", "t": "\t", "r": "\r"}.get(nxt, nxt))
            i += 2
            continue
        out.append(raw[i])
        i += 1
    return "".join(out)


def tokenize(src, where):
    toks = []
    i = 0
    line = 1
    n = len(src)
    while i < n:
        c = src[i]
        if c == "\n":
            line += 1
            i += 1
            continue
        if c in " \t\r":
            i += 1
            continue
        if src.startswith("//", i):
            j = src.find("\n", i)
            i = n if j < 0 else j
            continue
        if src.startswith("/*", i):
            j = src.find("*/", i + 2)
            if j < 0:
                die("%s:%d: unterminated block comment" % (where, line))
            line += src.count("\n", i, j)
            i = j + 2
            continue
        if c in "\"'`":
            quote = c
            i += 1
            buf = []
            start = line
            while i < n and src[i] != quote:
                if src[i] == "\\" and i + 1 < n:
                    buf.append(src[i:i + 2])
                    i += 2
                    continue
                if src[i] == "\n":
                    line += 1
                buf.append(src[i])
                i += 1
            if i >= n:
                die("%s:%d: unterminated string" % (where, start))
            i += 1
            toks.append(("str", unescape("".join(buf)), start))
            continue
        if c.isalpha() or c in "_$":
            j = i
            while j < n and (src[j].isalnum() or src[j] in "_$"):
                j += 1
            toks.append(("id", src[i:j], line))
            i = j
            continue
        if c.isdigit():
            j = i
            while j < n and (src[j].isdigit() or src[j] == "."):
                j += 1
            toks.append(("num", src[i:j], line))
            i = j
            continue
        matched = False
        for op in PUNCT2:
            if src.startswith(op, i):
                toks.append(("op", op, line))
                i += len(op)
                matched = True
                break
        if matched:
            continue
        if c in SINGLE:
            toks.append(("op", c, line))
            i += 1
            continue
        die("%s:%d: unexpected character %r - the pin does not read this dialect"
            % (where, line, c))
    toks.append(("eof", "", line))
    return toks


class Parser:
    def __init__(self, toks, where):
        self.toks = toks
        self.i = 0
        self.where = where

    def peek(self, k=0):
        j = self.i + k
        return self.toks[j] if j < len(self.toks) else self.toks[-1]

    def is_op(self, value):
        t = self.peek()
        return t[0] == "op" and t[1] == value

    def is_id(self, value):
        t = self.peek()
        return t[0] == "id" and t[1] == value

    def take(self):
        t = self.peek()
        self.i += 1
        return t

    def want(self, value):
        t = self.take()
        if not (t[0] == "op" and t[1] == value):
            die("%s:%d: expected %r, got %r" % (self.where, t[2], value, t[1]))
        return t

    def block(self):
        self.want("{")
        stmts = []
        while not self.is_op("}"):
            if self.peek()[0] == "eof":
                die("%s: a block runs to end of file - unbalanced braces" % self.where)
            stmts.append(self.stmt())
        self.want("}")
        return stmts

    def skip_value(self):
        """skip an expression we never read (a deps callback, a check flag)."""
        depth = 0
        while True:
            t = self.peek()
            if t[0] == "eof":
                die("%s: an entry field runs to end of file" % self.where)
            if t[0] == "op" and t[1] in "([{":
                depth += 1
            elif t[0] == "op" and t[1] in ")]}":
                if depth == 0:
                    return
                depth -= 1
            elif t[0] == "op" and t[1] == "," and depth == 0:
                return
            self.take()

    def stmt(self):
        t = self.peek()
        if t[0] == "id" and t[1] == "if":
            self.take()
            self.want("(")
            cond = self.expr()
            self.want(")")
            then = self.block() if self.is_op("{") else [self.stmt()]
            els = None
            if self.is_id("else"):
                self.take()
                els = self.block() if self.is_op("{") else [self.stmt()]
            return ("if", cond, then, els, t[2])
        if t[0] == "id" and t[1] in ("const", "let", "var"):
            self.take()
            name = self.take()
            if name[0] != "id":
                die("%s:%d: a %s with no name" % (self.where, t[2], t[1]))
            while not self.is_op("="):
                if self.peek()[0] == "eof" or self.is_op(";"):
                    die("%s:%d: `%s` carries no initializer" % (self.where, t[2], name[1]))
                self.take()
            self.want("=")
            value = self.expr()
            if self.is_op(";"):
                self.take()
            return ("const", name[1], value, t[2])
        if t[0] == "id" and t[1] == "return":
            self.take()
            if self.is_op(";"):
                self.take()
                return ("return", None, t[2])
            value = self.expr()
            if self.is_op(";"):
                self.take()
            return ("return", value, t[2])
        if t[0] == "id" and t[1] in ("for", "while", "switch", "function", "do"):
            die("%s:%d: a `%s` in a builder the pin walks - the pin reads "
                "if/const/return/push; extend it before the menu moves"
                % (self.where, t[2], t[1]))
        value = self.expr()
        if self.is_op(";"):
            self.take()
        return ("expr", value, t[2])

    def expr(self):
        return self.ternary()

    def ternary(self):
        cond = self.binary(0)
        if self.is_op("?"):
            self.take()
            then = self.ternary()
            self.want(":")
            els = self.ternary()
            return ("ternary", cond, then, els, node_line(cond))
        return cond

    LEVELS = (("||",), ("&&",), ("===", "!==", "==", "!="), ("+", "-"))

    def binary(self, level):
        if level == len(self.LEVELS):
            return self.unary()
        left = self.binary(level + 1)
        while True:
            t = self.peek()
            if t[0] == "op" and t[1] in self.LEVELS[level]:
                self.take()
                right = self.binary(level + 1)
                left = ("bin", t[1], left, right, t[2])
                continue
            return left

    def unary(self):
        t = self.peek()
        if t[0] == "op" and t[1] in ("!", "-"):
            self.take()
            return ("un", t[1], self.unary(), t[2])
        return self.postfix()

    def postfix(self):
        node = self.primary()
        while True:
            if self.is_op("."):
                self.take()
                prop = self.take()
                if prop[0] not in ("id", "num"):
                    die("%s:%d: a property access the pin cannot read"
                        % (self.where, prop[2]))
                node = ("member", node, prop[1], prop[2])
                continue
            if self.is_op("("):
                self.take()
                args = []
                while not self.is_op(")"):
                    args.append(self.expr())
                    if self.is_op(","):
                        self.take()
                    elif not self.is_op(")"):
                        die("%s:%d: a call argument list the pin cannot read"
                            % (self.where, self.peek()[2]))
                at = self.peek()[2]
                self.want(")")
                node = ("call", node, args, at)
                continue
            return node

    def arrow_end(self):
        """index of the `)` matching the `(` already consumed, or None."""
        depth = 1
        j = self.i
        while j < len(self.toks):
            t = self.toks[j]
            if t[0] == "op" and t[1] in "([{":
                depth += 1
            elif t[0] == "op" and t[1] in ")]}":
                depth -= 1
                if depth == 0:
                    return j
            j += 1
        return None

    def primary(self):
        t = self.take()
        if t[0] == "str":
            return ("str", t[1], t[2])
        if t[0] == "num":
            return ("num", t[1], t[2])
        if t[0] == "id":
            return ("id", t[1], t[2])
        if t[0] == "op" and t[1] == "(":
            close = self.arrow_end()
            if close is not None and close + 1 < len(self.toks) \
                    and self.toks[close + 1][0] == "op" and self.toks[close + 1][1] == "=>":
                params = []
                segment = []
                depth = 0
                while self.i < close:
                    tok = self.take()
                    if tok[0] == "op" and tok[1] in "([{":
                        depth += 1
                    elif tok[0] == "op" and tok[1] in ")]}":
                        depth -= 1
                    if tok[0] == "op" and tok[1] == "," and depth == 0:
                        params.append(segment)
                        segment = []
                        continue
                    segment.append(tok)
                params.append(segment)
                names = []
                for seg in params:
                    for tok in seg:
                        if tok[0] == "id":
                            names.append(tok[1])
                            break
                    else:
                        die("%s:%d: an arrow parameter the pin cannot name"
                            % (self.where, t[2]))
                self.want(")")
                self.want("=>")
                body = self.block() if self.is_op("{") else self.expr()
                return ("arrow", names, body, t[2])
            node = self.expr()
            self.want(")")
            return node
        if t[0] == "op" and t[1] == "[":
            items = []
            while not self.is_op("]"):
                items.append(self.expr())
                if self.is_op(","):
                    self.take()
                elif not self.is_op("]"):
                    die("%s:%d: an array the pin cannot read (missing , or ])"
                        % (self.where, self.peek()[2]))
            self.want("]")
            return ("array", items, t[2])
        if t[0] == "op" and t[1] == "{":
            pairs = []
            while not self.is_op("}"):
                key = self.take()
                if key[0] not in ("id", "str"):
                    die("%s:%d: an object key the pin cannot read" % (self.where, key[2]))
                self.want(":")
                if key[1] in ("label", "kind"):
                    pairs.append((key[1], self.expr()))
                else:
                    self.skip_value()
                if self.is_op(","):
                    self.take()
                elif not self.is_op("}"):
                    die("%s:%d: an object the pin cannot read (missing , or })"
                        % (self.where, self.peek()[2]))
            self.want("}")
            return ("object", pairs, t[2])
        die("%s:%d: unexpected token %r in a builder expression"
            % (self.where, t[2], t[1]))


# --- evaluation: literals, shapes, ternaries -------------------------------
class Unevaluated:
    def __init__(self, name):
        self.name = name

    def __repr__(self):
        return "<computed %s>" % self.name


class Env:
    def __init__(self, layers=None):
        self.layers = list(layers or [])

    def child(self, layer):
        return Env(self.layers + [layer])

    def get(self, name, where, line):
        for layer in reversed(self.layers):
            if name in layer:
                return layer[name]
        die("%s:%d: `%s` is not a field the pin binds for this surface - a "
            "builder condition moved beyond the pin's model, extend SURFACES"
            % (where, line, name))


def node_line(node):
    for part in reversed(node):
        if isinstance(part, int):
            return part
    return 0


def truthy(value, where, node):
    if isinstance(value, Unevaluated):
        die("%s:%d: `%s` is computed by a call the pin does not model - "
            "extend the pin" % (where, node_line(node), value.name))
    return bool(value)


def evaluate(node, env, where):
    kind = node[0]
    if kind == "str":
        return node[1]
    if kind == "num":
        return int(node[1]) if node[1].isdigit() else float(node[1])
    if kind == "id":
        if node[1] == "true":
            return True
        if node[1] == "false":
            return False
        if node[1] == "null":
            return None
        return env.get(node[1], where, node[2])
    if kind == "member":
        base = evaluate(node[1], env, where)
        if isinstance(base, dict) and node[2] in base:
            return base[node[2]]
        die("%s:%d: cannot read `.%s` off %r - the pin binds flat records only"
            % (where, node[3], node[2], base))
    if kind == "un":
        value = evaluate(node[2], env, where)
        if node[1] == "!":
            return not truthy(value, where, node)
        return -value
    if kind == "bin":
        op = node[1]
        left = evaluate(node[2], env, where)
        if op == "||":
            return left if truthy(left, where, node) else evaluate(node[3], env, where)
        if op == "&&":
            return evaluate(node[3], env, where) if truthy(left, where, node) else left
        right = evaluate(node[3], env, where)
        if op == "===" or op == "==":
            return left == right
        if op == "!==" or op == "!=":
            return left != right
        if op == "+":
            if isinstance(left, str) and isinstance(right, str):
                return left + right
            die("%s: a `+` the pin cannot fold (%r + %r)" % (where, left, right))
        return left - right
    if kind == "ternary":
        return (evaluate(node[2], env, where)
                if truthy(evaluate(node[1], env, where), where, node[1])
                else evaluate(node[3], env, where))
    if kind == "call":
        die("%s:%d: a computed value the pin does not model - extend the pin "
            "before the menu moves" % (where, node_line(node)))
    die("%s: an expression the pin does not model (%s)" % (where, kind))


def rows(node, env, where):
    """the rendered LABELS this expression contributes, in source order."""
    kind = node[0]
    if kind == "array":
        out = []
        for item in node[1]:
            out.extend(rows(item, env, where))
        return out
    if kind == "object":
        fields = dict(node[1])
        if "label" in fields:
            value = evaluate(fields["label"], env, where)
            if not isinstance(value, str):
                die("%s:%d: an entry label reads %r, not a string literal - the "
                    "pin compares rendered labels" % (where, node[2], value))
            return [value]
        if "kind" in fields and evaluate(fields["kind"], env, where) == "separator":
            return []
        die("%s:%d: an entry with neither `label` nor a separator `kind`"
            % (where, node[2]))
    if kind == "ternary":
        chosen = node[2] if truthy(evaluate(node[1], env, where), where, node[1]) else node[3]
        return rows(chosen, env, where)
    if kind == "call" and node[1][0] == "member":
        prop = node[1][2]
        if prop == "push":
            return rows(node[2][0], env, where)
        if prop == "map":
            base = evaluate(node[1][1], env, where)
            if not isinstance(base, list):
                die("%s: a `.map` over %r - the pin reads a catalog array"
                    % (where, base))
            fn = node[2][0]
            if fn[0] != "arrow" or not isinstance(fn[2], list):
                die("%s: a `.map` the pin cannot walk (arrow body is not a block)" % where)
            out = []
            for item in base:
                out.extend(run_block(fn[2], env.child({fn[1][0]: item}), where)[0])
            return out
        die("%s:%d: a row builder call the pin does not model (`.%s`) - extend the "
            "pin before the menu moves" % (where, node_line(node), prop))
    if kind == "id":
        bound = env.get(node[1], where, node[2])
        if bound == ("items",):
            # the accumulator its own pushes already emitted, in order
            return []
        die("%s:%d: `%s` is not a row list the pin can read - extend the pin before "
            "the menu moves" % (where, node[2], node[1]))
    die("%s:%d: a menu row the pin cannot read from this expression - extend the pin "
        "before the menu moves" % (where, node_line(node)))


def is_row_call(node):
    return (node[0] == "call" and node[1][0] == "member"
            and node[1][2] in ("push", "map"))


def run_block(stmts, env, where):
    """walk a builder block: returns (labels, did-return)."""
    out = []
    local = {}
    env = env.child(local)
    for stmt in stmts:
        kind = stmt[0]
        if kind == "if":
            branch = stmt[2] if truthy(evaluate(stmt[1], env, where), where, stmt[1]) \
                else (stmt[3] or [])
            got, returned = run_block(branch, env, where)
            out.extend(got)
            if returned:
                return out, True
            continue
        if kind == "const":
            node = stmt[2]
            if node[0] in ("array", "object", "ternary") or is_row_call(node):
                out.extend(rows(node, env, where))
                local[stmt[1]] = ("items",)
                continue
            try:
                local[stmt[1]] = evaluate(node, env, where)
            except PinError:
                local[stmt[1]] = Unevaluated(stmt[1])
            continue
        if kind == "return":
            if stmt[1] is not None:
                out.extend(rows(stmt[1], env, where))
            return out, True
        # an expression statement: the push / the call
        out.extend(rows(stmt[1], env, where))
    return out, False


# --- reading a builder file -------------------------------------------------
def read_file(root, rel):
    path = os.path.join(root, rel)
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return handle.read()
    except OSError as exc:
        die("cannot read %s: %s" % (rel, exc))


def scan_balanced(src, start, where):
    """text of the bracket group opened at src[start], comments/strings skipped."""
    opener = src[start]
    closer = {"{": "}", "[": "]", "(": ")"}[opener]
    depth = 0
    i = start
    while i < len(src):
        c = src[i]
        if c in "\"'`":
            quote = c
            i += 1
            while i < len(src) and src[i] != quote:
                i += 2 if src[i] == "\\" else 1
            i += 1
            continue
        if src.startswith("//", i):
            j = src.find("\n", i)
            i = len(src) if j < 0 else j
            continue
        if src.startswith("/*", i):
            j = src.find("*/", i + 2)
            if j < 0:
                die("%s: unterminated block comment" % where)
            i = j + 2
            continue
        if c == opener:
            depth += 1
        elif c == closer:
            depth -= 1
            if depth == 0:
                return src[start:i + 1]
        i += 1
    die("%s: unbalanced %r group" % (where, opener))


def function_parts(src, where, name):
    match = re.search(r"\bexport\s+function\s+" + re.escape(name) + r"\s*\(", src)
    if not match:
        die("%s: no `export function %s(` - the pin names a builder that is gone"
            % (where, name))
    params_at = match.end() - 1
    params_text = scan_balanced(src, params_at, where)[1:-1]
    i = params_at + len(params_text) + 2
    while i < len(src) and src[i] not in "{;":
        i += 1
    if i >= len(src) or src[i] != "{":
        die("%s: `export function %s` carries no body" % (where, name))
    body = scan_balanced(src, i, where)
    names = []
    depth = 0
    segment = ""
    for c in params_text + ",":
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
        if c == "," and depth == 0:
            if not segment.strip():
                segment = ""
                continue
            got = re.match(r"\s*([A-Za-z_$][\w$]*)", segment)
            if not got:
                die("%s: `%s` takes a parameter the pin cannot name: %r"
                    % (where, name, segment.strip()))
            names.append(got.group(1))
            segment = ""
            continue
        segment += c
    return names, body


def module_catalog(src, where):
    out = {}
    for match in re.finditer(r"\bexport\s+const\s+([A-Za-z_$][\w$]*)", src):
        name = match.group(1)
        i = src.find("=", match.end())
        if i < 0:
            continue
        i += 1
        while i < len(src) and src[i] in " \t\r\n":
            i += 1
        if i >= len(src) or src[i] != "[":
            continue
        node = Parser(tokenize(scan_balanced(src, i, where), where), where).expr()
        if node[0] != "array":
            die("%s: `%s` does not read as an array literal" % (where, name))
        items = []
        for entry in node[1]:
            if entry[0] != "object":
                die("%s: `%s` holds a non-object entry" % (where, name))
            record = {}
            for key, value in entry[1]:
                if value[0] != "str":
                    die("%s: `%s.%s` is not a string literal" % (where, name, key))
                record[key] = value[1]
            items.append(record)
        out[name] = items
    return out


def builder_labels(root, spec):
    where = spec["builder"]
    src = read_file(root, where)
    params, body = function_parts(src, where, spec["function"])
    for field in spec["shape"]:
        if field not in params:
            die("%s: the pin binds `%s`, but `%s` takes (%s) - the shape table "
                "drifted from the builder" % (where, field, spec["function"],
                                              ", ".join(params)))
    env = Env([module_catalog(src, where), dict(spec["shape"])])
    stmts = Parser(tokenize(body, where), where).block()
    labels, _ = run_block(stmts, env, where)
    if not labels:
        die("%s: the %s shape renders no rows at all - the shape table names a "
            "branch the builder no longer takes" % (where, spec["surface"]))
    return labels


# --- reading a recipe assertion --------------------------------------------
def logical_lines(text):
    out = []
    lines = text.split("\n")
    i = 0
    while i < len(lines):
        start = i + 1
        buf = lines[i]
        while buf.rstrip().endswith("\\") and i + 1 < len(lines):
            buf = buf.rstrip()[:-1]
            i += 1
            buf += " " + lines[i]
        out.append((start, buf))
        i += 1
    return out


def bash_args(line):
    match = re.match(r"\s*assert_eq\b", line)
    if not match:
        return None
    i = match.end()
    n = len(line)
    args = []
    while i < n:
        while i < n and line[i] in " \t":
            i += 1
        if i >= n:
            break
        c = line[i]
        if c == '"':
            i += 1
            buf = []
            while i < n and line[i] != '"':
                if line[i] == "\\" and i + 1 < n:
                    buf.append(line[i:i + 2])
                    i += 2
                    continue
                buf.append(line[i])
                i += 1
            if i >= n:
                return None
            i += 1
            args.append(unescape("".join(buf)))
            continue
        if c == "'":
            j = line.find("'", i + 1)
            if j < 0:
                return None
            args.append(line[i + 1:j])
            i = j + 1
            continue
        j = i
        while j < n and line[j] not in " \t":
            j += 1
        args.append(line[i:j])
        i = j
    return args


def witness(root, rel, phrase):
    text = read_file(root, rel)
    hits = []
    for lineno, line in logical_lines(text):
        args = bash_args(line)
        if not args or len(args) < 3:
            continue
        if phrase in args[2]:
            hits.append((lineno, args[1]))
    if not hits:
        die("%s: no assert_eq line carries the pin's witness phrase %r - the pin "
            "lost its witness (a recipe message moved): update SURFACES" % (rel, phrase))
    if len(hits) > 1:
        die("%s: %d assert_eq lines carry %r - the witness phrase must name one line"
            % (rel, len(hits), phrase))
    return hits[0]


def pin_surface(root, spec):
    labels = builder_labels(root, spec)
    seq = "|".join(labels)
    findings = []
    for rel, kind, phrase in spec["witnesses"]:
        if kind == "click":
            # a label the recipe CLICKS is a witness too: click_row dies at runtime
            # on a row name the builder no longer renders
            if phrase not in labels:
                findings.append(
                    "menu-pin: the %s moved - %s renders\n"
                    "    \"%s\"\n"
                    "  and no longer renders %r, the label %s clicks\n"
                    "  (a surface lost the row its recipe drives: move both together)."
                    % (spec["surface"], spec["builder"], seq, phrase, rel))
                continue
            text = read_file(root, rel)
            if ('click_row "%s"' % phrase) not in text:
                findings.append(
                    "menu-pin: the %s moved - %s renders\n"
                    "    \"%s\"\n"
                    "  but %s no longer clicks %r - the recipe's browser half lost "
                    "the row it drives: move the recipe in the same change."
                    % (spec["surface"], spec["builder"], seq, rel, phrase))
            continue
        lineno, asserted = witness(root, rel, phrase)
        asserted = asserted.strip()
        if kind == "seq":
            if asserted != seq:
                findings.append(
                    "menu-pin: the %s moved - %s renders\n"
                    "    \"%s\"  (%d rows)\n"
                    "  but %s:%d still asserts\n"
                    "    \"%s\"\n"
                    "  the menu moved and the recipe did not: move the recipe in the "
                    "same change (or fix the pin's shape if the row is wrong)."
                    % (spec["surface"], spec["builder"], seq, len(labels),
                       rel, lineno, asserted))
        elif kind == "count":
            if not asserted.isdigit() or int(asserted) != len(labels):
                findings.append(
                    "menu-pin: the %s moved - %s renders %d rows\n"
                    "    \"%s\"\n"
                    "  but %s:%d still asserts %r rows\n"
                    "  the menu moved and the recipe did not: move the recipe in the "
                    "same change." % (spec["surface"], spec["builder"], len(labels),
                                      seq, rel, lineno, asserted))
        elif kind == "last":
            if asserted != labels[-1]:
                findings.append(
                    "menu-pin: the %s moved - %s renders its last row as %r\n"
                    "    \"%s\"\n"
                    "  but %s:%d still asserts %r is last\n"
                    "  the menu moved and the recipe did not: move the recipe in the "
                    "same change." % (spec["surface"], spec["builder"], labels[-1],
                                      seq, rel, lineno, asserted))
        else:
            die("menu-pin: unknown witness kind %r in SURFACES" % kind)
    return labels, findings


def main(argv):
    if len(argv) < 2:
        die("menu-pin: usage: menu-pin.sh [--root <dir>]")
    root = argv[1]
    rest = argv[2:]
    i = 0
    while i < len(rest):
        arg = rest[i]
        if arg in ("-h", "--help"):
            print("usage: menu-pin.sh [--root <dir>]")
            return 0
        if arg == "--root":
            if i + 1 >= len(rest):
                die("menu-pin: --root needs a directory")
            root = rest[i + 1]
            i += 2
            continue
        die("menu-pin: unknown option: %s" % arg)
    if not os.path.isdir(root):
        die("menu-pin: no such directory: %s" % root)

    findings = []
    for spec in SURFACES:
        labels, got = pin_surface(root, spec)
        findings.extend(got)
    if findings:
        for finding in findings:
            print(finding)
        print("menu-pin: %d recipe assertion(s) red" % len(findings))
        return 1
    witnesses = sum(len(spec["witnesses"]) for spec in SURFACES)
    print("menu-pin: %d surfaces and %d recipe assertions agree" % (len(SURFACES), witnesses))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except PinError as exc:
        sys.stderr.write(str(exc) + "\n")
        sys.exit(exc.code)
PY
