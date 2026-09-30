#!/usr/bin/env bash
# Live harness smoke test. Installs the scripts/live/templates profile for each
# harness into a fresh git workspace, then checks with the harness's own CLI
# (no model calls) that the files load, and that ocws status / re-apply /
# remove behave.
#
# Usage: live-harnesses.sh [harness...]      (default: all)
# Env:   OCWS=path/to/ocws  TEMPLATES=path/to/scripts/live/templates
#        LIVE=~/ocws-live (workspaces, isolated homes, logs, CLI prefixes)
set -uo pipefail

LIVE=${LIVE:-$HOME/ocws-live}
OCWS=${OCWS:-$LIVE/ocws}
TEMPLATES=${TEMPLATES:-$LIVE/templates}
BASE_PATH="$LIVE/npm/bin:$HOME/.local/bin:$PATH"
ALL=(opencode opencode-v1 claude codex gemini qwen copilot cursor droid kiro amp crush goose cline kilo pi hermes)
HARNESSES=("${@:-${ALL[@]}}")
[ $# -eq 0 ] && HARNESSES=("${ALL[@]}")

declare -A RESULT NOTE
REAL_HOME=$HOME

# check <description> <expect-substring|-> <cmd...>: run a model-free CLI
# command with a timeout; fail if it exits non-zero or lacks the substring.
# Retries a few times: some CLIs (OpenCode V2) answer from a background
# service that may still be loading the project.
check() {
	local what=$1 expect=$2
	shift 2
	local out rc try
	for try in 1 2 3; do
		out=$(timeout 90 "$@" </dev/null 2>&1)
		rc=$?
		printf '$ %s  (try %s)\n%s\n[rc=%s]\n' "$*" "$try" "$out" "$rc" >>"$LOG"
		if [ $rc -eq 0 ] && { [ "$expect" = "-" ] || grep -qiF -- "$expect" <<<"$out"; }; then
			return
		fi
		sleep 5
	done
	if [ $rc -ne 0 ]; then
		FAILS+=("$what: exit $rc")
	else
		FAILS+=("$what: missing '$expect'")
	fi
}

# tool_call <label> <fake-tool> <fake-args-json> [opencode run flags...]:
# point OpenCode at scripts/live/fakellm.py, have the fake model call the
# live_echo custom tool (directly, or through V2's Code Mode `execute`), and
# check the tool's output came back. Proves the tool loads and runs.
tool_call() {
	local what=$1 tool=$2 args=$3
	shift 3
	local port=$((48300 + RANDOM % 500)) flog="$LIVE/logs/$h-fakellm.jsonl"
	: >"$flog"
	python3 "$LIVE/fakellm.py" "$port" "$flog" "$tool" "$args" >/dev/null 2>&1 &
	local fpid=$!
	sleep 1
	mkdir -p "$HOME/.config/opencode"
	printf '{"provider":{"fake":{"npm":"@ai-sdk/openai-compatible","name":"Fake","options":{"baseURL":"http://127.0.0.1:%s/v1","apiKey":"x"},"models":{"fake":{"name":"fake"}}}},"model":"fake/fake","permission":{"*":"allow"}}\n' "$port" >"$HOME/.config/opencode/opencode.json"
	timeout 300 opencode run "$@" -m fake/fake "run the live check" </dev/null >>"$LOG" 2>&1
	kill $fpid 2>/dev/null
	rm -f "$HOME/.config/opencode/opencode.json"
	if ! python3 -c 'import json,sys; sys.exit(0 if any(m.get("role")=="tool" and "echo: hello hello" in str(m.get("content")) for l in open(sys.argv[1]) for m in (json.loads(l).get("messages") or [])) else 1)' "$flog"; then
		FAILS+=("$what: live_echo result not returned")
	fi
}

# crush_mcp: run Crush against scripts/live/fakellm.py and check the model
# request offers the live-time MCP tools, i.e. the project .crushrc loaded.
crush_mcp() {
	local port=$((48300 + RANDOM % 500)) flog="$LIVE/logs/$h-fakellm.jsonl"
	: >"$flog"
	python3 "$LIVE/fakellm.py" "$port" "$flog" >/dev/null 2>&1 &
	local fpid=$!
	sleep 1
	mkdir -p "$HOME/.config/crush"
	printf 'provider add fake --type openai-compat --base-url "http://127.0.0.1:%s/v1" --api-key x\nmodel add fake/fake --name fake --context-window 128000 --default-max-tokens 1000\nmodel large fake/fake\nmodel small fake/fake\n' "$port" >"$HOME/.config/crush/crushrc"
	timeout 180 crush run "say ok" </dev/null >>"$LOG" 2>&1
	kill $fpid 2>/dev/null
	if ! python3 -c 'import json,sys; sys.exit(0 if any("live-time" in (t.get("function") or t).get("name","") for l in open(sys.argv[1]) for t in (json.loads(l).get("tools") or [])) else 1)' "$flog"; then
		FAILS+=("crushrc MCP: live-time tools not offered")
	fi
}

# trust <harness>: record the per-harness "trust this project" decision in
# the isolated home, as a user would on first run.
trust() {
	case $1 in
	codex) mkdir -p "$HOME/.codex" && printf '[projects."%s"]\ntrust_level = "trusted"\n' "$WS" >"$HOME/.codex/config.toml" ;;
	copilot) mkdir -p "$HOME/.copilot" && printf '{"trusted_folders": ["%s"]}\n' "$WS" >"$HOME/.copilot/config.json" ;;
	pi) mkdir -p "$HOME/.pi/agent" && printf '{"%s": true}\n' "$WS" >"$HOME/.pi/agent/trust.json" ;;
	hermes) hermes skills trust "$WS" >>"$LOG" 2>&1 ;;
	esac
}

# verify <harness>: the harness's own view of the workspace.
verify() {
	case $1 in
	opencode)
		PATH="$LIVE/npm-v2/bin:$PATH" check "mcp list" live-time opencode mcp list
		PATH="$LIVE/npm-v2/bin:$PATH" check "agents" live-reviewer opencode debug agents
		# V2 registers the V1 tool through the ocws plugin shim, in Code Mode.
		PATH="$LIVE/npm-v2/bin:$PATH" tool_call "custom tool" execute '{"code":"return await tools.live_echo({ text: \"hello\", repeat: 2 })"}' --standalone
		;;
	opencode-v1)
		PATH="$LIVE/npm-v1/bin:$PATH" check "mcp list" live-time opencode mcp list
		PATH="$LIVE/npm-v1/bin:$PATH" check "agent list" live-reviewer opencode agent list
		PATH="$LIVE/npm-v1/bin:$PATH" check "skills" live-check opencode debug skill
		PATH="$LIVE/npm-v1/bin:$PATH" tool_call "custom tool" live_echo '{"text":"hello","repeat":2}'
		;;
	claude) check "mcp list" live-time claude mcp list ;;
	codex) check "mcp list" live-time codex mcp list ;;
	gemini)
		GEMINI_CLI_TRUST_WORKSPACE=true check "mcp list" live-time gemini mcp list
		GEMINI_CLI_TRUST_WORKSPACE=true check "skills list" live-check gemini skills list --all
		;;
	qwen) check "mcp list" live-time qwen mcp list ;;
	copilot)
		check "mcp list" live-time copilot mcp list
		check "skill list" live-check copilot skill list
		;;
	cursor) check "version" - agent --version ;;
	droid) check "version" - droid --version ;;
	kiro) check "version (listing needs login)" - kiro-cli --version ;;
	amp) check "version (listing needs login)" - amp --version ;;
	crush) crush_mcp ;;
	goose) check "skills list" live-check goose skills list ;;
	cline) check "version" - cline --version ;;
	kilo)
		check "mcp list" live-time kilo mcp list
		check "agent list" live-reviewer kilo agent list
		;;
	pi) check "mcp list" "live-time: connected" pi mcp list ;;
	hermes) check "skills list" live-check hermes skills list ;;
	esac
}

mkdir -p "$LIVE/logs"
for h in "${HARNESSES[@]}"; do
	WS="$LIVE/ws-$h"
	# Hermes' launcher needs the real HOME, and its runtime lives in
	# HERMES_HOME, so keep the install's own home (never wiped here).
	if [ "$h" = hermes ]; then
		export HOME="$REAL_HOME" HERMES_HOME="${HERMES_HOME:-$LIVE/hermes-home}"
	else
		export HOME="$LIVE/home-$h"
	fi
	LOG="$LIVE/logs/$h.log"
	FAILS=()
	rm -rf "$WS" "$LIVE/home-$h"
	mkdir -p "$WS" "$LIVE/home-$h"
	: >"$LOG"
	export PATH="$BASE_PATH"
	cd "$WS" || exit 1
	git init -q && git -c user.email=t@t -c user.name=t commit -q --allow-empty -m init

	if ! "$OCWS" --templates "$TEMPLATES" apply -p live --harness "$h" --agents create >>"$LOG" 2>&1; then
		FAILS+=("ocws apply failed")
	else
		trust "$h"
		verify "$h"
		if ! "$OCWS" --templates "$TEMPLATES" status --json 2>>"$LOG" | python3 -c 'import json,sys; c=json.load(sys.stdin)["components"]; bad=[x["id"] for x in c if x["refreshState"]!="current"]; sys.exit(1 if bad or not c else 0)'; then
			FAILS+=("status not current")
		fi
		# .ocws/manifest.json carries a timestamp, so compare everything else.
		git add -A -- . ':!.ocws' && before=$(git write-tree)
		"$OCWS" --templates "$TEMPLATES" apply -p live --harness "$h" --agents create >>"$LOG" 2>&1
		git add -A -- . ':!.ocws' && [ "$(git write-tree)" = "$before" ] || FAILS+=("re-apply changed files")
		git reset -q
		ids=$("$OCWS" --templates "$TEMPLATES" status --json | python3 -c 'import json,sys; print(" ".join(x["harness"]+":"+x["id"] for x in json.load(sys.stdin)["components"]))')
		# shellcheck disable=SC2086
		"$OCWS" --templates "$TEMPLATES" remove $ids >>"$LOG" 2>&1 || FAILS+=("ocws remove failed")
		# Nothing the packs installed may remain. Instruction files, the
		# workspace config and files the harness itself wrote (Qwen's
		# "$version", Kilo's .kilo/.gitignore) may stay, but none of them may
		# still mention the pack's live-* names.
		for f in $(git ls-files --others --exclude-standard); do
			case $f in .ocws/*) continue ;; esac
			grep -q 'live-' "$f" 2>/dev/null && FAILS+=("left after remove: $f")
		done
	fi
	if [ ${#FAILS[@]} -eq 0 ]; then
		RESULT[$h]=PASS
	else
		RESULT[$h]=FAIL
		NOTE[$h]=$(printf '%s; ' "${FAILS[@]}")
	fi
	printf '%-12s %s %s\n' "$h" "${RESULT[$h]}" "${NOTE[$h]:-}"
done

echo
printf '%-12s %-5s %s\n' HARNESS RESULT NOTES
rc=0
for h in "${HARNESSES[@]}"; do
	printf '%-12s %-5s %s\n' "$h" "${RESULT[$h]}" "${NOTE[$h]:-}"
	[ "${RESULT[$h]}" = PASS ] || rc=1
done
echo "logs: $LIVE/logs"
exit $rc
