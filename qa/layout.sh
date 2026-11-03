#!/bin/zsh
# Layout QA: start the workbench and a headless browser, walk every tab at
# three widths, and fail on anything that spills past its pane or is clipped
# with no way to recover the text.
#
#   zsh qa/layout.sh
#
# Needs Go, node and a Chrome or Chromium. Point CROSSWIRE_CHROME at the
# browser if it is not in a standard location.
set -u
ROOT=${0:A:h:h}
QA=${QA:-${TMPDIR:-/tmp}/crosswire-qa}
mkdir -p $QA
cd $ROOT

CHROME=${CROSSWIRE_CHROME:-}
if [[ -z $CHROME ]]; then
  for c in "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
           "/Applications/Chromium.app/Contents/MacOS/Chromium" \
           "$HOME/Library/Caches/ms-playwright/chromium-"*/chrome-mac/Chromium.app/Contents/MacOS/Chromium \
           /usr/bin/chromium /usr/bin/google-chrome; do
    [[ -x $c ]] && { CHROME=$c; break }
  done
fi
[[ -x $CHROME ]] || { print "no Chrome or Chromium found; set CROSSWIRE_CHROME"; exit 2 }

[[ -x $ROOT/crosswire ]] || go build -o $ROOT/crosswire ./cmd/crosswire || exit 1
[[ -d $QA/node_modules/ws ]] || (cd $QA && npm install ws --silent --no-fund --no-audit) || exit 1

typeset -a KIDS
cleanup() { for p in $KIDS; do kill $p 2>/dev/null; done }
trap cleanup EXIT INT TERM

rm -rf $QA/layout-data
# Another server already on 7117 would answer every request and the audit
# would silently grade the wrong build — which is how a broken UI once passed.
# Refuse to start rather than report on something we did not build.
if lsof -ti :7117 >/dev/null 2>&1; then
  print "FATAL: something is already serving 127.0.0.1:7117."
  print "Stop it first, or the audit will test that server instead of this build."
  exit 2
fi
if lsof -ti :9333 >/dev/null 2>&1; then
  print "FATAL: something is already on 127.0.0.1:9333 (the browser's debug port)."
  exit 2
fi

$ROOT/crosswire serve --listen 127.0.0.1:7117 --data $QA/layout-data > $QA/layout-serve.log 2>&1 &
SERVE_PID=$!
KIDS+=($SERVE_PID)
for i in {1..40}; do curl -sf http://127.0.0.1:7117/api/health >/dev/null && break; sleep 0.3; done
if ! kill -0 $SERVE_PID 2>/dev/null; then
  print "FATAL: the workbench exited during startup."; cat $QA/layout-serve.log; exit 2
fi
curl -sf http://127.0.0.1:7117/api/health >/dev/null || {
  print "FATAL: the workbench never answered."; cat $QA/layout-serve.log; exit 2 }

"$CHROME" --headless --disable-gpu --no-sandbox --hide-scrollbars --window-size=1440,900 \
  --remote-debugging-port=9333 --user-data-dir=$QA/layout-profile \
  "http://127.0.0.1:7117/" > $QA/layout-chrome.log 2>&1 &
KIDS+=($!)
for i in {1..40}; do curl -sf http://127.0.0.1:9333/json >/dev/null && break; sleep 0.3; done

cp $ROOT/qa/layout-audit.js $QA/layout-audit.js
(cd $QA && node layout-audit.js)
