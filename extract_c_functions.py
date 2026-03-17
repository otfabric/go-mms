#!/usr/bin/env python3
import argparse
import csv
import os
import re
from pathlib import Path

# Very small C parser for function declarations/definitions.
# Good enough for API/gap analysis, not a full C grammar.

CONTROL_KEYWORDS = {
    "if", "for", "while", "switch", "return", "sizeof"
}

SKIP_PREFIXES = (
    "typedef ",
    "struct ",
    "enum ",
    "union ",
    "#",
)

QUALIFIERS = {
    "static", "inline", "extern", "const", "volatile",
    "__inline", "__inline__", "__stdcall", "__cdecl"
}

FUNC_PTR_PARAM_RE = re.compile(r'\(\s*\*\s*([A-Za-z_]\w*)\s*\)')

def strip_comments(text: str) -> str:
    # remove block comments
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    # remove line comments
    text = re.sub(r"//.*?$", "", text, flags=re.M)
    return text

def squash_continuations(text: str) -> str:
    # join backslash-newline continuations
    return re.sub(r"\\\n", " ", text)

def normalize_ws(s: str) -> str:
    return re.sub(r"\s+", " ", s).strip()

def split_statements(text: str):
    """
    Split text into chunks ending with ; or { or } while keeping line numbers.
    """
    statements = []
    buf = []
    start_line = 1
    line = 1

    for ch in text:
        if not buf:
            start_line = line
        buf.append(ch)
        if ch == "\n":
            line += 1
        if ch in ";{}":
            stmt = "".join(buf).strip()
            if stmt:
                statements.append((start_line, stmt))
            buf = []

    trailing = "".join(buf).strip()
    if trailing:
        statements.append((start_line, trailing))

    return statements

def looks_like_function(stmt: str):
    s = stmt.strip()

    if not s:
        return False
    if any(s.startswith(p) for p in SKIP_PREFIXES):
        return False
    if "(" not in s or ")" not in s:
        return False
    if s.endswith("}"):
        return False

    head = s.split("(", 1)[0].strip()
    last_token = head.split()[-1] if head.split() else ""
    if last_token in CONTROL_KEYWORDS:
        return False

    return True

def extract_name(signature_head: str):
    tokens = signature_head.strip().split()
    if not tokens:
        return None
    name = tokens[-1].strip("*")
    if not re.match(r"^[A-Za-z_]\w*$", name):
        return None
    if name in CONTROL_KEYWORDS:
        return None
    return name

def classify(stmt: str, path: str):
    s = normalize_ws(stmt)

    if not looks_like_function(s):
        return None

    kind = None
    if s.endswith(";"):
        kind = "declaration"
    elif s.endswith("{"):
        kind = "definition"
    else:
        return None

    sig = s[:-1].strip()

    # Exclude typedef function pointers and variable/function pointer declarations
    if sig.startswith("typedef "):
        return None
    if FUNC_PTR_PARAM_RE.search(sig.split("(", 1)[0]):
        return None

    head, rest = sig.split("(", 1)
    name = extract_name(head)
    if not name:
        return None

    params = rest.rsplit(")", 1)[0].strip()

    storage = []
    head_tokens = head.split()
    ret_tokens = head_tokens[:-1]

    while ret_tokens and ret_tokens[0] in QUALIFIERS:
        storage.append(ret_tokens.pop(0))

    return_type = " ".join(ret_tokens).strip()
    storage_class = " ".join(storage).strip()

    if not return_type:
        return_type = "(unknown)"

    return {
        "file": path,
        "kind": kind,
        "name": name,
        "return_type": return_type,
        "storage": storage_class,
        "params": normalize_ws(params),
        "signature": sig,
    }

def should_skip(path: Path, include_asn1c: bool):
    p = str(path).replace("\\", "/")
    if not include_asn1c and "/iso_mms/asn1c/" in p:
        return True
    return False

def collect(root: Path, include_asn1c: bool):
    rows = []

    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix not in {".c", ".h"}:
            continue
        if should_skip(path, include_asn1c):
            continue

        rel = str(path.relative_to(root.parent)).replace("\\", "/")
        text = path.read_text(encoding="utf-8", errors="ignore")
        text = squash_continuations(strip_comments(text))

        for line_no, stmt in split_statements(text):
            rec = classify(stmt, rel)
            if rec is None:
                continue
            rec["line"] = line_no
            rows.append(rec)

    return rows

def main():
    ap = argparse.ArgumentParser(description="Extract C function declarations/definitions from a source tree")
    ap.add_argument("root", help="root directory to scan, e.g. sources/mms")
    ap.add_argument("-o", "--output", default="c_functions.csv", help="output CSV file")
    ap.add_argument("--include-asn1c", action="store_true", help="include generated iso_mms/asn1c files")
    ap.add_argument("--definitions-only", action="store_true", help="only include function definitions")
    ap.add_argument("--declarations-only", action="store_true", help="only include function declarations")
    args = ap.parse_args()

    if args.definitions_only and args.declarations_only:
        raise SystemExit("Choose only one of --definitions-only or --declarations-only")

    root = Path(args.root).resolve()
    rows = collect(root, include_asn1c=args.include_asn1c)

    if args.definitions_only:
        rows = [r for r in rows if r["kind"] == "definition"]
    elif args.declarations_only:
        rows = [r for r in rows if r["kind"] == "declaration"]

    rows.sort(key=lambda r: (r["name"], r["kind"], r["file"], r["line"]))

    with open(args.output, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(
            f,
            fieldnames=[
                "name", "kind", "return_type", "storage",
                "params", "file", "line", "signature"
            ],
        )
        w.writeheader()
        w.writerows(rows)

    print(f"Wrote {len(rows)} records to {args.output}")

if __name__ == "__main__":
    main()