#!/usr/bin/env python3
"""Convert gitleaks' default rule file into rules.json for internal/secrets.

Usage: vendor-rules.py <gitleaks.toml> <version> <commit>

Keeps only what the scanner reads: regex, path, entropy, secretGroup,
keywords and allowlists. Stopwords and keywords are lowercased, as gitleaks
does when it loads them.
"""

import json
import pathlib
import sys
import tomllib


def allowlist(entry):
    out = {}
    if entry.get("condition"):
        out["condition"] = entry["condition"].upper()
    if entry.get("regexTarget"):
        out["regexTarget"] = entry["regexTarget"]
    for key in ("regexes", "paths"):
        if entry.get(key):
            out[key] = entry[key]
    if entry.get("stopwords"):
        out["stopwords"] = [word.lower() for word in entry["stopwords"]]
    return out


def allowlists(table, single, multiple):
    found = []
    if single in table:
        found.append(allowlist(table[single]))
    for entry in table.get(multiple, []):
        found.append(allowlist(entry))
    return found


def rule(entry):
    out = {"id": entry["id"]}
    for key in ("regex", "path", "entropy", "secretGroup"):
        if key in entry:
            out[key] = entry[key]
    if entry.get("keywords"):
        out["keywords"] = sorted({word.lower() for word in entry["keywords"]})
    lists = allowlists(entry, "allowlist", "allowlists")
    if lists:
        out["allowlists"] = lists
    return out


def main():
    source, version, commit = sys.argv[1:4]
    config = tomllib.loads(pathlib.Path(source).read_text())
    vendored = {
        "source": {"repository": "github.com/gitleaks/gitleaks", "file": "config/gitleaks.toml", "version": version, "commit": commit},
        "allowlists": allowlists(config, "allowlist", "allowlists"),
        "rules": [rule(entry) for entry in config["rules"]],
    }
    target = pathlib.Path(__file__).with_name("rules.json")
    target.write_text(json.dumps(vendored, indent=1, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
