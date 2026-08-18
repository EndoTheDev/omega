#!/usr/bin/env python3
"""Validate a Hermes skill file against common issues.

Usage:
    python scripts/validate-skill.py path/to/skill.md

Checks:
    - Valid YAML frontmatter
    - Required fields (name, description)
    - Name format (lowercase-hyphens)
    - Description quality (trigger language, length)
    - Body structure (headings)
    - AI writing patterns (boldface, title case, emojis)
    - Size warnings (>500 lines)

Exit code 0 = pass, 1 = failures found.
"""

import re
import sys
import yaml  # type: ignore[import-unresolved]


def load_skill(path):
    """Read a skill file and return (frontmatter, body, errors)."""
    try:
        with open(path, encoding="utf-8") as f:
            content = f.read()
    except FileNotFoundError:
        return None, None, [f"file not found: {path}"]
    except Exception as e:
        return None, None, [f"read error: {e}"]

    if not path.endswith(".md"):
        return None, None, [f"not a .md file: {path}"]

    # Extract frontmatter
    if not content.startswith("---"):
        return None, None, ["no YAML frontmatter (file must start with ---)"]

    parts = content.split("---", 2)
    if len(parts) < 3:
        return None, None, ["malformed frontmatter (missing closing ---)"]

    fm_text = parts[1].strip()
    body = parts[2].strip()

    try:
        fm = yaml.safe_load(fm_text)
    except yaml.YAMLError as e:
        return None, None, [f"YAML parse error: {e}"]

    if not isinstance(fm, dict):
        return None, None, ["frontmatter is not a YAML mapping"]

    return fm, body, []


def check_name(fm):
    """Check the name field."""
    name = fm.get("name", "")
    if not name:
        return ["missing 'name' field in frontmatter"]
    issues = []
    if not re.match(r"^[a-z0-9][a-z0-9-]*$", name):
        issues.append(f"name '{name}' is not lowercase-hyphens (use only a-z, 0-9, hyphens)")
    if name.startswith("-") or name.endswith("-"):
        issues.append(f"name '{name}' starts or ends with a hyphen")
    return issues


def check_description(fm):
    """Check the description field."""
    desc = fm.get("description", "")
    if not desc:
        return ["missing 'description' field in frontmatter"]
    issues = []
    if len(desc) > 200:
        issues.append(f"description is {len(desc)} chars (keep under 200; first 57 shown in system prompt)")
    # Check for trigger language
    trigger_words = ["use when", "use for", "load when", "apply when", "trigger"]
    if not any(w in desc.lower() for w in trigger_words):
        issues.append("description should start with trigger language (e.g. 'Use when...')")
    return issues


def check_body(body, path):
    """Check the body for structure and AI patterns."""
    issues = []
    lines = body.split("\n")

    # Structure
    headings = [l for l in lines if l.startswith("## ")]
    if not headings:
        issues.append("body has no ## headings — add at least one section")

    # Size
    if len(lines) > 500:
        issues.append(f"body is {len(lines)} lines — consider splitting into references/")

    # AI patterns
    if re.search(r"\*\*[^*]+\*\*", body):
        issues.append("body contains boldface (**text**) — use plain text instead")

    for h in headings:
        # Check for title case (more than one capital word after ##)
        words = h[3:].split()
        caps = [w for w in words if w[0].isupper() and len(w) > 3]
        if len(caps) > 1:
            issues.append(f"heading '{h}' uses title case — use sentence case")

    # Emojis
    emoji = re.search(r"[\U0001F300-\U0001F9FF]", body)
    if emoji:
        issues.append(f"body contains emoji: {emoji.group()}")

    return issues


def main():
    if len(sys.argv) < 2:
        print("Usage: python scripts/validate-skill.py path/to/skill.md")
        sys.exit(1)

    path = sys.argv[1]
    fm, body, errors = load_skill(path)

    if errors:
        for e in errors:
            print(f"✗ {e}")
        sys.exit(1)

    assert fm is not None, "load_skill returned None without errors"
    assert body is not None, "load_skill returned None without errors"

    all_issues = []
    all_issues.extend(check_name(fm))
    all_issues.extend(check_description(fm))
    all_issues.extend(check_body(body, path))

    # Report
    checks = {
        "frontmatter valid": True,
        f"name: {fm.get('name', 'MISSING')}": "name" in fm,
        "description: present": bool(fm.get("description")),
        "name format: lowercase-hyphens": not any("lowercase-hyphens" in i for i in all_issues),
        "description starts with trigger": not any("trigger language" in i for i in all_issues),
        f"description length: {len(fm.get('description', ''))} chars": len(fm.get("description", "")) <= 200,
        "body has headings": not any("no ## headings" in i for i in all_issues),
        "no boldface": not any("boldface" in i for i in all_issues),
        "no title case headings": not any("title case" in i for i in all_issues),
        "no emojis": not any("emoji" in i for i in all_issues),
    }

    passed = 0
    for label, ok in checks.items():
        if ok:
            print(f"✓ {label}")
            passed += 1
        else:
            print(f"✗ {label}")

    # Warnings (non-fatal)
    warnings = [i for i in all_issues if "lines — consider" in i]
    for w in warnings:
        print(f"⚠ {w}")

    # Errors (fatal)
    failures = [i for i in all_issues if "lines — consider" not in i]
    for f in failures:
        print(f"✗ {f}")

    total = len(checks)
    if failures:
        print(f"\nFAIL ({passed}/{total} checks, {len(failures)} failures, {len(warnings)} warnings)")
        sys.exit(1)
    else:
        print(f"\nPASS ({passed}/{total} checks, {len(warnings)} warnings)")
        sys.exit(0)


if __name__ == "__main__":
    main()
