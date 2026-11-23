#!/bin/sh
printf '%s %.160s\n' "$(basename "$0")" "$*" >> "$(dirname "$0")/calls"
echo "$(basename "$0"): tests never run the installed CLI" >&2
exit 1
