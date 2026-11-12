#!/usr/bin/env python3
"""A6 OpenAPI<->router parity audit.

Extracts /api/v1 paths+methods from openapi.yaml (regex; no YAML dependency) and
from Go route registrations (method-call regex with a Route() prefix stack).
Normalizes {param} -> {} so chi/spec parameter names do not matter.

Exit 1 when the spec documents a route the router does not register ("the spec
never lies"). Routes registered but undocumented are reported as the B12
backlog, grouped by domain.

Limitations (documented in docs/tech-specs/openapi-router-parity-a6.md):
- Route("prefix", setupFunc) indirection is not expanded.
- Only /api/v1/* is in scope (legacy /api/* routes excluded).
"""
import argparse
import collections
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SPEC = os.path.join(ROOT, "openapi.yaml")
GO_ROOTS = [os.path.join(ROOT, "internal"), os.path.join(ROOT, "cmd")]

PATH_RE = re.compile(r"^  (/[^:\s]+):\s*$")
METHOD_RE = re.compile(r"^    (get|post|put|delete|patch|head|options):\s*$")
ROUTE_FUNC_RE = re.compile(r'\bRoute\(\s*"([^"]+)"\s*,\s*func\s*\(')
METHOD_CALL_RE = re.compile(r'\b(Get|Post|Put|Delete|Patch)\(\s*"([^"]*)"')
PARAM_RE = re.compile(r"\{[^}/]+\}")


def norm(path: str) -> str:
    path = PARAM_RE.sub("{}", path)
    if path != "/" and path.endswith("/"):
        path = path.rstrip("/")
    return path


def strip_strings_and_comments(line: str) -> str:
    out = []
    i = 0
    n = len(line)
    while i < n:
        ch = line[i]
        if ch == '"' or ch == "'" or ch == chr(96):
            quote = ch
            i += 1
            while i < n:
                if line[i] == "\\":
                    i += 2
                    continue
                if line[i] == quote:
                    i += 1
                    break
                i += 1
            out.append(" ")
            continue
        if ch == "/" and i + 1 < n and line[i + 1] == "/":
            break
        out.append(ch)
        i += 1
    return "".join(out)


def spec_routes():
    routes = set()
    current = None
    in_paths = False
    with open(SPEC, encoding="utf-8") as fh:
        for line in fh:
            if not in_paths:
                if line.startswith("paths:"):
                    in_paths = True
                continue
            if line.strip() and not line.startswith(" "):
                break  # left paths section
            m = PATH_RE.match(line)
            if m:
                current = m.group(1)
                continue
            m = METHOD_RE.match(line)
            if m and current:
                routes.add((norm(current), m.group(1).upper()))
    return {r for r in routes if r[0].startswith("/api/v1")}


def go_files():
    for root in GO_ROOTS:
        for dirpath, _dirnames, filenames in os.walk(root):
            for name in filenames:
                if name.endswith(".go") and not name.endswith("_test.go"):
                    yield os.path.join(dirpath, name)


def router_routes():
    routes = set()
    for path in go_files():
        try:
            lines = open(path, encoding="utf-8").read().splitlines()
        except OSError:
            continue
        depth = 0
        stack = []  # (target_depth, prefix)
        for raw in lines:
            code = strip_strings_and_comments(raw)
            opens = code.count("{")
            closes = code.count("}")
            new_depth = depth + opens - closes
            m = ROUTE_FUNC_RE.search(raw)
            if m:
                prefix = m.group(1)
                if prefix.startswith("/api"):
                    stack.append((new_depth, prefix))
            depth = new_depth
            while stack and depth < stack[-1][0]:
                stack.pop()
            for call in METHOD_CALL_RE.finditer(raw):
                route = call.group(2)
                # chi treats "/x" inside Route("/prefix") as relative; only a
                # literal "/api/..." path is absolute.
                if not route.startswith("/api/"):
                    if not stack:
                        continue
                    route = stack[-1][1].rstrip("/") + "/" + route.lstrip("/")
                route = norm(route)
                if route.startswith("/api/v1"):
                    routes.add((route, call.group(1).upper()))
    return routes


def _route_ident(raw):
    marker = 'Route(' + chr(34)
    pos = raw.find(marker)
    if pos < 0:
        return None
    rest = raw[pos + len(marker):]
    end = rest.find(chr(34))
    if end < 0:
        return None
    prefix = rest[:end]
    after = rest[end + 1:].lstrip()
    if not after.startswith(','):
        return None
    after = after[1:].lstrip()
    name_chars = []
    for ch in after:
        if ch.isalnum() or ch == '_':
            name_chars.append(ch)
        else:
            break
    name = ''.join(name_chars)
    if not name:
        return None
    tail = after[len(name):].lstrip()
    if not tail.startswith(')'):
        return None
    return prefix, name


def _setup_func_names(lines):
    funcs = {}
    for i, raw in enumerate(lines):
        marker = ':= func('
        pos = raw.find(marker)
        if pos < 0:
            continue
        head = raw[:pos].strip()
        name = head.split()[-1] if head else ''
        if not name or not (name[0].isalpha() or name[0] == '_'):
            continue
        code = strip_strings_and_comments(raw)
        depth = code.count('{') - code.count('}')
        collected = []
        j = i
        while j < len(lines):
            if j > i:
                code = strip_strings_and_comments(lines[j])
                depth += code.count('{') - code.count('}')
            for call in METHOD_CALL_RE.finditer(lines[j]):
                collected.append((call.group(1).upper(), call.group(2)))
            if j > i and depth <= 0:
                break
            j += 1
        funcs[name] = collected
    return funcs


def router_routes_v2():
    routes = set()
    for path in go_files():
        try:
            lines = open(path, encoding='utf-8').read().splitlines()
        except OSError:
            continue
        depth = 0
        stack = []
        setup_funcs = _setup_func_names(lines)
        for raw in lines:
            ident = _route_ident(raw)
            if ident and ident[1] in setup_funcs:
                prefix = ident[0]
                for method, rel in setup_funcs[ident[1]]:
                    route = rel if rel.startswith('/api/') else prefix.rstrip('/') + '/' + rel.lstrip('/')
                    route = norm(route)
                    if route.startswith('/api/v1'):
                        routes.add((route, method))
            code = strip_strings_and_comments(raw)
            opens = code.count('{')
            closes = code.count('}')
            new_depth = depth + opens - closes
            m = ROUTE_FUNC_RE.search(raw)
            if m:
                prefix = m.group(1)
                if prefix.startswith('/api'):
                    stack.append((new_depth, prefix))
            depth = new_depth
            while stack and depth < stack[-1][0]:
                stack.pop()
            for call in METHOD_CALL_RE.finditer(raw):
                route = call.group(2)
                if not route.startswith('/api/'):
                    if not stack:
                        continue
                    route = stack[-1][1].rstrip('/') + '/' + route.lstrip('/')
                route = norm(route)
                if route.startswith('/api/v1'):
                    routes.add((route, call.group(1).upper()))
    return routes

def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true", help="machine-readable output")
    args = ap.parse_args()

    spec = spec_routes()
    router = router_routes_v2()
    dead = sorted(spec - router)
    undocumented = sorted(router - spec)

    groups = collections.Counter()
    for route, _method in undocumented:
        parts = route.split("/")
        groups["/".join(parts[:4]) or route] += 1

    if args.json:
        print(json.dumps({
            "spec": len(spec),
            "router": len(router),
            "dead_spec": [f"{m} {p}" for p, m in dead],
            "undocumented_count": len(undocumented),
            "undocumented_groups": groups.most_common(),
        }, indent=2))
    else:
        print(f"A6 parity: spec={len(spec)} router={len(router)}")
        print(f"dead spec entries (documented, not registered): {len(dead)}")
        for p, m in dead:
            print(f"  DEAD {m} {p}")
        print(f"undocumented router routes (B12 backlog): {len(undocumented)}")
        for group, count in groups.most_common(12):
            print(f"  {group}: {count}")

    if dead:
        print("FAIL: openapi.yaml documents routes the router does not register", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())


