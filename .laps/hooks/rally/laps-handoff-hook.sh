#!/bin/sh
# Hook-only command: laps handoff
# Sets handoff state and directs agent to wrapup

# Audit trail: record this hook firing.
AUDIT_FILE=".rally/state/hook-audit.jsonl"
mkdir -p "$(dirname "$AUDIT_FILE")" 2>/dev/null || true
TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
printf '{"ts":"%s","hook":"laps-handoff","args":"%s","pid":%d}\n' "$TS" "$*" "$$" >> "$AUDIT_FILE" 2>/dev/null || true

rally progress --set-handoff
echo "Handoff signaled. Commit your work and wrap up before exiting:"
echo "  Commit (replace <lap-description> with this lap's description):"
echo '    git commit -m "<lap-description>: in progress (handoff)"'
echo '  Wrapup: laps wrapup --summary "<why blocked>" --followup "<unblocker task>"'
echo "Each followup will be created as a new lap at the head of the queue."
echo "For the summary, include what you tried, what failed, what you suspect, relevant current-state findings, and any test assertions you changed."
