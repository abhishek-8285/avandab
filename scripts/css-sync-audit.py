"""Audit v3: classes used in Go templates vs classes defined anywhere.

CSS corpus = tailwind.css + app.css + every inline <style> block in every
template. A class is only "missing" if it is referenced in a class attribute
but defined nowhere.

Two buckets matter:
  A. missing but a plain, valid Tailwind utility -> the compiled bundle is STALE
     (template edited after the last `make build-css`)
  B. missing and not a valid Tailwind utility -> dead/typo class, the element
     silently renders unstyled

v3 noise fixes (v2 reported ~28 false positives out of 39):
  1. Go template actions are stripped from the HTML *before* class attributes
     are extracted. v2 extracted first, so a quoted action like
     {{if eq .Status "open"}} truncated the attribute and leaked Go builtins
     (`eq`, `and`, `not`) in as class names.
  2. camelCase tokens are skipped: inside <script> blocks a class attribute is
     often a JS template string with variable interpolation
     ('... ' + badgeColor + ' ...'), and camelCase means JS identifier, not a
     class. This project's classes are kebab-case.
  3. Classes used as JS selectors (querySelector('.x'), classList.add('x'),
     closest('.x'), ...) are hooks by design and carry no CSS. They are now
     recognised and excluded.

Exit code is 1 when anything is missing, so CI can use this as a hard gate.
"""
import re
import glob
import sys

corpus = ""
for p in ["internal/static/css/tailwind.css", "internal/static/css/app.css"]:
    corpus += open(p, encoding="utf-8", errors="ignore").read()

templates = glob.glob("internal/templates/**/*.html", recursive=True)
inline_css = ""
for f in templates:
    html = open(f, encoding="utf-8", errors="ignore").read()
    for block in re.findall(r"<style[^>]*>(.*?)</style>", html, re.S):
        inline_css += block
corpus += inline_css

TEMPLATE_ACTION = re.compile(r"\{\{.*?\}\}", re.S)
VALID_TOKEN = re.compile(r"^-?[a-zA-Z][a-zA-Z0-9:_\-\[\]\(\)\.,%#\/'\*\$\s]*$")

# Tailwind-ish shape: optional variants, then a utility family
UTILITY = re.compile(
    r"^(?:[a-z0-9]+:)*-?"
    r"(?:p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|w|h|min-w|min-h|max-w|max-h|"
    r"gap|gap-x|gap-y|space-x|space-y|text|font|leading|tracking|bg|border|rounded|"
    r"shadow|opacity|flex|grid|col|row|items|justify|self|order|overflow|object|"
    r"relative|absolute|fixed|sticky|static|inset|top|right|bottom|left|z|"
    r"block|inline|inline-block|inline-flex|hidden|table|truncate|whitespace|break|"
    r"cursor|select|pointer-events|transition|duration|ease|delay|animate|transform|"
    r"translate|scale|rotate|skew|origin|ring|divide|sr|aspect|container|prose|"
    r"uppercase|lowercase|capitalize|italic|underline|line-through|no-underline|"
    r"align|float|clear|list|decoration|indent|placeholder|caret|accent|fill|stroke|"
    r"backdrop|blur|brightness|contrast|grayscale|hue-rotate|invert|saturate|sepia|"
    r"tabular|antialiased|subpixel-antialiased|isolation|mix-blend|bg-blend|"
    r"content|columns|break-after|break-before|break-inside|box|"
    r"[0-9]+(?:\.[0-9]+)?|[a-z0-9]+(?:-[a-z0-9]+)*)$"
)

# A class used only as a JS selector carries no CSS by design — .sc-text is a
# querySelector target, not a style hook. Collect them so they are not reported.
JS_HOOK = re.compile(
    r"(?:querySelector|querySelectorAll|closest|matches|getElementsByClassName)"
    r"\(\s*['\"]\.([A-Za-z][\w-]*)['\"]"
    r"|classList\.(?:add|remove|toggle|contains)\(\s*['\"]([A-Za-z][\w-]*)['\"]"
)
CAMEL = re.compile(r"[A-Z]")

# Semantic marker classes. These resolve to no CSS and are not bugs: each one
# is either driven by id= (getElementById('tier-card-1')), is a third-party
# widget convention, or is pure labelling. In every case the element's actual
# appearance comes from the utilities sitting next to the marker. They are
# listed here with a reason so the audit can be a hard CI gate rather than a
# wall of noise — if a marker ever starts needing styles, delete its line.
MARKERS = {
    "back-link": "labelling only; hover/colour comes from utilities",
    "board-cards": "drop target found via [data-status], not the class",
    "cf-turnstile": "Cloudflare Turnstile widget class, styled by their JS",
    "desktop-logo": "labelling only; layout comes from utilities",
    "draw-mode-btn": "selected by id=, not by class",
    "flash-dismiss": "onclick handler target; styling from utilities",
    "htmx-indicator": "htmx convention; paired with the `hidden` utility here",
    "htmx-indicator-hide": "htmx convention; hidden by htmx, not by CSS",
    "js-tenant-toggle": "JS hook prefix, intentionally unstyled",
    "multistop-timeline-container": "labelling only; id= drives behaviour",
    "navigation-top-menu-main": "labelling only; sibling .navigation-top-menu styling",
    "pay-root": "widget mount point, styled by the widget's own JS",
    "step-tab": "driven by getElementById('step-tab-N')",
    "text-decoration-none": "Bootstrap-ism; Tailwind preflight already resets links",
    "tier-card": "driven by getElementById('tier-card-N')",
    "time-display": "JS rewrites the text via [data-utc-time]",
    "wizard-step": "labelling only; id=step-content-N drives behaviour",
}

hooks = set()
for f in templates + glob.glob("internal/static/js/**/*.js", recursive=True):
    try:
        src = open(f, encoding="utf-8", errors="ignore").read()
    except OSError:
        continue
    for a, b in JS_HOOK.findall(src):
        name = a or b
        if name:
            hooks.add(name)

tokens = {}
for f in templates:
    html = open(f, encoding="utf-8", errors="ignore").read()
    # Strip Go template actions BEFORE extracting class attributes: an action
    # containing a quote ({{if eq .Status "open"}}) otherwise truncates the
    # attribute and leaks Go builtins like `eq` in as class names.
    html = TEMPLATE_ACTION.sub(" ", html)
    for m in re.findall(r'class="([^"]*)"', html):
        for t in m.split():
            if len(t) > 80 or not VALID_TOKEN.match(t):
                continue
            if CAMEL.search(t):
                # camelCase inside a <script> template string = JS identifier
                # being interpolated, not a class. This repo's classes are
                # kebab-case.
                continue
            if t in hooks or t in MARKERS:
                continue
            tokens.setdefault(t, f)


def esc(t):
    return "." + "".join(c if (c.isalnum() or c in "-_") else "\\" + c for c in t)


missing = [t for t in sorted(tokens) if esc(t) not in corpus and ("." + t) not in corpus]

stale, dead = [], []
for t in missing:
    (stale if UTILITY.match(t) else dead).append(t)

print(f"distinct class tokens in templates : {len(tokens)}")
print(f"js hook classes (excluded)         : {len(hooks)}")
print(f"defined nowhere                    : {len(missing)}")
print()
print(f"A. STALE BUNDLE (valid utility, not compiled) : {len(stale)}")
for t in stale:
    print("   ", t, "  <-", tokens[t])
print()
print(f"B. DEAD / TYPO CLASS (not a utility, not defined) : {len(dead)}")
for t in dead:
    print("   ", t, "  <-", tokens[t])

if missing:
    print()
    print(f"FAIL: {len(missing)} class(es) referenced in templates resolve to no CSS.")
    print("Run `make build-css`, or fix the class name if it is a typo.")
    sys.exit(1)
print()
print("OK: every template class resolves to defined CSS.")
