#!/bin/sh
printf '%s %.160s\n' "$(basename "$0")" "$*" >> "$(dirname "$0")/calls"
echo "$(basename "$0"): no live model runs in tests" >&2
exit 1
