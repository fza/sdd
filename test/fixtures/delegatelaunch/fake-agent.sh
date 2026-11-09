#!/bin/sh
# A fake agent CLI. It records its working folder, arguments and stdin under
# FAKE_AGENT_DIR, then answers with the stdout, stderr and exit status the
# test left there for its name. A <name>.hang file makes it outlive the round.
record="$FAKE_AGENT_DIR/${0##*/}"
{
  printf 'cwd=%s\n' "$PWD"
  for arg in "$@"; do printf 'arg=%s\n' "$arg"; done
} > "$record.call"
/bin/cat > "$record.stdin"
if [ -f "$record.hang" ]; then exec /bin/sleep 30; fi
if [ -f "$record.stderr" ]; then /bin/cat "$record.stderr" >&2; fi
if [ -f "$record.out" ]; then /bin/cat "$record.out"; fi
status=0
if [ -f "$record.exit" ]; then status=$(/bin/cat "$record.exit"); fi
exit "$status"
