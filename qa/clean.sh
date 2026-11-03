#!/bin/zsh
# Clear every port and process an integration run uses.
# Kill only real server processes, by exact binary name and by port owner.
# Never grep a pattern that this script's own command line would match.
pkill -x crosswire 2>/dev/null
pkill -x fakemodel 2>/dev/null
sleep 1
for port in 7410 7500 7201 7202 7203 7204 7205 7206 9910; do
  for pid in $(lsof -ti :$port 2>/dev/null); do
    [[ $pid == $$ ]] && continue
    kill -9 $pid 2>/dev/null
  done
done
sleep 1
for port in 7410 7500 7201 7206 9910; do
  print -n "$port:$(lsof -ti :$port 2>/dev/null | tr '\n' ',') "
done
print ""
