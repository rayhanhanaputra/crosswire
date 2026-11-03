#!/bin/zsh
# One clean QA cycle: build what is needed, clear the machine, start the
# deterministic fake model, run the suite.
#
#   zsh qa/run.sh
#
# Needs Go and zsh. No network, no API key, no hardware.
set -u
ROOT=${0:A:h:h}
QA=${QA:-${TMPDIR:-/tmp}/crosswire-qa}
mkdir -p $QA
cd $ROOT

[[ -x $ROOT/crosswire ]] || { print "building crosswire..."; (cd $ROOT && go build -o crosswire ./cmd/crosswire) || exit 1 }
[[ -x $QA/fakemodel ]] || {
  print "building the fake model..."
  cp $ROOT/qa/fakemodel.go $QA/fakemodel.go
  (cd $QA && go mod init fakemodel >/dev/null 2>&1; go build -o fakemodel fakemodel.go) || exit 1
}
zsh $ROOT/qa/clean.sh >/dev/null
rm -f $QA/*.json(N) $QA/qa-out.txt(N)
$QA/fakemodel > $QA/model.log 2>&1 &
MODEL=$!
for i in {1..30}; do lsof -ti :7500 >/dev/null 2>&1 && break; sleep 0.3; done
zsh $ROOT/qa/integration.sh 2>&1 | tee $QA/qa-out.txt
RC=${pipestatus[1]}
kill $MODEL 2>/dev/null
zsh $ROOT/qa/clean.sh >/dev/null
exit $RC
