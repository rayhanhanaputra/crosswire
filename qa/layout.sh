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
$ROOT/crosswire serve --listen 127.0.0.1:7117 --data $QA/layout-data > $QA/layout-serve.log 2>&1 &
KIDS+=($!)
for i in {1..40}; do curl -sf http://127.0.0.1:7117/api/health >/dev/null && break; sleep 0.3; done

"$CHROME" --headless --disable-gpu --no-sandbox --hide-scrollbars --window-size=1440,900 \
  --remote-debugging-port=9333 --user-data-dir=$QA/layout-profile \
  "http://127.0.0.1:7117/" > $QA/layout-chrome.log 2>&1 &
KIDS+=($!)
for i in {1..40}; do curl -sf http://127.0.0.1:9333/json >/dev/null && break; sleep 0.3; done

cp $ROOT/qa/layout-audit.js $QA/layout-audit.js
(cd $QA && node layout-audit.js)
