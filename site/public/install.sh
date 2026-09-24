#!/bin/sh
#
#   curl -fsSL https://warmbly.com/install.sh | sh
#
# Installs Warmbly on this machine: pull the published release images, write a
# real .env and a compose file, start the stack, print the link that claims it.
# No git clone, no Go, Rust, Node or Elixir toolchain, nothing compiled here.
#
#   sh install.sh --wizard    ask where the data lives, what is kept and for
#                             how long, and how it is backed up
#   sh install.sh --dry-run   print the exact files it would write, touch nothing
#   sh install.sh --help      every flag, and the environment variable for each
#
# On reading this before running it: that is the right instinct, and it is why
# the checksum is published next to it. The short version of what it does:
#
#   * checks for docker, and offers to install it rather than doing so quietly
#   * creates one directory (default /opt/warmbly) and writes only inside it
#   * generates five secrets with openssl, writes them 0600, prints the two
#     that are unrecoverable and waits for you to say you have copied them
#   * resolves the newest release once and PINS it in .env, so every later
#     `docker compose up` in that directory is the same version. Never :latest
#   * runs `docker compose pull` and `docker compose up -d`
#
# It asks for sudo only for the steps that need it, names them when it does,
# and never wants to be piped into sudo itself. Re-running it reconfigures in
# place: it does not regenerate secrets, does not touch an existing data root,
# and refuses a directory it did not create unless you pass --force.
#
# Verify before running, if you would rather:
#
#   curl -fsSLO https://warmbly.com/install.sh
#   curl -fsSLO https://warmbly.com/install.sh.sha256
#   sha256sum -c install.sh.sha256
#   less install.sh && sh install.sh
#
# https://docs.warmbly.com/development/install/

set -eu

# ─────────────────────────────────────────────────────────────────────────
# Constants
# ─────────────────────────────────────────────────────────────────────────

SCRIPT_NAME="Warmbly installer"
REPO="warmbly/warmbly"
DEFAULT_REGISTRY="ghcr.io/warmbly/warmbly"
DEFAULT_DIR="/opt/warmbly"
DOCS="https://docs.warmbly.com"
RELEASES_API="https://api.github.com/repos/${REPO}/releases"

# The marker file that says this directory is ours. Its absence in a non-empty
# directory is what --force overrides, so the installer can never take over a
# path that belongs to something else by accident.
MARKER=".warmbly-install"

# Ports the stack publishes, in the order they are checked for collisions.
# 3000 is the one that actually collides: half of self-hosted software wants it.
PORT_BACKEND=8080
PORT_WEB=5173
PORT_ADMIN=5174
PORT_TRACKING=3000
PORT_REALTIME=4000
PORT_FORMS=8090

# ─────────────────────────────────────────────────────────────────────────
# Answers. Every one is a flag, an environment variable and a wizard question,
# and the defaults here are exactly what the fast path installs.
# ─────────────────────────────────────────────────────────────────────────

DIR="${WARMBLY_DIR:-$DEFAULT_DIR}"
HOSTNAME_ANSWER="${WARMBLY_HOST:-localhost}"
TLS="${WARMBLY_TLS:-none}"
DATA_ROOT="${WARMBLY_DATA_ROOT:-}"          # empty = <dir>/data; "volumes" = docker named volumes
BLOBS="${WARMBLY_BLOBS:-filesystem}"        # filesystem | s3
COMPONENTS="${WARMBLY_COMPONENTS:-full}"    # core | full
VERSION="${WARMBLY_VERSION:-}"              # empty = resolve the newest release
CHANNEL="${WARMBLY_CHANNEL:-stable}"
REGISTRY="${WARMBLY_IMAGE_PREFIX:-$DEFAULT_REGISTRY}"
EXTERNAL_DB="${WARMBLY_DATABASE_URL:-}"
EXTERNAL_REDIS="${WARMBLY_REDIS_URL:-}"
MAIL_MODE="${WARMBLY_MAIL:-log}"            # log | smtp
REGISTRATION="${WARMBLY_REGISTRATION:-invite_only}"
UPDATE_CHECK="${WARMBLY_UPDATE_CHECK:-true}"
BOOTSTRAP_EMAIL="${WARMBLY_BOOTSTRAP_EMAIL:-}"
BOOTSTRAP_HASH="${WARMBLY_BOOTSTRAP_PASSWORD_HASH:-}"
BACKUP_DIR="${WARMBLY_BACKUP_DIR:-}"        # empty = no scheduled backup
BACKUP_KEEP="${WARMBLY_BACKUP_KEEP:-14}"
BACKUP_SCHEDULE="${WARMBLY_BACKUP_SCHEDULE:-daily}"
BACKUP_KEYS="${WARMBLY_BACKUP_KEYS:-true}"
BACKUP_S3="${WARMBLY_BACKUP_S3:-}"

# Retention and sync, the data-control answers. They are written into .env as
# one WARMBLY_SETTINGS_BOOTSTRAP document, applied on first boot and editable
# in Instance > Instance settings from then on.
SYNC_BACKFILL_DAYS="${WARMBLY_SYNC_BACKFILL_DAYS:-90}"
SYNC_BACKFILL_MESSAGES="${WARMBLY_SYNC_BACKFILL_MESSAGES:-5000}"
SYNC_DAILY_MAILBOX="${WARMBLY_SYNC_DAILY_MAILBOX:-2000}"
SYNC_DAILY_ORG="${WARMBLY_SYNC_DAILY_ORG:-25000}"
RET_ENGAGEMENT="${WARMBLY_RETENTION_ENGAGEMENT_DAYS:-365}"
RET_FORMS="${WARMBLY_RETENTION_FORM_DAYS:-180}"
RET_AUDIT="${WARMBLY_RETENTION_AUDIT_DAYS:-90}"
RET_WARMUP_MAIL="${WARMBLY_RETENTION_WARMUP_MAIL_DAYS:-30}"
RET_WARMUP_EVENTS="${WARMBLY_RETENTION_WARMUP_EVENT_DAYS:-365}"

# S3 blob answers, only read when BLOBS=s3.
S3_BUCKET="${WARMBLY_S3_BUCKET:-}"
S3_ENDPOINT="${WARMBLY_S3_ENDPOINT:-}"
S3_REGION="${WARMBLY_S3_REGION:-auto}"
S3_KEY="${WARMBLY_S3_ACCESS_KEY_ID:-}"
S3_SECRET="${WARMBLY_S3_SECRET_ACCESS_KEY:-}"

# SMTP answers, only read when MAIL_MODE=smtp.
SMTP_HOST=""; SMTP_PORT=""; SMTP_USER=""; SMTP_PASS=""; SMTP_SECURITY="starttls"
SMTP_FROM=""

# Reverse-proxy answer, only read when TLS=proxy.
PROXY_CIDRS="${WARMBLY_TRUSTED_PROXIES:-172.16.0.0/12,10.0.0.0/8,192.168.0.0/16}"

# ─────────────────────────────────────────────────────────────────────────
# Modes
# ─────────────────────────────────────────────────────────────────────────

MODE=install            # install | uninstall
WIZARD=0
DEMO=0
ASSUME_YES=0
DRY_RUN=0
PRINT_ENV=0
FORCE=0
PURGE_DATA=0
USE_COLOR=1
# Appending is the default. The wizard reads as a transcript, nothing above it
# is touched, and there is no way for a redraw to land on top of something
# else. --clear opts into the full-screen version.
USE_CLEAR=0
INTERACTIVE=0
TTY=/dev/tty

# Filled in as the run proceeds.
RESOLVED_TAG=""
COMPOSE="docker compose"
SUDO=""
EXISTING=0

# The palette starts empty so that anything reachable before setup_term (--help,
# an unknown flag) still prints under set -u. setup_term fills it in.
ESC=""; R=""; B=""; DIM=""; SKY=""; GREEN=""; AMBER=""
RED=""; GREY=""; WHITE=""; HIDE=""; SHOW=""
COLS=80; WIDTH=76

# ─────────────────────────────────────────────────────────────────────────
# Terminal: colour, cursor, and the drawing primitives everything else uses.
#
# Every escape sequence goes through these, so NO_COLOR, a pipe, a dumb
# terminal and --no-color all degrade to plain text in one place instead of
# leaking half-rendered ANSI into a log file.
# ─────────────────────────────────────────────────────────────────────────

setup_term() {
    # A wizard needs a keyboard. Piped into sh, stdin is the script itself, so
    # the terminal is reopened explicitly; without one, only the answered
    # (flag or environment) path can run.
    if [ -t 0 ] && [ -t 1 ]; then
        INTERACTIVE=1
        TTY=/dev/stdin
    elif [ -c /dev/tty ] && ( exec 9<>/dev/tty ) 2>/dev/null && [ -t 1 ]; then
        INTERACTIVE=1
        TTY=/dev/tty
    fi

    if [ -n "${NO_COLOR:-}" ] || [ "${TERM:-dumb}" = "dumb" ] || [ ! -t 1 ]; then
        USE_COLOR=0
    fi
    if [ "$USE_COLOR" = 1 ]; then
        ESC=$(printf '\033')
        R="${ESC}[0m";   B="${ESC}[1m";   DIM="${ESC}[2m"
        SKY="${ESC}[38;5;39m"
        GREEN="${ESC}[38;5;42m"; AMBER="${ESC}[38;5;214m"; RED="${ESC}[38;5;203m"
        GREY="${ESC}[38;5;245m"; WHITE="${ESC}[38;5;255m"
        HIDE="${ESC}[?25l"; SHOW="${ESC}[?25h"
    else
        ESC=""; R=""; B=""; DIM=""; SKY=""; GREEN=""; AMBER=""
        RED=""; GREY=""; WHITE=""; HIDE=""; SHOW=""
        USE_CLEAR=0
    fi

    # Clearing a terminal that is not ours to clear is never right.
    [ "$INTERACTIVE" = 1 ] || USE_CLEAR=0

    # Width. Everything drawn inside a redraw loop is fitted to this, so
    # getting it wrong is what makes a menu draw over itself. Three sources,
    # because tput is missing on a minimal image and stty needs the terminal:
    # an exported COLUMNS wins, since that is how a caller says so explicitly.
    COLS=""
    case "${COLUMNS:-}" in ''|*[!0-9]*) ;; *) COLS=$COLUMNS ;; esac
    if [ -z "$COLS" ] && have tput; then
        COLS=$(tput cols 2>/dev/null || true)
    fi
    if [ -z "$COLS" ] && [ "$INTERACTIVE" = 1 ]; then
        COLS=$(stty size <"$TTY" 2>/dev/null | cut -d' ' -f2 || true)
    fi
    case "$COLS" in ''|*[!0-9]*) COLS=80 ;; esac
    # Clamped: a 300 column terminal should not get a 300 column rule, and
    # below the floor the boxes stop being boxes.
    [ "$COLS" -lt 44 ] && COLS=44
    [ "$COLS" -gt 92 ] && COLS=92
    WIDTH=$((COLS - 4))
}

say()  { printf '%s\n' "$*"; }
out()  { printf '%b\n' "$*"; }
outn() { printf '%b' "$*"; }

# Restore the cursor whatever happens: a Ctrl-C inside a spinner would
# otherwise leave the operator's terminal without one.
cleanup_term() {
    [ "$USE_COLOR" = 1 ] && printf '%b' "$SHOW"
    stty_sane
}
stty_sane() {
    [ "$INTERACTIVE" = 1 ] || return 0
    stty sane <"$TTY" 2>/dev/null || true
}
trap 'cleanup_term' EXIT
trap 'cleanup_term; printf "\n"; die "cancelled"' INT

# clear_screen is opt-in (--clear). Wiping someone's terminal is a thing to be
# asked for, not assumed, and a clear that the terminal declines leaves the
# wizard drawing over whatever was already there. ESC[3J is deliberately not
# sent: it erases the scrollback buffer, and the conversation above this
# command is not ours to delete.
clear_screen() {
    [ "$USE_CLEAR" = 1 ] || return 0
    printf '%b' "${ESC}[H${ESC}[2J"
}

repeat_char() {
    # $1 char, $2 count. printf pads with spaces and sed swaps them for the
    # character: no loop and no fork per character, which is what keeps the
    # redraws smooth. sed rather than tr because tr maps byte to byte and every
    # box-drawing character here is three bytes of UTF-8.
    _n=$2
    [ "$_n" -lt 1 ] && { printf ''; return; }
    printf "%${_n}s" "" | sed "s/ /$1/g"
}

rule() { out "${DIM}$(repeat_char '─' "$WIDTH")${R}"; }

# fit truncates plain text to n columns.
#
# It is what keeps the in-place redraws honest: a line longer than the terminal
# wraps onto a second physical row, and every cursor-up in this script counts
# LOGICAL rows, so one long option hint is enough to make a menu redraw over
# itself and over whatever was on screen before it. Applied to everything drawn
# inside a loop; prose printed once is free to wrap.
fit() {
    _t=$1; _w=$2
    [ "$_w" -lt 12 ] && _w=12
    if [ "${#_t}" -le "$_w" ]; then
        printf '%s' "$_t"
        return 0
    fi
    printf '%.*s...' "$((_w - 3))" "$_t"
}

# A rounded box. Content comes on stdin, one line per row, already coloured.
# The border colour is $1.
box() {
    _c="${1:-$DIM}"
    out "${_c}╭$(repeat_char '─' $((WIDTH - 2)))╮${R}"
    while IFS= read -r _line; do
        _plain=$(strip_ansi "$_line")
        # A content line wider than the box turns the border into a ragged
        # edge. Truncating costs the line its colour, which is a better trade
        # than a box that is not one.
        if [ "${#_plain}" -gt $((WIDTH - 4)) ]; then
            _line=$(fit "$_plain" $((WIDTH - 4)))
            _plain=$_line
        fi
        _pad=$((WIDTH - 4 - ${#_plain}))
        [ "$_pad" -lt 0 ] && _pad=0
        printf '%b %s%s %b\n' "${_c}│${R}" "$_line" "$(repeat_char ' ' "$_pad")" "${_c}│${R}"
    done
    out "${_c}╰$(repeat_char '─' $((WIDTH - 2)))╯${R}"
}

# strip_ansi is what makes the borders line up: the padding is computed on the
# printable text, not on the byte length of a coloured string. ${#} counts
# bytes in a POSIX shell, so box CONTENT is kept ASCII; the border itself is
# printed rather than measured, and may be anything.
strip_ansi() {
    printf '%s' "$1" | sed "s/$(printf '\033')\[[0-9;]*m//g"
}

# ─────────────────────────────────────────────────────────────────────────
# The wordmark. Drawn once at the top of the run, one row at a time so it
# arrives rather than appears. Falls back to a single bold line when the
# terminal is not ours to animate.
# ─────────────────────────────────────────────────────────────────────────

banner() {
    if [ "$USE_COLOR" = 0 ] || [ "$COLS" -lt 60 ]; then
        # The block wordmark is 56 columns wide, so on anything narrower it
        # wraps into noise. One line says the same thing.
        say ""
        out "  ${B}${SKY}WARMBLY${R} ${DIM}installer${R}"
        say ""
        return
    fi
    printf '%b' "$HIDE"
    say ""
    _i=0
    printf '%s\n' \
"██     ██  █████  ██████  ███    ███ ██████  ██   ██  ██" \
"██  █  ██ ██   ██ ██   ██ ████  ████ ██   ██ ██    ██ ██" \
"██ ███ ██ ███████ ██████  ██ ████ ██ ██████  ██     ███ " \
"████ ████ ██   ██ ██   ██ ██  ██  ██ ██   ██ ██     ██  " \
" ██   ██  ██   ██ ██   ██ ██      ██ ██████  ██████ ██  " \
    | while IFS= read -r _row; do
        _i=$((_i + 1))
        case "$_i" in
            1|2) _tone="${ESC}[38;5;33m" ;;
            3)   _tone="${ESC}[38;5;39m" ;;
            *)   _tone="${ESC}[38;5;74m" ;;
        esac
        printf '  %b%s%b\n' "$_tone" "$_row" "$R"
        nap 0.03
    done
    printf '  %b%s%b\n\n' "$DIM" "email warmup and cold outreach, on your own machine" "$R"
    printf '%b' "$SHOW"
}

# nap sleeps a fraction of a second where the shell can, and not at all where
# it cannot. Animation is a nicety; a busybox sleep that rejects "0.03" must
# not turn into an error under set -e.
nap() {
    [ "$USE_COLOR" = 1 ] || return 0
    sleep "$1" 2>/dev/null || true
}

# ─────────────────────────────────────────────────────────────────────────
# Messages
# ─────────────────────────────────────────────────────────────────────────

info() { out "  ${SKY}·${R} $*"; }
ok()   { out "  ${GREEN}✓${R} $*"; }
warn() { out "  ${AMBER}!${R} $*"; }
# note is the explanatory prose under a question. It is wrapped to the
# terminal rather than hand-wrapped to 72 columns, because the hand-wrapped
# version is ragged on anything narrower and this is the only text in the
# script long enough to care.
note() {
    if [ "$COLS" -ge 78 ] || ! have fold; then
        out "    ${DIM}$*${R}"
        return 0
    fi
    printf '%s\n' "$*" | fold -s -w $((COLS - 6)) | while IFS= read -r _l; do
        out "    ${DIM}${_l}${R}"
    done
}
step() { out "\n  ${B}$*${R}"; }

die() {
    cleanup_term
    out "\n  ${RED}✗${R} ${B}$*${R}\n" >&2
    exit 1
}

# fail_with prints the error and, under it, what to do about it. An installer
# that stops without a next step is a support ticket.
fail_with() {
    _msg=$1; shift
    cleanup_term
    out "\n  ${RED}✗${R} ${B}${_msg}${R}" >&2
    for _l in "$@"; do out "    ${DIM}${_l}${R}" >&2; done
    out "" >&2
    exit 1
}

# ─────────────────────────────────────────────────────────────────────────
# The stepper: where the wizard is, drawn at the top of every question.
# ─────────────────────────────────────────────────────────────────────────

STEP_TITLES="Where it lives|How it is reached|Where the data sits|Keys and secrets|What is kept|Backups|Who gets in|Footprint|Review"
STEP_TOTAL=9
STEP_CURRENT=0

stepper() {
    STEP_CURRENT=$1
    if [ "$USE_CLEAR" = 1 ]; then
        clear_screen
    else
        # Appending, so the steps need their own separator or they run into
        # each other and into whatever was on screen before the installer.
        say ""
        rule
    fi
    if [ "$USE_COLOR" = 0 ]; then
        say ""
        say "Step ${STEP_CURRENT} of ${STEP_TOTAL}: $(step_title "$STEP_CURRENT")"
        say ""
        return
    fi
    say ""
    _line="  "
    _i=1
    while [ "$_i" -le "$STEP_TOTAL" ]; do
        if [ "$_i" -lt "$STEP_CURRENT" ]; then
            _line="${_line}${GREEN}●${R}"
        elif [ "$_i" -eq "$STEP_CURRENT" ]; then
            _line="${_line}${SKY}◉${R}"
        else
            _line="${_line}${DIM}○${R}"
        fi
        [ "$_i" -lt "$STEP_TOTAL" ] && _line="${_line}${DIM}──${R}"
        _i=$((_i + 1))
    done
    out "$_line   ${DIM}step ${STEP_CURRENT}/${STEP_TOTAL}${R}"
    out "  ${B}${WHITE}$(step_title "$STEP_CURRENT")${R}"
    say ""
}

step_title() {
    printf '%s' "$STEP_TITLES" | cut -d'|' -f"$1"
}

# ─────────────────────────────────────────────────────────────────────────
# Spinner. Runs a command in the background and animates until it finishes,
# then leaves one settled line behind: a tick, or a cross and the log.
# ─────────────────────────────────────────────────────────────────────────

SPIN_FRAMES="⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

# spin <label> <command...>
spin() {
    _label=$1; shift
    if [ "$USE_COLOR" = 0 ]; then
        say "  ... $_label"
        if "$@" >"$LOGFILE" 2>&1; then say "  ok  $_label"; return 0; fi
        return 1
    fi

    "$@" >"$LOGFILE" 2>&1 &
    _pid=$!
    printf '%b' "$HIDE"
    _f=1
    while kill -0 "$_pid" 2>/dev/null; do
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        printf '\r  %b%s%b %s' "$SKY" "$_frame" "$R" "$_label"
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.08
    done
    wait "$_pid" 2>/dev/null && _rc=0 || _rc=$?
    printf '\r%b' "${ESC}[2K"
    printf '%b' "$SHOW"
    if [ "$_rc" = 0 ]; then
        ok "$_label"
    else
        out "  ${RED}✗${R} $_label"
    fi
    return "$_rc"
}

# ─────────────────────────────────────────────────────────────────────────
# Progress bar. Used for the pull and for the health wait, where there is a
# real fraction to show rather than an indeterminate spin.
# ─────────────────────────────────────────────────────────────────────────

# bar <done> <total> <width>
bar() {
    _done=$1; _total=$2; _w=$3
    [ "$_total" -lt 1 ] && _total=1
    _fill=$((_done * _w / _total))
    [ "$_fill" -gt "$_w" ] && _fill=$_w
    printf '%b%s%b%s%b' "$SKY" "$(repeat_char '━' "$_fill")" "$DIM" "$(repeat_char '━' $((_w - _fill)))" "$R"
}

elapsed_since() {
    _s=$(( $(now_s) - $1 ))
    printf '%dm%02ds' $((_s / 60)) $((_s % 60))
}
now_s() { date +%s; }

# ─────────────────────────────────────────────────────────────────────────
# Input
#
# The wizard reads the keyboard, not stdin: piped into sh, stdin IS the
# script. Everything below reads $TTY, and every one of them has an answered
# path so that --yes and the environment variables reach the same code.
# ─────────────────────────────────────────────────────────────────────────

# A terminal that cannot be put into raw mode is not fatal: the menus still
# draw, they just read a whole line instead of a keypress.
raw_on() {
    [ "$INTERACTIVE" = 1 ] || return 0
    stty raw -echo <"$TTY" 2>/dev/null || true
}
raw_off() {
    [ "$INTERACTIVE" = 1 ] || return 0
    stty -raw echo <"$TTY" 2>/dev/null || true
}

# read_key leaves the decimal code of one keypress in KEY. Arrow keys arrive
# as three bytes; the escape prefix is consumed here so callers only ever see
# UP, DOWN, ENTER and the printable code.
read_key() {
    KEY=$(dd bs=1 count=1 2>/dev/null <"$TTY" | od -An -tu1 | tr -d ' \n')
    [ -n "$KEY" ] || KEY=0
    if [ "$KEY" = 27 ]; then
        _b=$(dd bs=1 count=1 2>/dev/null <"$TTY" | od -An -tu1 | tr -d ' \n')
        if [ "$_b" = 91 ]; then
            _c=$(dd bs=1 count=1 2>/dev/null <"$TTY" | od -An -tu1 | tr -d ' \n')
            case "$_c" in
                65) KEY=UP ;;
                66) KEY=DOWN ;;
                *)  KEY=OTHER ;;
            esac
        else
            KEY=ESC
        fi
    fi
    # Normalised here rather than in each caller, because every consumer of
    # read_key is a menu and they should all answer to the same keys.
    case "$KEY" in
        10|13)   KEY=ENTER ;;
        32|108)  KEY=SELECT ;;    # space, l
        107|16)  KEY=UP ;;        # k, ctrl-p
        106|14)  KEY=DOWN ;;      # j, ctrl-n
        21)      KEY=HALFUP ;;    # ctrl-u
        4)       KEY=HALFDOWN ;;  # ctrl-d
        103)     KEY=GKEY ;;      # g, as in gg
        71)      KEY=END ;;       # G
        113)     KEY=QUIT ;;      # q
        3)       KEY=INT ;;       # ctrl-c
    esac
}

# menu_hint is the key legend under every menu. It is one rendered row, so it
# is part of the redraw arithmetic below rather than something printed once.
MENU_HINT=""
build_menu_hint() {
    # Two forms, because the long one is 64 columns before any indent and this
    # row is inside the redraw: if it wraps, the menu draws over itself.
    if [ "$COLS" -ge 78 ]; then
        MENU_HINT="${DIM}↑↓ ${WHITE}jk${DIM} move   ${WHITE}gg${DIM}/${WHITE}G${DIM} first, last   ${WHITE}1-9${DIM} jump   ${WHITE}enter${DIM} select   ${WHITE}q${DIM} quit${R}"
    else
        MENU_HINT="${DIM}${WHITE}jk${DIM} move   ${WHITE}1-9${DIM} jump   ${WHITE}enter${DIM} select   ${WHITE}q${DIM} quit${R}"
    fi
}

# choose <default-index> <"label|hint"> ...  -> CHOICE (1-based)
#
# Arrows, j/k, ctrl-n/ctrl-p, ctrl-d/ctrl-u, gg and G all move; a digit jumps
# straight to an option; enter, space or l takes it; q or escape leaves.
# Answered runs take the default without drawing anything.
choose() {
    _default=$1; shift
    CHOICE=$_default
    _count=$#
    # --yes means every unanswered question takes its default, and a menu is a
    # question. Without this the flag only silenced the confirmations, which is
    # not what it says it does. The choice is still echoed: the question above
    # it was printed, and a header with nothing under it reads as a bug.
    if [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ]; then
        _i=1
        for _opt in "$@"; do
            if [ "$_i" = "$CHOICE" ]; then
                out "  ${GREEN}✓${R} ${WHITE}$(fit "${_opt%%|*}" $((COLS - 6)))${R}"
                say ""
            fi
            _i=$((_i + 1))
        done
        return 0
    fi
    build_menu_hint
    # Two rows per option, plus the key legend. Every redraw moves up by this,
    # so the legend has to be counted rather than printed once above.
    _rows=$((_count * 2 + 1))
    _half=$(((_count + 1) / 2))

    _sel=$_default
    _g=0
    printf '%b' "$HIDE"
    _first=1
    while :; do
        [ "$_first" = 1 ] || printf '%b' "${ESC}[${_rows}A"
        _first=0
        _i=1
        for _opt in "$@"; do
            _label=$(fit "${_opt%%|*}" $((COLS - 6)))
            _hint=${_opt#*|}
            [ "$_hint" = "$_opt" ] && _hint=""
            [ -n "$_hint" ] && _hint=$(fit "$_hint" $((COLS - 8)))
            printf '%b' "${ESC}[2K"
            if [ "$_i" = "$_sel" ]; then
                out "  ${SKY}❯${R} ${B}${WHITE}${_label}${R}"
            else
                out "    ${GREY}${_label}${R}"
            fi
            printf '%b' "${ESC}[2K"
            if [ -n "$_hint" ]; then
                out "      ${DIM}${_hint}${R}"
            else
                say ""
            fi
            _i=$((_i + 1))
        done
        printf '%b' "${ESC}[2K"
        out "  $MENU_HINT"

        raw_on
        read_key
        raw_off
        # gg is the only two-key sequence, so any other key ends a pending g.
        [ "$KEY" = "GKEY" ] || _g=0
        case "$KEY" in
            UP)       _sel=$((_sel - 1)); [ "$_sel" -lt 1 ] && _sel=$_count ;;
            DOWN)     _sel=$((_sel + 1)); [ "$_sel" -gt "$_count" ] && _sel=1 ;;
            HALFUP)   _sel=$((_sel - _half)); [ "$_sel" -lt 1 ] && _sel=1 ;;
            HALFDOWN) _sel=$((_sel + _half)); [ "$_sel" -gt "$_count" ] && _sel=$_count ;;
            GKEY)     if [ "$_g" = 1 ]; then _sel=1; _g=0; else _g=1; fi ;;
            END)      _sel=$_count ;;
            ENTER|SELECT) break ;;
            QUIT|ESC|INT) printf '%b' "$SHOW"; die "cancelled" ;;
            *)
                # A digit picks that option outright: everyone tries it.
                case "$KEY" in
                    49|50|51|52|53|54|55|56|57)
                        _n=$((KEY - 48))
                        [ "$_n" -le "$_count" ] && { _sel=$_n; break; }
                        ;;
                esac
                ;;
        esac
    done
    printf '%b' "$SHOW"
    CHOICE=$_sel

    # Collapse the menu to the one line that says what was chosen, so the
    # wizard reads back as a list of decisions rather than a wall of options.
    _chosen=""
    _i=1
    for _opt in "$@"; do
        [ "$_i" = "$CHOICE" ] && _chosen=${_opt%%|*}
        _i=$((_i + 1))
    done
    printf '%b' "${ESC}[${_rows}A"
    _i=0
    while [ "$_i" -lt "$_rows" ]; do
        printf '%b
' "${ESC}[2K"
        _i=$((_i + 1))
    done
    printf '%b' "${ESC}[${_rows}A"
    out "  ${GREEN}✓${R} ${WHITE}$(fit "$_chosen" $((COLS - 6)))${R}"
    say ""
}


# ask <prompt> <default> -> ANSWER. An empty answer takes the default, which
# is shown, so pressing enter through the whole wizard is the fast path.
ask() {
    _prompt=$1; _default=${2:-}
    ANSWER=$_default
    [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ] && return 0
    if [ -n "$_default" ]; then
        printf '  %b%s%b %b[%s]%b ' "$WHITE" "$_prompt" "$R" "$DIM" "$_default" "$R"
    else
        printf '  %b%s%b %b(optional)%b ' "$WHITE" "$_prompt" "$R" "$DIM" "$R"
    fi
    IFS= read -r _in <"$TTY" || _in=""
    # An `if`, not `[ -n ] && ...`: the latter would be this function's exit
    # status, so pressing enter to take the default would look like a failure
    # to set -e and end the run.
    if [ -n "$_in" ]; then ANSWER=$_in; fi
}

# ask_secret is ask with the echo off, for an SMTP password or an S3 key.
ask_secret() {
    _prompt=$1
    ANSWER=""
    [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ] && return 0
    printf '  %b%s%b ' "$WHITE" "$_prompt" "$R"
    stty -echo <"$TTY" 2>/dev/null || true
    IFS= read -r ANSWER <"$TTY" || ANSWER=""
    stty echo <"$TTY" 2>/dev/null || true
    say ""
}

# ask_number keeps asking until the answer is a whole number in range, and
# says what is wrong inline instead of clamping something the operator meant.
ask_number() {
    _prompt=$1; _default=$2; _min=$3; _max=$4
    while :; do
        ask "$_prompt" "$_default"
        case "$ANSWER" in
            ''|*[!0-9]*) ;;
            *)
                if [ "$ANSWER" -ge "$_min" ] && [ "$ANSWER" -le "$_max" ]; then
                    return 0
                fi
                ;;
        esac
        [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ] && { ANSWER=$_default; return 0; }
        out "    ${RED}Enter a whole number between ${_min} and ${_max}.${R}"
    done
}

# press_enter holds the screen until the operator is ready.
#
# It exists because the wizard clears between steps: without it the banner, the
# demo notice and anything else above the first question are wiped a fraction
# of a second after they are drawn, which reads as the whole thing flashing
# past. Any key continues; q or escape leaves.
press_enter() {
    _label=${1:-Press enter to begin}
    [ "$INTERACTIVE" = 1 ] || return 0
    [ "$ASSUME_YES" = 1 ] && return 0
    printf '  %b%s%b' "$DIM" "$_label" "$R"
    raw_on
    read_key
    raw_off
    case "$KEY" in
        QUIT|ESC|INT) say ""; die "nothing was changed" ;;
    esac
    clear_line
    return 0
}

# offer_wizard asks a fresh install whether to configure itself, when nothing
# already answered that.
#
# `curl ... | sh` with no flags used to go straight to defaults, which means
# localhost URLs and a tracking host on localhost: an instance that installs,
# starts, and sends campaign mail whose links and opt-out address nobody
# outside that machine can open. It is a terminal in front of a person, so it
# can just ask. Declining is a real answer and says what it costs.
#
# Skipped whenever something else already decided: --wizard, --assume-yes, a
# non-interactive shell, a re-run over an existing install (its .env is
# adopted), and any run that writes nothing.
offer_wizard() {
    [ "$WIZARD" = 0 ] || return 0
    [ "$INTERACTIVE" = 1 ] || return 0
    [ "$ASSUME_YES" = 0 ] || return 0
    [ "$EXISTING" = 0 ] || return 0
    [ "$DEMO" = 0 ] || return 0
    [ "$DRY_RUN" = 0 ] || return 0
    [ "$PRINT_ENV" = 0 ] || return 0
    # An operator who named a host has already answered the question that
    # matters, by flag or by WARMBLY_HOST.
    [ -z "${WARMBLY_HOST:-}" ] || return 0
    [ -z "${HOST_SET:-}" ] || return 0

    say ""
    note "Nothing has told this install where it will be reached, so it would use"
    note "localhost: a dashboard, a tracking host and an unsubscribe address that"
    note "only work from this machine. Campaign mail carries those addresses."
    say ""
    if confirm "Answer a few questions to set it up properly?" yes; then
        WIZARD=1
        return 0
    fi
    say ""
    warn "Going with the defaults. Every address this instance builds is on"
    note "localhost, so campaign mail would carry a pixel, links and an opt-out"
    note "no recipient can open. Fine to look around; edit .env or re-run with"
    note "--wizard before you send anything real."
    say ""
    return 0
}

# confirm <question> <default yes|no> -> 0 when yes
confirm() {
    _q=$1; _def=${2:-yes}
    if [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ]; then
        [ "$_def" = "yes" ]
        return
    fi
    if [ "$_def" = "yes" ]; then _hint="Y/n"; else _hint="y/N"; fi
    printf '  %b%s%b %b[%s]%b ' "$WHITE" "$_q" "$R" "$DIM" "$_hint" "$R"
    IFS= read -r _a <"$TTY" || _a=""
    case "$_a" in
        [yY]*) return 0 ;;
        [nN]*) return 1 ;;
        *)     [ "$_def" = "yes" ] ;;
    esac
}

# confirm_phrase makes the operator type something specific. Reserved for the
# two moments where a wrong keystroke is unrecoverable: the encryption keys,
# and --purge-data.
confirm_phrase() {
    _q=$1; _phrase=$2
    [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ] && return 0
    printf '  %b%s%b ' "$WHITE" "$_q" "$R"
    IFS= read -r _a <"$TTY" || _a=""
    [ "$_a" = "$_phrase" ]
}

# ─────────────────────────────────────────────────────────────────────────
# Host checks
# ─────────────────────────────────────────────────────────────────────────

have() { command -v "$1" >/dev/null 2>&1; }

detect_platform() {
    OS=$(uname -s 2>/dev/null || echo unknown)
    ARCH=$(uname -m 2>/dev/null || echo unknown)
    case "$ARCH" in
        x86_64|amd64) ARCH=amd64 ;;
        aarch64|arm64) ARCH=arm64 ;;
    esac
    case "$OS" in
        Linux|Darwin) ;;
        *) fail_with "Warmbly's release images are Linux containers and $OS is not supported by this installer." \
                     "Docker Desktop on Windows runs them: install it, then run this script inside WSL." \
                     "$DOCS/development/deployment-guide/" ;;
    esac
    case "$ARCH" in
        amd64|arm64) ;;
        *) fail_with "The release images are published for amd64 and arm64, and this machine is $ARCH." \
                     "Build from source instead: $DOCS/development/deployment-guide/" ;;
    esac
}

# need_sudo decides how privileged steps run. The script is never meant to be
# piped into sudo: it asks for elevation only where it needs it, and says which
# step wanted it.
need_sudo() {
    if [ "$(id -u)" = 0 ]; then
        SUDO=""
        return 0
    fi
    if have sudo; then
        SUDO="sudo"
        return 0
    fi
    return 1
}

check_docker() {
    if have docker && docker info >/dev/null 2>&1; then
        if docker compose version >/dev/null 2>&1; then
            COMPOSE="docker compose"
            return 0
        fi
        if have docker-compose; then
            COMPOSE="docker-compose"
            warn "Using the standalone docker-compose. Compose v2 (docker compose) is what this is tested against."
            return 0
        fi
        fail_with "Docker is running but the Compose plugin is missing." \
                  "Install it: https://docs.docker.com/compose/install/"
    fi

    if have docker; then
        fail_with "Docker is installed but not answering." \
                  "The daemon is probably not running, or your user is not in the docker group." \
                  "Try:  sudo systemctl start docker" \
                  "and:  sudo usermod -aG docker \$USER   (then log out and back in)"
    fi

    offer_docker
}

# offer_docker installs Docker only after saying so and being told yes. It runs
# the official convenience script, which is the same one Docker documents.
offer_docker() {
    if [ "$OS" = "Darwin" ]; then
        fail_with "Docker is not installed." \
                  "On macOS, install Docker Desktop and start it, then run this again:" \
                  "https://docs.docker.com/desktop/install/mac-install/"
    fi
    say ""
    out "  ${AMBER}!${R} ${B}Docker is not installed on this machine.${R}"
    note "Warmbly runs as containers, so it is a hard requirement."
    note "The installer can run Docker's own convenience script for you:"
    note "  curl -fsSL https://get.docker.com | sudo sh"
    say ""
    if ! confirm "Install Docker now?" yes; then
        fail_with "Docker is required." "Install it and run this again: https://docs.docker.com/engine/install/"
    fi
    need_sudo || fail_with "Installing Docker needs root and sudo is not available." \
                           "Run this script as root, or install Docker yourself first."
    info "Installing Docker (this is the only step that uses sudo)"
    curl -fsSL https://get.docker.com -o "$TMPDIR_RUN/get-docker.sh" \
        || fail_with "Could not download Docker's install script."
    if ! spin "Installing Docker" $SUDO sh "$TMPDIR_RUN/get-docker.sh"; then
        show_log
        fail_with "Docker's installer did not finish." "Install it by hand and run this again."
    fi
    if [ -n "$SUDO" ]; then
        $SUDO usermod -aG docker "$(id -un)" 2>/dev/null || true
        note "Your user was added to the docker group. It takes effect at your next login;"
        note "this run keeps using sudo for docker."
        DOCKER_NEEDS_SUDO=1
    fi
    have docker || fail_with "Docker still is not on PATH after installing."
}

# docker_cmd runs docker with sudo when the current session is not yet in the
# docker group, which is the state right after the group was added.
DOCKER_NEEDS_SUDO=0
dockerc() {
    if [ "$DOCKER_NEEDS_SUDO" = 1 ]; then
        $SUDO docker "$@"
    else
        docker "$@"
    fi
}
composec() {
    if [ "$DOCKER_NEEDS_SUDO" = 1 ]; then
        $SUDO docker compose "$@"
    else
        # shellcheck disable=SC2086
        $COMPOSE "$@"
    fi
}

# port_busy answers whether something already listens on a port, using
# whichever of the three usual tools this host happens to have.
port_busy() {
    _p=$1
    if have ss; then
        ss -ltn 2>/dev/null | grep -qE "[:.]${_p}[[:space:]]" && return 0
    elif have netstat; then
        netstat -ltn 2>/dev/null | grep -qE "[:.]${_p}[[:space:]]" && return 0
    elif have lsof; then
        lsof -iTCP:"$_p" -sTCP:LISTEN >/dev/null 2>&1 && return 0
    fi
    return 1
}

check_ports() {
    _busy=""
    for _p in $1; do
        port_busy "$_p" && _busy="$_busy $_p"
    done
    printf '%s' "${_busy# }"
}

# ─────────────────────────────────────────────────────────────────────────
# Secrets
# ─────────────────────────────────────────────────────────────────────────

gen_hex() {
    if have openssl; then openssl rand -hex "$1"; return; fi
    od -An -tx1 -N"$1" /dev/urandom | tr -d ' \n'
}
gen_b64() {
    if have openssl; then openssl rand -base64 "$1" | tr -d '\n'; return; fi
    head -c "$1" /dev/urandom | base64 | tr -d '\n'
}

require_randomness() {
    if ! have openssl && [ ! -r /dev/urandom ]; then
        fail_with "Neither openssl nor /dev/urandom is available, so secrets cannot be generated." \
                  "Install openssl and run this again. Warmbly will not start with guessable keys."
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# Releases
#
# The tag is resolved ONCE and written into .env, so every later
# `docker compose up` in the install directory brings up the same version.
# The default is never a moving tag.
# ─────────────────────────────────────────────────────────────────────────

resolve_version() {
    if [ -n "$VERSION" ]; then
        RESOLVED_TAG=$VERSION
        return 0
    fi
    _url="$RELEASES_API/latest"
    [ "$CHANNEL" = "dev" ] && _url="$RELEASES_API?per_page=10"
    _json=$(curl -fsSL -H 'Accept: application/vnd.github+json' "$_url" 2>/dev/null || true)
    RESOLVED_TAG=$(printf '%s' "$_json" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
    if [ -z "$RESOLVED_TAG" ]; then
        fail_with "Could not read the newest release from GitHub." \
                  "Pass the version you want instead:  --version v1.0.0" \
                  "Releases: https://github.com/$REPO/releases"
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# Derived values
#
# One place turns the answers into the URLs, paths and provider settings the
# services read, so --dry-run prints exactly what a real run would write.
# ─────────────────────────────────────────────────────────────────────────

derive() {
    # Data root. Empty means "under the install directory"; the literal string
    # "volumes" means Docker's own named volumes. Compose reads a source
    # starting with / as a bind mount and anything else as a named volume, so
    # one variable per store covers both with no second compose file.
    if [ "$DATA_ROOT" = "volumes" ]; then
        V_PG=warmbly_postgres; V_REDIS=warmbly_redis; V_NATS=warmbly_nats
        V_BLOBS=warmbly_blobs; V_WORKER=warmbly_worker; V_UPDATER=warmbly_updater
        DATA_DESC="Docker named volumes"
    else
        [ -n "$DATA_ROOT" ] || DATA_ROOT="$DIR/data"
        V_PG="$DATA_ROOT/postgres"; V_REDIS="$DATA_ROOT/redis"; V_NATS="$DATA_ROOT/nats"
        V_BLOBS="$DATA_ROOT/blobs"; V_WORKER="$DATA_ROOT/worker"; V_UPDATER="$DATA_ROOT/updater"
        DATA_DESC="$DATA_ROOT"
    fi

    # URLs. Three shapes, and the difference between them is what an operator
    # gets wrong by hand every time.
    case "$TLS" in
        caddy)
            SCHEME=https
            H_APP="app.$HOSTNAME_ANSWER"; H_API="api.$HOSTNAME_ANSWER"
            H_ADMIN="admin.$HOSTNAME_ANSWER"; H_WS="ws.$HOSTNAME_ANSWER"
            H_TRACK="track.$HOSTNAME_ANSWER"; H_FORMS="forms.$HOSTNAME_ANSWER"
            URL_APP="https://$H_APP"; URL_API="https://$H_API"; URL_ADMIN="https://$H_ADMIN"
            URL_WS="wss://$H_WS/socket/websocket"
            TRACKING_DOMAIN="$H_TRACK"; FORMS_DOMAIN="$H_FORMS"
            PHX_HOST="$H_WS"
            # The origins a browser connects FROM, which are the dashboard and
            # the admin panel, not this service's own host.
            CHECK_ORIGIN_HOSTS="$URL_APP,$URL_ADMIN"
            # Caddy sits on the same compose network, so the private ranges are
            # the honest answer here rather than a single container address that
            # changes on every recreate.
            TRUSTED="172.16.0.0/12,10.0.0.0/8,192.168.0.0/16"
            BIND="127.0.0.1:"
            ;;
        proxy)
            SCHEME=https
            H_APP="$HOSTNAME_ANSWER"; H_API="$HOSTNAME_ANSWER"; H_ADMIN="$HOSTNAME_ANSWER"
            URL_APP="${WARMBLY_APP_URL:-https://$HOSTNAME_ANSWER}"
            URL_API="${WARMBLY_API_URL:-https://api.$HOSTNAME_ANSWER}"
            URL_ADMIN="${WARMBLY_ADMIN_URL:-https://admin.$HOSTNAME_ANSWER}"
            URL_WS="${WARMBLY_WS_URL:-wss://ws.$HOSTNAME_ANSWER/socket/websocket}"
            TRACKING_DOMAIN="${WARMBLY_TRACKING_DOMAIN:-track.$HOSTNAME_ANSWER}"
            FORMS_DOMAIN="${WARMBLY_FORMS_DOMAIN:-forms.$HOSTNAME_ANSWER}"
            PHX_HOST=$(printf '%s' "$URL_WS" | sed -e 's|^wss\{0,1\}://||' -e 's|/.*$||')
            CHECK_ORIGIN_HOSTS="$URL_APP,$URL_ADMIN"
            TRUSTED="$PROXY_CIDRS"
            BIND="127.0.0.1:"
            ;;
        *)
            SCHEME=http
            URL_APP="http://$HOSTNAME_ANSWER:$PORT_WEB"
            URL_API="http://$HOSTNAME_ANSWER:$PORT_BACKEND"
            URL_ADMIN="http://$HOSTNAME_ANSWER:$PORT_ADMIN"
            URL_WS="ws://$HOSTNAME_ANSWER:$PORT_REALTIME/socket/websocket"
            TRACKING_DOMAIN="$HOSTNAME_ANSWER:$PORT_TRACKING"
            FORMS_DOMAIN="$HOSTNAME_ANSWER:$PORT_FORMS"
            PHX_HOST="$HOSTNAME_ANSWER"
            # Plain HTTP on a LAN: the origin is whatever host the person typed,
            # which this install cannot know, so the check stays off. The socket
            # still requires a short-lived ticket, which is the actual control.
            CHECK_ORIGIN_HOSTS=""
            # Nothing in front, so nothing may set X-Forwarded-For. Trusting a
            # proxy that is not there is how a rate limit gets bypassed.
            TRUSTED=""
            BIND=""
            ;;
    esac
    CORS="$URL_APP,$URL_ADMIN"

    # Postgres and Redis: bundled unless the operator brought their own.
    if [ -n "$EXTERNAL_DB" ]; then
        DB_URL="$EXTERNAL_DB"; BUNDLED_PG=0
    else
        DB_URL="postgres://warmbly:${PG_PASSWORD}@postgres:5432/warmbly?sslmode=disable"; BUNDLED_PG=1
    fi
    if [ -n "$EXTERNAL_REDIS" ]; then
        REDIS_URL="$EXTERNAL_REDIS"; BUNDLED_REDIS=0
    else
        REDIS_URL="redis://redis:6379"; BUNDLED_REDIS=1
    fi

    # The component set decides which services exist at all. A core install
    # has no tracking and no forms service, so naming a host for either is
    # worse than leaving it unset: campaign mail would ship links, an opt-out
    # address and share URLs pointing at a name nothing answers on.
    if [ "$COMPONENTS" = "core" ]; then
        WANT_TRACKING=0; WANT_REALTIME=0; WANT_FORMS=0
        TRACKING_DOMAIN=""; FORMS_DOMAIN=""
    else
        WANT_TRACKING=1; WANT_REALTIME=1; WANT_FORMS=1
    fi
    [ "$TLS" = "caddy" ] && WANT_CADDY=1 || WANT_CADDY=0
}

# ─────────────────────────────────────────────────────────────────────────
# .env
# ─────────────────────────────────────────────────────────────────────────

render_env() {
    cat <<ENVEOF
# Warmbly. Written by install.sh on $(date -u '+%Y-%m-%d %H:%M:%S UTC').
#
# This file is the whole configuration of this instance and it holds every
# secret it has. Keep it 0600 and back it up with the data.
#
# Re-running the installer edits this file rather than replacing it: your own
# edits survive, and the secrets below are never regenerated.
#
# Reference: $DOCS/development/configuration/

# ── Release ──────────────────────────────────────────────────────────────
# Pinned, not moving. The admin panel's "Update and restart" rewrites this
# line; so does re-running the installer with --version.
WARMBLY_TAG=$RESOLVED_TAG
WARMBLY_IMAGE_PREFIX=$REGISTRY

APP_ENV=prod
DEPLOYMENT_MODE=self_hosted

# ── How it is reached ────────────────────────────────────────────────────
PUBLIC_HOST=$HOSTNAME_ANSWER
APP_URL=$URL_APP
API_PUBLIC_URL=$URL_API
CORS_ALLOW_ORIGINS=$CORS
WEBSOCKET_URL=$URL_WS
PHX_HOST=$PHX_HOST
CHECK_ORIGIN_HOSTS=$CHECK_ORIGIN_HOSTS
# Unset means campaign mail ships with no open pixel and unwrapped links. A
# workspace that verifies its own domain against this one also serves its
# recipients' unsubscribe link there; otherwise it stays on API_PUBLIC_URL.
TRACKING_DOMAIN=$TRACKING_DOMAIN
FORMS_DOMAIN=$FORMS_DOMAIN
# CIDRs allowed to set X-Forwarded-For. Empty trusts nothing, which is correct
# for a directly exposed backend and wrong the moment a proxy is in front.
TRUSTED_PROXIES=$TRUSTED

# ── Where the data sits ──────────────────────────────────────────────────
# A source starting with / is a bind mount; anything else is a Docker named
# volume. That is the whole difference between "I can rsync this" and "ask
# docker volume inspect where it went".
WARMBLY_PG_DATA=$V_PG
WARMBLY_REDIS_DATA=$V_REDIS
WARMBLY_NATS_DATA=$V_NATS
WARMBLY_BLOBS=$V_BLOBS
WARMBLY_WORKER_STATE=$V_WORKER
WARMBLY_UPDATER_STATE=$V_UPDATER

PRIMARY_DB=$DB_URL
DATABASE_SSL=${DATABASE_SSL:-false}
REDIS=$REDIS_URL
$(render_env_pgpassword)
$(render_env_blobs)

# ── Keys ─────────────────────────────────────────────────────────────────
# The first two are UNRECOVERABLE. Every mailbox credential and every
# per-organization key is sealed with them, so a database backup without them
# restores an instance whose mailboxes authenticate against nothing.
CREDENTIALS_ENCRYPTION_KEY=$CREDENTIALS_ENCRYPTION_KEY
KMS_LOCAL_MASTER_KEY=$KMS_LOCAL_MASTER_KEY
AUTH_SECRET=$AUTH_SECRET
INTERNAL_API_TOKEN=$INTERNAL_API_TOKEN
SECRET_KEY_BASE=$SECRET_KEY_BASE

# ── Providers (all local; no cloud account of any kind) ──────────────────
EVENTBUS_PROVIDER=nats
NATS_URL=nats://nats:4222
CODEC_PROVIDER=json
KMS_PROVIDER=local
TASKS_PROVIDER=local
BILLING_PROVIDER=none
CAPTCHA_PROVIDER=none
PUBSUB_ENABLED=false
ENCRYPTED_KEYS_PROVIDER=postgres

# ── What is kept, and for how long ───────────────────────────────────────
# Applied on the first boot of a fresh database and editable from then on in
# the admin panel under Instance > Instance settings, which is authoritative
# once it has been saved. Leaving this line here never undoes an edit made
# there.
WARMBLY_SETTINGS_BOOTSTRAP=$(render_settings_bootstrap)

# ── Who gets in ──────────────────────────────────────────────────────────
# invite_only still lets an invitation link create an account; it stops a
# stranger without one. The very first signup is always allowed, so the claim
# link works on a fresh instance.
DISABLE_REGISTRATION=$REGISTRATION
$(render_env_bootstrap_owner)

# ── Platform mail (login codes, resets, invitations) ─────────────────────
$(render_env_mail)

# ── Whose instance this is ───────────────────────────────────────────────
# Unset, this instance claims nothing: the sign-in screen shows no Terms or
# Privacy link, transactional email carries no company line, and public form
# pages carry no "powered by". Nothing here points at warmbly.com. Fill these
# in to put your own name on those surfaces.
# EMAIL_BRAND_NAME=Acme
# EMAIL_BRAND_WEBSITE_URL=https://acme.example
# EMAIL_BRAND_TERMS_URL=https://acme.example/terms
# EMAIL_BRAND_PRIVACY_URL=https://acme.example/privacy
# EMAIL_BRAND_SUPPORT_EMAIL=support@acme.example
# EMAIL_BRAND_LEGAL_ENTITY=Acme Ltd
# EMAIL_BRAND_COMPANY_NUMBER=
# EMAIL_BRAND_PLACE_OF_REG=
# EMAIL_BRAND_ADDRESS=

# ── Updates ──────────────────────────────────────────────────────────────
# The check is an outbound call to the GitHub releases API and nothing else.
# There is no telemetry in Warmbly; set UPDATE_CHECK_ENABLED=false and this
# instance makes no outbound call of its own at all.
UPDATE_CHECK_ENABLED=$UPDATE_CHECK
UPDATE_CHANNEL=$CHANNEL
RELEASES_GITHUB_REPO=$REPO
UPDATER_URL=http://updater:8095
UPDATER_TOKEN=$INTERNAL_API_TOKEN
ENVEOF
}

render_env_pgpassword() {
    [ "$BUNDLED_PG" = 1 ] || return 0
    printf '%s\n' "POSTGRES_PASSWORD=$PG_PASSWORD"
}

render_env_blobs() {
    if [ "$BLOBS" = "s3" ]; then
        cat <<S3EOF
BLOB_PROVIDER=s3
BLOB_BUCKET=$S3_BUCKET
AWS_ENDPOINT_URL_S3=$S3_ENDPOINT
AWS_REGION=$S3_REGION
AWS_ACCESS_KEY_ID=$S3_KEY
AWS_SECRET_ACCESS_KEY=$S3_SECRET
S3EOF
    else
        cat <<FSEOF
BLOB_PROVIDER=filesystem
# Inside the containers. On this host it is $V_BLOBS.
BLOB_FS_ROOT=/data/blobs
FSEOF
    fi
}

render_settings_bootstrap() {
    printf '{"sync":{"backfill_days":%s,"backfill_messages":%s,"daily_messages_per_mailbox":%s,"daily_messages_per_org":%s},"retention":{"engagement_event_days":%s,"form_event_days":%s,"audit_log_days":%s,"warmup_mail_days":%s,"warmup_event_days":%s}}' \
        "$SYNC_BACKFILL_DAYS" "$SYNC_BACKFILL_MESSAGES" "$SYNC_DAILY_MAILBOX" "$SYNC_DAILY_ORG" \
        "$RET_ENGAGEMENT" "$RET_FORMS" "$RET_AUDIT" "$RET_WARMUP_MAIL" "$RET_WARMUP_EVENTS"
}

render_env_bootstrap_owner() {
    if [ -n "$BOOTSTRAP_EMAIL" ]; then
        printf '%s\n' "WARMBLY_BOOTSTRAP_EMAIL=$BOOTSTRAP_EMAIL"
        printf '%s\n' "WARMBLY_BOOTSTRAP_PASSWORD_HASH=$BOOTSTRAP_HASH"
    else
        printf '%s\n' "# No first owner set: the backend prints a single-use claim link to its log."
        printf '%s\n' "# WARMBLY_BOOTSTRAP_EMAIL=you@example.com"
        printf '%s\n' "# WARMBLY_BOOTSTRAP_PASSWORD_HASH=  # warmblyctl hash-password"
    fi
}

render_env_mail() {
    if [ "$MAIL_MODE" = "smtp" ]; then
        cat <<MAILEOF
MAIL_TRANSPORT=smtp
EMAIL_ADDRESS=$SMTP_FROM
SMTP_HOST=$SMTP_HOST
SMTP_PORT=$SMTP_PORT
SMTP_USERNAME=$SMTP_USER
SMTP_PASSWORD=$SMTP_PASS
SMTP_SECURITY=$SMTP_SECURITY
MAILEOF
    else
        cat <<MAILEOF
# log: password resets, login codes and invitations are PRINTED TO THE
# BACKEND LOG instead of being sent. Fine for a trial, not for a team.
#   docker compose -p warmbly logs backend | grep -i code
# Switch to a relay by setting MAIL_TRANSPORT=smtp and the SMTP_* values.
MAIL_TRANSPORT=log
EMAIL_ADDRESS=noreply@$HOSTNAME_ANSWER
MAILEOF
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# docker-compose.yml
#
# Generated rather than downloaded, because the answers change the SHAPE of
# the file and not only its values: an external database means no postgres
# service at all, "core only" means no tracking, realtime or forms, and a
# bundled Caddy adds one. Every service reads the .env next to it, so there is
# one place to change a setting and it is the file an operator would look in.
# ─────────────────────────────────────────────────────────────────────────

render_compose() {
    cat <<'HDR'
# Warmbly. Generated by install.sh; safe to read, safe to edit.
#
# Every value lives in the .env next to this file, which each service reads in
# full. Re-running the installer regenerates THIS file (keeping a .bak) and
# edits .env in place, so put your own changes in .env or in a
# docker-compose.override.yml, which compose merges on top of this one.
#
#   docker compose -p warmbly ps          what is running
#   docker compose -p warmbly logs -f      follow everything
#   docker compose -p warmbly pull && docker compose -p warmbly up -d   update

name: warmbly

x-app: &app
  restart: unless-stopped
  # Time for a connect or send in flight to finish before an update stops it.
  stop_grace_period: 60s
  env_file: [.env]

services:
HDR

    [ "$BUNDLED_PG" = 1 ] && cat <<PGEOF

  postgres:
    restart: unless-stopped
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: warmbly
      POSTGRES_PASSWORD: \${POSTGRES_PASSWORD}
      POSTGRES_DB: warmbly
    volumes:
      - \${WARMBLY_PG_DATA}:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U warmbly"]
      interval: 5s
      timeout: 5s
      retries: 10
PGEOF

    [ "$BUNDLED_REDIS" = 1 ] && cat <<'REDISEOF'

  redis:
    restart: unless-stopped
    image: redis:7-alpine
    volumes:
      - ${WARMBLY_REDIS_DATA}:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 10
REDISEOF

    cat <<'NATSEOF'

  # The event bus: one ~15MB JetStream binary, no Kafka, no Zookeeper.
  nats:
    restart: unless-stopped
    image: nats:2.10-alpine
    command: ["-js", "-sd", "/data", "-m", "8222"]
    volumes:
      - ${WARMBLY_NATS_DATA}:/data
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8222/healthz"]
      interval: 5s
      timeout: 3s
      retries: 10
NATSEOF

    cat <<BACKENDEOF

  # The control plane. Applies its own migrations on boot.
  backend:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/backend:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_BACKEND}:8080"]
    environment:
      API_HOST: "0.0.0.0:8080"
      GIN_MODE: release
    volumes:
      - \${WARMBLY_BLOBS}:/data/blobs
    depends_on:$(depends_infra)
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://127.0.0.1:8080/health"]
      interval: 10s
      timeout: 3s
      retries: 10
      start_period: 20s

  # Turns bus events into platform state: replies, opens, clicks, bounces.
  consumer:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/consumer:\${WARMBLY_TAG}
    volumes:
      - \${WARMBLY_BLOBS}:/data/blobs
    depends_on:
      backend: { condition: service_healthy }
      nats: { condition: service_healthy }

  # The execution plane: sends and syncs. Scale it out with
  # \`docker compose -p warmbly up -d --scale worker=3\`, or enroll workers on
  # other machines from the admin panel. Outbound IPs belong to the mail
  # provider, so more workers means more parallelism, not more IPs.
  worker:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/worker:\${WARMBLY_TAG}
    environment:
      # Workers hold no database connection; they read encrypted keys through
      # the backend's internal API. Keep it that way.
      ENCRYPTED_KEYS_PROVIDER: http
      ENCRYPTED_KEYS_BACKEND_URL: http://backend:8080
      ENCRYPTED_KEYS_WORKER_TOKEN: \${INTERNAL_API_TOKEN}
      WORKER_STATE_DIR: /data/state
      MAIL_TLS_INSECURE: "false"
    volumes:
      - \${WARMBLY_BLOBS}:/data/blobs
      - \${WARMBLY_WORKER_STATE}:/data/state
    depends_on:
      backend: { condition: service_healthy }
      nats: { condition: service_healthy }

  # The dashboard and the operator panel. Static builds; their API URLs are
  # injected at container start, so these are the images the release publishes.
  web:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/web:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_WEB}:80"]
    environment:
      WARMBLY_API_URL: \${API_PUBLIC_URL}
      WARMBLY_APP_URL: \${APP_URL}
    depends_on:
      backend: { condition: service_healthy }

  admin:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/admin:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_ADMIN}:80"]
    environment:
      WARMBLY_API_URL: \${API_PUBLIC_URL}
      WARMBLY_DASHBOARD_URL: \${APP_URL}
      WARMBLY_ENV_LABEL: production
    depends_on:
      backend: { condition: service_healthy }
BACKENDEOF

    [ "$WANT_TRACKING" = 1 ] && cat <<TRACKEOF

  # Open pixels and click tickets. Without it, campaign mail ships with no
  # pixel and unwrapped links, and open/click reporting stays empty.
  tracking:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/tracking:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_TRACKING}:3000"]
    environment:
      TRACKING_HOST: "0.0.0.0"
      TRACKING_PORT: "3000"
      BACKEND_INTERNAL_URL: http://backend:8080
    depends_on:
      nats: { condition: service_healthy }
TRACKEOF

    [ "$WANT_REALTIME" = 1 ] && cat <<RTEOF

  # Websocket fanout: live inbox, presence, and everything the dashboard
  # updates without a refresh.
  realtime:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/realtime:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_REALTIME}:4000"]
    environment:
      PORT: 4000
      DATABASE_URL: \${PRIMARY_DB}
      REDIS_URL: \${REDIS}
      # Must equal the backend's AUTH_SECRET or no JWT validates here.
      JWT_SECRET: \${AUTH_SECRET}
    depends_on:$(depends_infra)
RTEOF

    [ "$WANT_FORMS" = 1 ] && cat <<FORMSEOF

  # Hosted lead-capture forms on their own origin. No database: the backend's
  # internal API is its only dependency.
  forms:
    <<: *app
    image: \${WARMBLY_IMAGE_PREFIX}/forms:\${WARMBLY_TAG}
    ports: ["${BIND}${PORT_FORMS}:8090"]
    environment:
      GIN_MODE: release
      FORMS_PORT: "8090"
      BACKEND_INTERNAL_URL: http://backend:8080
    depends_on:
      backend: { condition: service_healthy }
FORMSEOF

    cat <<UPDEOF

  # "Update and restart" in the admin panel. Image mode: it pins the release
  # tag in the .env next to this file, pulls, and recreates what changed. It
  # holds the docker socket, which is root on this host, so remove this service
  # if you would rather update by hand with
  # \`docker compose -p warmbly pull && docker compose -p warmbly up -d\`.
  updater:
    restart: unless-stopped
    image: \${WARMBLY_IMAGE_PREFIX}/updater:\${WARMBLY_TAG}
    environment:
      UPDATER_MODE: image
      UPDATER_TOKEN: \${INTERNAL_API_TOKEN}
      UPDATER_REPO_DIR: $DIR
      UPDATER_COMPOSE_PROJECT: warmbly
      UPDATER_COMPOSE_PROFILES: ""
      UPDATER_BACKEND_HEALTH_URL: http://backend:8080/health
    working_dir: $DIR
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - $DIR:$DIR
      - \${WARMBLY_UPDATER_STATE}:/var/lib/warmbly-updater
UPDEOF

    [ "$WANT_CADDY" = 1 ] && cat <<'CADDYEOF'

  # Automatic HTTPS. Caddy answers on 80 and 443 and obtains a certificate for
  # each hostname in the Caddyfile the first time it is asked for, so every
  # name below has to resolve to this machine before it can succeed.
  caddy:
    restart: unless-stopped
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    depends_on:
      backend: { condition: service_healthy }
CADDYEOF

    render_compose_volumes
}

# depends_infra is the dependency block backend and realtime share. An
# external database or cache is not ours to wait for, so it is left out
# rather than named as a service that does not exist.
depends_infra() {
    printf '\n'
    [ "$BUNDLED_PG" = 1 ] && printf '      postgres: { condition: service_healthy }\n'
    [ "$BUNDLED_REDIS" = 1 ] && printf '      redis: { condition: service_healthy }\n'
    printf '      nats: { condition: service_healthy }'
}

# Only the named volumes actually referenced are declared. With a data root on
# a path, every source is a bind mount and this block holds nothing but Caddy's
# certificate store.
render_compose_volumes() {
    _named=""
    if [ "$DATA_ROOT" = "volumes" ]; then
        _named="warmbly_nats warmbly_blobs warmbly_worker warmbly_updater"
        [ "$BUNDLED_PG" = 1 ] && _named="warmbly_postgres $_named"
        [ "$BUNDLED_REDIS" = 1 ] && _named="warmbly_redis $_named"
    fi
    [ "$WANT_CADDY" = 1 ] && _named="$_named caddy_data caddy_config"
    [ -n "$(printf '%s' "$_named" | tr -d ' ')" ] || return 0
    printf '\nvolumes:\n'
    for _v in $_named; do printf '  %s:\n' "$_v"; done
}

render_caddyfile() {
    cat <<CADDYFILE
# Warmbly, behind automatic HTTPS. Written by install.sh.
#
# Every hostname here must resolve to this machine's public IP before Caddy can
# obtain a certificate for it. Check with:  dig +short $H_APP
#
# on_demand_tls covers the hostnames that are not knowable at install time: a
# workspace can point its own tracking or forms domain here later, and Caddy
# asks the backend whether it has verified that name before obtaining a
# certificate for it. Without the ask, Caddy would issue for anything anyone
# aimed at this machine.
{
	email ${CADDY_EMAIL:-admin@$HOSTNAME_ANSWER}
	on_demand_tls {
		ask http://backend:8080/tls/authorize
	}
}

# Applied to every site below. HSTS is set here rather than per service
# because Caddy is the only thing terminating TLS: whatever it proxies to
# speaks plain HTTP on the compose network and cannot know the request
# arrived over TLS. Server header removed so the version is not advertised.
(warmbly_headers) {
	header {
		Strict-Transport-Security "max-age=31536000; includeSubDomains"
		X-Content-Type-Options "nosniff"
		Referrer-Policy "strict-origin-when-cross-origin"
		-Server
	}
}

$H_APP {
	import warmbly_headers
	reverse_proxy web:80
}

$H_ADMIN {
	import warmbly_headers
	reverse_proxy admin:80
}

$H_API {
	import warmbly_headers
	reverse_proxy backend:8080
}
CADDYFILE
    [ "$WANT_REALTIME" = 1 ] && cat <<CADDYFILE

$H_WS {
	import warmbly_headers
	reverse_proxy realtime:4000
}
CADDYFILE
    [ "$WANT_TRACKING" = 1 ] && cat <<CADDYFILE

$H_TRACK {
	import warmbly_headers
	reverse_proxy tracking:3000
}
CADDYFILE
    [ "$WANT_FORMS" = 1 ] && cat <<CADDYFILE

$H_FORMS {
	import warmbly_headers
	reverse_proxy forms:8090
}
CADDYFILE
    render_caddy_custom_domains
    return 0
}

# The catch-all for customer-owned domains. A named site block above always
# wins over it, so this only ever sees a hostname the install was not told
# about, and `tls { on_demand }` is what makes the certificate for that name
# appear. Every request through here has already passed the ask endpoint.
#
# Forms and tracking share the block because they share the door: the two
# services have disjoint paths, so the path decides which one answers rather
# than a hostname list this file cannot know.
render_caddy_custom_domains() {
    [ "$WANT_TRACKING" = 1 ] || [ "$WANT_FORMS" = 1 ] || return 0

    cat <<'CADDYFILE'

# Custom tracking and forms domains a workspace points here (CNAME). Caddy
# obtains the certificate on the first request, after /tls/authorize confirms
# this instance has verified the name.
https:// {
	import warmbly_headers
	tls {
		on_demand
	}
CADDYFILE

    if [ "$WANT_FORMS" = 1 ] && [ "$WANT_TRACKING" = 1 ]; then
        cat <<'CADDYFILE'
	@forms path /f/* /forms.js /api/forms/*
	handle @forms {
		reverse_proxy forms:8090
	}
	handle {
		reverse_proxy tracking:3000
	}
}
CADDYFILE
    elif [ "$WANT_FORMS" = 1 ]; then
        cat <<'CADDYFILE'
	reverse_proxy forms:8090
}
CADDYFILE
    else
        cat <<'CADDYFILE'
	reverse_proxy tracking:3000
}
CADDYFILE
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────
# Scheduled backups
#
# A backup that is only a pg_dump is not a restore: the bodies live in the blob
# root and the ciphertext in the database is opened by keys in .env. The bundle
# `warmblyctl backup` writes holds all three, and this is the timer that runs it.
# ─────────────────────────────────────────────────────────────────────────

render_backup_script() {
    cat <<BACKUPEOF
#!/bin/sh
# Warmbly scheduled backup. Written by install.sh; edit freely.
#
# One bundle per run: the database, the blob root, and (unless you turn it off)
# the two encryption keys without which the rest cannot be read. Restore it on
# another host with:
#
#   docker compose -p warmbly exec backend warmblyctl restore --file <bundle>
set -eu

INSTALL_DIR=$DIR
TARGET=$BACKUP_DIR
KEEP=$BACKUP_KEEP
KEYS=$BACKUP_KEYS
S3_TARGET=${BACKUP_S3:-}

stamp=\$(date -u '+%Y%m%d-%H%M%S')
name="warmbly-\${stamp}.tar.gz"
mkdir -p "\$TARGET"

# warmblyctl writes inside the container; the blob mount is the one path both
# sides can see, so the bundle lands there and is moved out afterwards.
flags=""
[ "\$KEYS" = "true" ] || flags="--no-keys"

cd "\$INSTALL_DIR"
docker compose -p warmbly exec -T backend warmblyctl backup --out "/data/blobs/\$name" \$flags
docker compose -p warmbly cp "backend:/data/blobs/\$name" "\$TARGET/\$name"
docker compose -p warmbly exec -T backend rm -f "/data/blobs/\$name"
chmod 600 "\$TARGET/\$name"

# Off-host, when an aws CLI is available. A backup that only exists on the
# machine it backs up is not one.
if [ -n "\$S3_TARGET" ]; then
  if command -v aws >/dev/null 2>&1; then
    aws s3 cp "\$TARGET/\$name" "\$S3_TARGET/\$name"
  else
    echo "warmbly-backup: aws CLI not found, so \$name was not copied to \$S3_TARGET" >&2
  fi
fi

# Keep the newest \$KEEP and delete the rest.
ls -1t "\$TARGET"/warmbly-*.tar.gz 2>/dev/null | tail -n +\$((KEEP + 1)) | while read -r old; do
  rm -f "\$old"
done

echo "warmbly-backup: wrote \$TARGET/\$name"
BACKUPEOF
}

render_backup_units() {
    cat <<UNITEOF
[Unit]
Description=Warmbly instance backup
After=docker.service

[Service]
Type=oneshot
ExecStart=$DIR/backup.sh
UNITEOF
}

render_backup_timer() {
    case "$BACKUP_SCHEDULE" in
        hourly) _cal="hourly" ;;
        weekly) _cal="weekly" ;;
        *)      _cal="daily" ;;
    esac
    cat <<TIMEREOF
[Unit]
Description=Warmbly instance backup ($_cal)

[Timer]
OnCalendar=$_cal
Persistent=true
RandomizedDelaySec=30m

[Install]
WantedBy=timers.target
TIMEREOF
}

install_backup_timer() {
    [ -n "$BACKUP_DIR" ] || return 0
    write_file "$DIR/backup.sh" "$(render_backup_script)" 0755
    if [ "$DRY_RUN" = 1 ]; then
        note "and a systemd service + timer for it ($BACKUP_SCHEDULE)"
        return 0
    fi
    if ! have systemctl; then
        warn "No systemd here, so the timer was not installed."
        note "backup.sh is written and works; schedule it with cron:"
        note "  0 3 * * *  $DIR/backup.sh"
        return 0
    fi
    if ! need_sudo; then
        warn "Installing the backup timer needs root and sudo is not available."
        note "Install it yourself later: $DIR/backup.sh is ready to schedule."
        return 0
    fi
    printf '%s\n' "$(render_backup_units)" | $SUDO tee /etc/systemd/system/warmbly-backup.service >/dev/null
    printf '%s\n' "$(render_backup_timer)" | $SUDO tee /etc/systemd/system/warmbly-backup.timer >/dev/null
    $SUDO systemctl daemon-reload >/dev/null 2>&1 || true
    $SUDO systemctl enable --now warmbly-backup.timer >/dev/null 2>&1 || true
    ok "Scheduled backup installed ($BACKUP_SCHEDULE, keeping $BACKUP_KEEP, into $BACKUP_DIR)"
}

# ─────────────────────────────────────────────────────────────────────────
# Writing
# ─────────────────────────────────────────────────────────────────────────

# write_file <path> <content> <mode>. Under --dry-run it prints instead, which
# is the whole point of --dry-run: what you see is what would land on disk.
write_file() {
    _path=$1; _body=$2; _mode=${3:-0644}
    if [ "$DRY_RUN" = 1 ]; then
        say ""
        out "  ${DIM}──${R} ${B}${_path}${R} ${DIM}(mode ${_mode})${R}"
        printf '%s\n' "$_body" | sed 's/^/  /'
        return 0
    fi
    _dir=$(dirname "$_path")
    mkdir -p "$_dir"
    # Created with the final mode rather than chmod'd afterwards: .env holds
    # every secret this instance has, and "briefly world-readable" is readable.
    (umask 077; : >"$_path")
    printf '%s\n' "$_body" >"$_path"
    chmod "$_mode" "$_path"
}

backup_existing() {
    _path=$1
    [ -f "$_path" ] || return 0
    [ "$DRY_RUN" = 1 ] && return 0
    cp -p "$_path" "${_path}.bak"
    note "kept the previous $(basename "$_path") as $(basename "$_path").bak"
}

# merge_env keeps an existing .env's own edits: every key the installer owns is
# rewritten, every other line is left exactly where it was. A second run must
# reconfigure, never clobber.
merge_env() {
    _existing=$1; _new=$2
    _keys=$(printf '%s\n' "$_new" | sed -n 's/^\([A-Z_][A-Z0-9_]*\)=.*/\1/p')
    printf '%s\n' "$_new"
    _extra=$(printf '%s\n' "$_existing" | awk -v keys="$_keys" '
        BEGIN { split(keys, k, "\n"); for (i in k) own[k[i]] = 1 }
        /^[A-Z_][A-Z0-9_]*=/ {
            split($0, parts, "=")
            if (parts[1] in own) next
            print
        }')
    if [ -n "$_extra" ]; then
        printf '\n# ── Kept from the previous .env ──────────────────────────────────────────\n'
        printf '%s\n' "$_extra"
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# Existing installs
# ─────────────────────────────────────────────────────────────────────────

env_get() {
    # Last assignment wins, as compose itself resolves it.
    [ -f "$1" ] || return 0
    sed -n "s/^${2}=//p" "$1" | tail -1
}

adopt_existing() {
    _env="$DIR/.env"
    [ -f "$_env" ] || return 0
    EXISTING=1

    # Secrets are adopted, never regenerated: a new AUTH_SECRET signs every
    # session out and a new CREDENTIALS_ENCRYPTION_KEY makes every stored
    # mailbox credential unreadable, permanently.
    AUTH_SECRET=$(env_get "$_env" AUTH_SECRET)
    INTERNAL_API_TOKEN=$(env_get "$_env" INTERNAL_API_TOKEN)
    SECRET_KEY_BASE=$(env_get "$_env" SECRET_KEY_BASE)
    KMS_LOCAL_MASTER_KEY=$(env_get "$_env" KMS_LOCAL_MASTER_KEY)
    CREDENTIALS_ENCRYPTION_KEY=$(env_get "$_env" CREDENTIALS_ENCRYPTION_KEY)
    PG_PASSWORD=$(env_get "$_env" POSTGRES_PASSWORD)

    # And so is the data root: pointing an existing install at a new path is
    # not a move, it is an empty instance next to a full one.
    _pg=$(env_get "$_env" WARMBLY_PG_DATA)
    if [ -n "$_pg" ] && [ -z "${WARMBLY_DATA_ROOT:-}" ] && [ -z "${DATA_ROOT_SET:-}" ]; then
        case "$_pg" in
            /*) DATA_ROOT=$(dirname "$_pg") ;;
            *)  DATA_ROOT=volumes ;;
        esac
    fi

    [ -z "${WARMBLY_HOST:-}" ] && [ -z "${HOST_SET:-}" ] && {
        _h=$(env_get "$_env" PUBLIC_HOST); [ -n "$_h" ] && HOSTNAME_ANSWER=$_h
    }
    [ -z "${WARMBLY_VERSION:-}" ] && [ -z "${VERSION_SET:-}" ] && {
        _t=$(env_get "$_env" WARMBLY_TAG); [ -n "$_t" ] && VERSION=$_t
    }
    return 0
}

ensure_secrets() {
    [ -n "${AUTH_SECRET:-}" ] || AUTH_SECRET=$(gen_hex 32)
    [ -n "${INTERNAL_API_TOKEN:-}" ] || INTERNAL_API_TOKEN=$(gen_hex 32)
    [ -n "${SECRET_KEY_BASE:-}" ] || SECRET_KEY_BASE=$(gen_hex 48)
    [ -n "${KMS_LOCAL_MASTER_KEY:-}" ] || KMS_LOCAL_MASTER_KEY=$(gen_b64 32)
    [ -n "${CREDENTIALS_ENCRYPTION_KEY:-}" ] || CREDENTIALS_ENCRYPTION_KEY=$(gen_hex 32)
    [ -n "${PG_PASSWORD:-}" ] || PG_PASSWORD=$(gen_hex 20)
    # The last `||` above returns non-zero when the key was already set, which
    # set -e would read as this function failing.
    return 0
}

# ─────────────────────────────────────────────────────────────────────────
# The wizard
# ─────────────────────────────────────────────────────────────────────────

wizard() {
    wiz_location
    wiz_access
    wiz_storage
    wiz_keys
    wiz_retention
    wiz_backups
    wiz_entry
    wiz_footprint
}

wiz_location() {
    stepper 1
    note "One directory holds the compose file, the .env, and (unless you move it"
    note "in the next step but one) every store this instance writes to."
    say ""
    ask "Install directory" "$DIR"
    DIR=$ANSWER
    if [ -d "$DIR" ] && [ -n "$(ls -A "$DIR" 2>/dev/null)" ] && [ ! -f "$DIR/$MARKER" ] && [ ! -f "$DIR/.env" ]; then
        say ""
        warn "$DIR is not empty and was not created by this installer."
        if ! confirm "Use it anyway?" no; then
            die "nothing was changed"
        fi
        FORCE=1
    fi
    adopt_existing
    if [ "$EXISTING" = 1 ]; then
        say ""
        ok "Found an existing install here. Its secrets and data root are kept;"
        note "everything else is yours to change, and nothing you have not been"
        note "asked about is touched."
    fi
}

wiz_access() {
    stepper 2
    note "The hostname every link in this instance is built from: the dashboard"
    note "URL, the unsubscribe link in campaign mail, the tracking pixel."
    say ""
    ask "Hostname or IP" "$HOSTNAME_ANSWER"
    HOSTNAME_ANSWER=$ANSWER

    say ""
    out "  ${WHITE}How is it reached?${R}"
    say ""
    _def=1
    case "$TLS" in caddy) _def=2 ;; proxy) _def=3 ;; esac
    choose "$_def" \
        "Plain HTTP on ports|Fine for localhost or a private network. No certificate." \
        "Bundled Caddy with automatic HTTPS|Automatic certificates. Needs DNS here and ports 80 and 443 free." \
        "Behind my own reverse proxy|Binds to 127.0.0.1 and sets TRUSTED_PROXIES, so client IPs survive."
    case "$CHOICE" in 1) TLS=none ;; 2) TLS=caddy ;; 3) TLS=proxy ;; esac

    if [ "$TLS" = "caddy" ]; then
        say ""
        note "Caddy serves one hostname per surface, all under $HOSTNAME_ANSWER:"
        note "  app.$HOSTNAME_ANSWER      admin.$HOSTNAME_ANSWER      api.$HOSTNAME_ANSWER"
        note "  ws.$HOSTNAME_ANSWER       track.$HOSTNAME_ANSWER      forms.$HOSTNAME_ANSWER"
        note "Each needs an A record pointing at this machine before it can get a"
        note "certificate. The exact list is printed again at the end."
        say ""
        ask "Email for the certificate authority (expiry notices)" "admin@$HOSTNAME_ANSWER"
        CADDY_EMAIL=$ANSWER
    elif [ "$TLS" = "proxy" ]; then
        say ""
        note "Your proxy terminates TLS and forwards to 127.0.0.1 on the ports below."
        note "TRUSTED_PROXIES is the one setting that is silently wrong on every"
        note "proxied install: without it every request looks like it came from the"
        note "proxy, so per-IP rate limits and click deduplication stop working."
        say ""
        ask "Public dashboard URL" "https://$HOSTNAME_ANSWER"
        WARMBLY_APP_URL=$ANSWER
        ask "Public API URL" "https://api.$HOSTNAME_ANSWER"
        WARMBLY_API_URL=$ANSWER
        # Asked rather than derived: it is one of the two origins CORS allows,
        # and an admin panel served somewhere unexpected is refused by the API
        # with an error that does not say why.
        ask "Public admin panel URL" "https://admin.$HOSTNAME_ANSWER"
        WARMBLY_ADMIN_URL=$ANSWER
        ask "Trusted proxy CIDRs" "$PROXY_CIDRS"
        PROXY_CIDRS=$ANSWER
    fi
}

wiz_storage() {
    stepper 3
    note "This is the question the rest of the install cannot answer for you."
    note "Self-hosting Warmbly means holding mailbox credentials, message bodies"
    note "and contact records on your own disk. Where they go decides how you"
    note "back this instance up and how you move it."
    say ""
    out "  ${WHITE}Where should the data live?${R}"
    say ""
    _def=1
    [ "$DATA_ROOT" = "volumes" ] && _def=3
    [ -n "$DATA_ROOT" ] && [ "$DATA_ROOT" != "volumes" ] && [ "$DATA_ROOT" != "$DIR/data" ] && _def=2
    choose "$_def" \
        "Under the install directory|$DIR/data: one path holding Postgres, blobs, NATS and worker state." \
        "A path I choose|An external disk, or a mount you already snapshot." \
        "Docker named volumes|Managed by Docker. Moved only with docker volume commands."
    case "$CHOICE" in
        1) DATA_ROOT="$DIR/data" ;;
        2)
            say ""
            ask "Data root" "${DATA_ROOT:-/mnt/data/warmbly}"
            DATA_ROOT=$ANSWER
            case "$DATA_ROOT" in
                /*) ;;
                *) DATA_ROOT="$PWD/$DATA_ROOT" ;;
            esac
            ;;
        3) DATA_ROOT=volumes ;;
    esac

    if confirm "Configure an external Postgres, Redis or object store?" no; then
        say ""
        ask "External Postgres URL (blank keeps the bundled one)" "$EXTERNAL_DB"
        EXTERNAL_DB=$ANSWER
        if [ -n "$EXTERNAL_DB" ]; then
            case "$EXTERNAL_DB" in
                postgres://*|postgresql://*) ok "Looks like a Postgres URL. It is checked for real before anything starts." ;;
                *) warn "That does not start with postgres:// — the backend will refuse it at boot." ;;
            esac
        fi
        ask "External Redis URL (blank keeps the bundled one)" "$EXTERNAL_REDIS"
        EXTERNAL_REDIS=$ANSWER

        say ""
        out "  ${WHITE}Where do message bodies, attachments and avatars go?${R}"
        say ""
        _bdef=1; [ "$BLOBS" = "s3" ] && _bdef=2
        choose "$_bdef" \
            "On this machine's filesystem|Under the data root. Correct while every worker runs here." \
            "S3-compatible object storage|MinIO, R2, B2 or AWS. Required once workers run off-host."
        [ "$CHOICE" = 2 ] && BLOBS=s3 || BLOBS=filesystem
        if [ "$BLOBS" = "s3" ]; then
            say ""
            ask "Bucket" "$S3_BUCKET"; S3_BUCKET=$ANSWER
            ask "Endpoint URL (blank for AWS)" "$S3_ENDPOINT"; S3_ENDPOINT=$ANSWER
            ask "Region" "$S3_REGION"; S3_REGION=$ANSWER
            ask "Access key id" "$S3_KEY"; S3_KEY=$ANSWER
            ask_secret "Secret access key:"; [ -n "$ANSWER" ] && S3_SECRET=$ANSWER
        else
            say ""
            note "Filesystem blobs are local to this machine. A worker running on"
            note "another host writes to ITS OWN disk, so once you scale workers out"
            note "this has to become S3-compatible storage or bodies go missing."
        fi
    fi
}

wiz_keys() {
    stepper 4
    ensure_secrets
    if [ "$EXISTING" = 1 ]; then
        ok "This install already has its keys and they are kept as they are."
        note "Regenerating CREDENTIALS_ENCRYPTION_KEY would make every stored"
        note "mailbox credential permanently unreadable, so the installer never does."
        say ""
        return 0
    fi

    note "Five secrets were generated for this instance. Two of them cannot be"
    note "recovered if they are lost, and a database backup without them is not"
    note "a backup: every mailbox credential in it stays sealed forever."
    say ""
    printf '%s\n' \
"${RED}${B}Copy these somewhere safe now${R}" \
"" \
"${DIM}CREDENTIALS_ENCRYPTION_KEY${R}" \
"${WHITE}$CREDENTIALS_ENCRYPTION_KEY${R}" \
"" \
"${DIM}KMS_LOCAL_MASTER_KEY${R}" \
"${WHITE}$KMS_LOCAL_MASTER_KEY${R}" \
"" \
"${DIM}Also written to keys-backup.txt in the install directory, 0600.${R}" \
"${DIM}That file is on the same disk as the database it protects, so it${R}" \
"${DIM}is not itself a backup.${R}" \
        | box "$RED"
    say ""
    if [ "$INTERACTIVE" = 1 ] && [ "$ASSUME_YES" = 0 ]; then
        while ! confirm_phrase "Type 'copied' when you have them somewhere else:" "copied"; do
            out "    ${DIM}Type the word copied, or Ctrl-C to stop.${R}"
        done
    fi
}

wiz_retention() {
    stepper 5
    note "How much mail is imported when a mailbox is connected, and how long"
    note "event-level history is kept afterwards. Every one of these is also a"
    note "privacy setting, and all of them stay editable in the admin panel."
    say ""
    out "  ${WHITE}Start from which posture?${R}"
    say ""
    choose 1 \
        "Defaults|90 days imported; opens/clicks 365 days, forms 180, audit 90, warmup mail 30." \
        "Minimal retention|30 days imported; every event log 30 days, warmup mail 7. Reports get shorter." \
        "Let me set each one|Nine questions, each with its default already filled in."
    case "$CHOICE" in
        2)
            SYNC_BACKFILL_DAYS=30; SYNC_BACKFILL_MESSAGES=2000
            RET_ENGAGEMENT=30; RET_FORMS=30; RET_AUDIT=30
            RET_WARMUP_MAIL=7; RET_WARMUP_EVENTS=30
            ok "Minimal retention"
            ;;
        3)
            say ""
            out "  ${DIM}Mailbox import, when a mailbox is first connected${R}"
            ask_number "How far back to import (days)" "$SYNC_BACKFILL_DAYS" 1 730; SYNC_BACKFILL_DAYS=$ANSWER
            ask_number "Most messages to import per mailbox" "$SYNC_BACKFILL_MESSAGES" 1 100000; SYNC_BACKFILL_MESSAGES=$ANSWER
            say ""
            out "  ${DIM}Daily sync ceilings. Over them, mail waits; nothing is dropped.${R}"
            ask_number "New messages per mailbox per day" "$SYNC_DAILY_MAILBOX" 1 100000; SYNC_DAILY_MAILBOX=$ANSWER
            ask_number "Messages per organization per day" "$SYNC_DAILY_ORG" 1 2000000; SYNC_DAILY_ORG=$ANSWER
            say ""
            out "  ${DIM}Event history. Each window is how long that data is held.${R}"
            ask_number "Opens and clicks (days)" "$RET_ENGAGEMENT" 1 3650; RET_ENGAGEMENT=$ANSWER
            ask_number "Form funnel events (days)" "$RET_FORMS" 1 3650; RET_FORMS=$ANSWER
            ask_number "Audit log, incl. IP addresses (days)" "$RET_AUDIT" 1 3650; RET_AUDIT=$ANSWER
            say ""
            out "  ${DIM}Warmup. Mail is deleted from each mailbox after the first window, records after the second.${R}"
            ask_number "Warmup mail in mailboxes (days)" "$RET_WARMUP_MAIL" 3 3650; RET_WARMUP_MAIL=$ANSWER
            ask_number "Warmup records (days)" "$RET_WARMUP_EVENTS" 30 3650; RET_WARMUP_EVENTS=$ANSWER
            ;;
        *) ok "Defaults" ;;
    esac
}

wiz_backups() {
    stepper 6
    note "A bundle holding the database, the blob root and the encryption keys."
    note "That combination is the only one that restores; any two of the three"
    note "restore an instance that looks fine and cannot read its own mailboxes."
    say ""
    if ! confirm "Schedule a backup?" yes; then
        BACKUP_DIR=""
        return 0
    fi
    say ""
    ask "Write bundles to" "${BACKUP_DIR:-/var/backups/warmbly}"
    BACKUP_DIR=$ANSWER
    say ""
    out "  ${WHITE}How often?${R}"
    say ""
    choose 2 "Hourly|" "Daily|Runs overnight with a random delay." "Weekly|"
    case "$CHOICE" in 1) BACKUP_SCHEDULE=hourly ;; 3) BACKUP_SCHEDULE=weekly ;; *) BACKUP_SCHEDULE=daily ;; esac
    ask_number "How many to keep" "$BACKUP_KEEP" 1 3650; BACKUP_KEEP=$ANSWER
    say ""
    if confirm "Include the encryption keys in each bundle?" yes; then
        BACKUP_KEYS=true
        note "The bundle is now as sensitive as the instance itself. Anyone holding"
        note "one can read every mailbox credential in it."
    else
        BACKUP_KEYS=false
        note "Keep $DIR/keys-backup.txt somewhere else, or the bundles cannot be restored."
    fi
    say ""
    ask "Also copy each bundle to an S3 URL (needs the aws CLI)" "$BACKUP_S3"
    BACKUP_S3=$ANSWER
}

wiz_entry() {
    stepper 7
    out "  ${WHITE}Who gets the first account?${R}"
    say ""
    choose 1 \
        "Print a claim link|A single-use link is printed; open it and set your own password." \
        "Create it now, unattended|An email plus a hash from warmblyctl hash-password."
    if [ "$CHOICE" = 2 ]; then
        say ""
        ask "Owner email" "$BOOTSTRAP_EMAIL"; BOOTSTRAP_EMAIL=$ANSWER
        ask "Password hash (argon2, from warmblyctl hash-password)" "$BOOTSTRAP_HASH"; BOOTSTRAP_HASH=$ANSWER
        if [ -z "$BOOTSTRAP_HASH" ]; then
            warn "No hash given, so the claim link is used instead."
            BOOTSTRAP_EMAIL=""
        fi
    else
        BOOTSTRAP_EMAIL=""
    fi

    say ""
    out "  ${WHITE}Who else may create an account?${R}"
    say ""
    _rdef=2
    case "$REGISTRATION" in false) _rdef=1 ;; true) _rdef=3 ;; esac
    choose "$_rdef" \
        "Anyone|Open sign-ups. Only sensible behind a private network." \
        "Invitation only|An invitation link creates the account; a stranger without one cannot." \
        "Nobody|Closed. Members cannot invite either; you create every account by hand."
    case "$CHOICE" in 1) REGISTRATION=false ;; 2) REGISTRATION=invite_only ;; 3) REGISTRATION=true ;; esac

    say ""
    out "  ${WHITE}Platform mail: login codes, password resets, invitations.${R}"
    note "This is not campaign sending. Campaign mail goes through the mailboxes"
    note "you connect; this is Warmbly's own transactional mail."
    say ""
    _mdef=1; [ "$MAIL_MODE" = "smtp" ] && _mdef=2
    choose "$_mdef" \
        "Skip it for now|Codes and reset links go to the backend log. Not for a team." \
        "An SMTP relay|Postmark, SES, Resend, or your own server."
    if [ "$CHOICE" = 2 ]; then
        MAIL_MODE=smtp
        say ""
        ask "SMTP host" "$SMTP_HOST"; SMTP_HOST=$ANSWER
        say ""
        out "  ${WHITE}Connection security${R}"
        say ""
        choose 1 "STARTTLS on 587|The usual choice." "Implicit TLS on 465|" "None|A local sink only; credentials are never sent in the clear."
        case "$CHOICE" in
            1) SMTP_SECURITY=starttls; SMTP_PORT=587 ;;
            2) SMTP_SECURITY=tls; SMTP_PORT=465 ;;
            3) SMTP_SECURITY=none; SMTP_PORT=25 ;;
        esac
        ask "Port" "$SMTP_PORT"; SMTP_PORT=$ANSWER
        ask "Username" "$SMTP_USER"; SMTP_USER=$ANSWER
        ask_secret "Password:"; SMTP_PASS=$ANSWER
        ask "From address" "noreply@$HOSTNAME_ANSWER"; SMTP_FROM=$ANSWER
    else
        MAIL_MODE=log
    fi
}

wiz_footprint() {
    stepper 8
    out "  ${WHITE}Which components?${R}"
    say ""
    _cdef=1; [ "$COMPONENTS" = "core" ] && _cdef=2
    choose "$_cdef" \
        "Everything|Adds open and click tracking, live updates, and hosted forms." \
        "Core only|API, dashboard, admin, worker. No tracking, websockets or forms."
    [ "$CHOICE" = 2 ] && COMPONENTS=core || COMPONENTS=full

    say ""
    _ports="$PORT_BACKEND $PORT_WEB $PORT_ADMIN"
    [ "$COMPONENTS" = "full" ] && _ports="$_ports $PORT_TRACKING $PORT_REALTIME $PORT_FORMS"
    [ "$TLS" = "caddy" ] && _ports="80 443 $_ports"
    _busy=$(check_ports "$_ports")
    if [ -n "$_busy" ] && [ "$DEMO" = 1 ]; then
        warn "Something already listens on:$( printf ' %s' "$_busy" )"
        note "A real install would stop here and ask. This one starts nothing, so"
        note "it carries on."
    elif [ -n "$_busy" ]; then
        warn "Something already listens on:$( printf ' %s' "$_busy" )"
        note "Warmbly's containers would fail to start. Free the port, or change the"
        note "published port in $DIR/docker-compose.yml before starting."
        say ""
        confirm "Continue anyway?" no || die "nothing was changed"
    else
        ok "Ports free:$(printf ' %s' "$_ports")"
    fi

    say ""
    note "The update check is one outbound call to the GitHub releases API, so the"
    note "admin panel can tell you a newer version exists. There is no telemetry"
    note "in Warmbly: nothing about this instance is ever sent anywhere."
    say ""
    if confirm "Check for new releases?" yes; then UPDATE_CHECK=true; else UPDATE_CHECK=false; fi
}

# ─────────────────────────────────────────────────────────────────────────
# Review
# ─────────────────────────────────────────────────────────────────────────

summary_rows() {
    printf '%s\n' \
"${DIM}Version${R}        ${WHITE}${RESOLVED_TAG}${R}  ${DIM}from ${REGISTRY}${R}" \
"${DIM}Directory${R}      ${WHITE}${DIR}${R}" \
"${DIM}Reached at${R}     ${WHITE}${URL_APP}${R}  ${DIM}$(tls_label)${R}" \
"${DIM}Data${R}           ${WHITE}${DATA_DESC}${R}" \
"${DIM}Database${R}       ${WHITE}$(db_label)${R}" \
"${DIM}Blobs${R}          ${WHITE}$(blob_label)${R}" \
"${DIM}Import${R}         ${WHITE}${SYNC_BACKFILL_DAYS} days, up to ${SYNC_BACKFILL_MESSAGES} messages per mailbox${R}" \
"${DIM}Kept${R}           ${WHITE}opens/clicks ${RET_ENGAGEMENT}d, forms ${RET_FORMS}d, audit ${RET_AUDIT}d, warmup mail ${RET_WARMUP_MAIL}d, records ${RET_WARMUP_EVENTS}d${R}" \
"${DIM}Backups${R}        ${WHITE}$(backup_label)${R}" \
"${DIM}First owner${R}    ${WHITE}$(owner_label)${R}" \
"${DIM}Sign-ups${R}       ${WHITE}$(registration_label)${R}" \
"${DIM}Platform mail${R}  ${WHITE}$(mail_label)${R}" \
"${DIM}Components${R}     ${WHITE}$(components_label)${R}" \
"${DIM}Update check${R}   ${WHITE}${UPDATE_CHECK}${R}"
}

tls_label() {
    case "$TLS" in
        caddy) printf 'automatic HTTPS via the bundled Caddy' ;;
        proxy) printf 'behind your reverse proxy, bound to 127.0.0.1' ;;
        *)     printf 'plain HTTP' ;;
    esac
}
db_label() { [ "$BUNDLED_PG" = 1 ] && printf 'bundled Postgres 16' || printf 'external'; }
blob_label() {
    if [ "$BLOBS" = "s3" ]; then printf 'S3-compatible (%s)' "${S3_BUCKET:-no bucket set}"
    else printf 'filesystem, %s' "$V_BLOBS"; fi
}
backup_label() {
    [ -n "$BACKUP_DIR" ] || { printf 'none scheduled'; return; }
    printf '%s into %s, keeping %s' "$BACKUP_SCHEDULE" "$BACKUP_DIR" "$BACKUP_KEEP"
}
owner_label() { [ -n "$BOOTSTRAP_EMAIL" ] && printf '%s' "$BOOTSTRAP_EMAIL" || printf 'claim link printed at the end'; }
registration_label() {
    case "$REGISTRATION" in
        false) printf 'anyone' ;;
        true)  printf 'nobody' ;;
        *)     printf 'invitation only' ;;
    esac
}
mail_label() { [ "$MAIL_MODE" = "smtp" ] && printf 'SMTP via %s' "$SMTP_HOST" || printf 'log only (codes printed to the backend log)'; }
components_label() { [ "$COMPONENTS" = "core" ] && printf 'core only' || printf 'everything (tracking, realtime, forms)'; }

review() {
    while :; do
        stepper 9
        summary_rows | box "$SKY"
        say ""
        if [ "$INTERACTIVE" = 0 ] || [ "$ASSUME_YES" = 1 ]; then
            return 0
        fi
        out "  ${DIM}${WHITE}enter${DIM} install   ${WHITE}e${DIM} change a section   ${WHITE}q${DIM} quit${R}"
        # One keypress, like the menus, and only the three keys that mean
        # something act: an unrecognised one redraws rather than installing.
        raw_on
        read_key
        raw_off
        case "$KEY" in
            QUIT|ESC|INT) die "nothing was changed" ;;
            ENTER|SELECT|105|121) return 0 ;;
            101)
                say ""
                out "  ${WHITE}Which section?${R}"
                say ""
                choose 3 \
                    "Where it lives|" "How it is reached|" "Where the data sits|" \
                    "What is kept|" "Backups|" "Who gets in|" "Footprint|"
                case "$CHOICE" in
                    1) wiz_location ;;
                    2) wiz_access ;;
                    3) wiz_storage ;;
                    4) wiz_retention ;;
                    5) wiz_backups ;;
                    6) wiz_entry ;;
                    7) wiz_footprint ;;
                esac
                derive
                ;;
        esac
    done
}

# ─────────────────────────────────────────────────────────────────────────
# Doing it
# ─────────────────────────────────────────────────────────────────────────

prepare_dirs() {
    [ "$DRY_RUN" = 1 ] && return 0
    if ! mkdir -p "$DIR" 2>/dev/null; then
        need_sudo || fail_with "Cannot create $DIR and sudo is not available." \
                               "Pick a directory you can write to:  --dir ~/warmbly"
        info "Creating $DIR (this step needs sudo)"
        $SUDO mkdir -p "$DIR"
        $SUDO chown "$(id -u):$(id -g)" "$DIR"
    fi
    [ -w "$DIR" ] || {
        need_sudo || fail_with "$DIR is not writable and sudo is not available."
        $SUDO chown "$(id -u):$(id -g)" "$DIR"
    }
    if [ "$DATA_ROOT" != "volumes" ]; then
        for _d in "$V_PG" "$V_REDIS" "$V_NATS" "$V_BLOBS" "$V_WORKER" "$V_UPDATER"; do
            mkdir -p "$_d" 2>/dev/null || {
                need_sudo || fail_with "Cannot create the data root $DATA_ROOT and sudo is not available."
                $SUDO mkdir -p "$_d"
                $SUDO chown "$(id -u):$(id -g)" "$_d"
            }
        done
        # The backend and worker run as uid 1000 inside their containers, so a
        # blob root owned by anyone else means the first send fails on mkdir.
        chown -R 1000:1000 "$V_BLOBS" "$V_WORKER" 2>/dev/null ||
            { [ -n "$SUDO" ] && $SUDO chown -R 1000:1000 "$V_BLOBS" "$V_WORKER" 2>/dev/null; } || true
    fi
}

write_all() {
    _env_body=$(render_env)
    if [ -f "$DIR/.env" ] && [ "$DRY_RUN" = 0 ]; then
        backup_existing "$DIR/.env"
        _env_body=$(merge_env "$(cat "$DIR/.env")" "$_env_body")
    fi
    write_file "$DIR/.env" "$_env_body" 0600

    backup_existing "$DIR/docker-compose.yml"
    write_file "$DIR/docker-compose.yml" "$(render_compose)" 0644

    [ "$WANT_CADDY" = 1 ] && write_file "$DIR/Caddyfile" "$(render_caddyfile)" 0644

    if [ "$EXISTING" = 0 ]; then
        write_file "$DIR/keys-backup.txt" "$(render_keys_backup)" 0600
    fi
    write_file "$DIR/$MARKER" "$(printf 'installed_at=%s\nversion=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$RESOLVED_TAG")" 0644
    install_backup_timer
}

render_keys_backup() {
    cat <<KEYSEOF
# Warmbly instance keys, $(date -u '+%Y-%m-%d %H:%M:%S UTC').
#
# The first two cannot be recovered. Every mailbox credential and every
# per-organization key on this instance is sealed with them, so a database
# backup that does not travel with them restores an instance whose mailboxes
# authenticate against nothing.
#
# This file sits on the same disk as the database it protects, which means it
# is not a backup. Copy it somewhere else.

CREDENTIALS_ENCRYPTION_KEY=$CREDENTIALS_ENCRYPTION_KEY
KMS_LOCAL_MASTER_KEY=$KMS_LOCAL_MASTER_KEY

# These three are replaceable, at the cost of signing everyone out and
# reconfiguring the workers.
AUTH_SECRET=$AUTH_SECRET
INTERNAL_API_TOKEN=$INTERNAL_API_TOKEN
SECRET_KEY_BASE=$SECRET_KEY_BASE
KEYSEOF
}

# A registry that refuses to serve an image answers "unauthorized", which the
# generic message below would report as a missing tag and send the operator
# looking in entirely the wrong place. Every Warmbly image is meant to be
# pullable with no account at all, so this is a bug in the release rather than
# anything wrong with their machine (#371).
pull_failed() {
    show_log
    if [ -s "$LOGFILE" ] &&
       grep -Eqi 'unauthorized|denied|authentication required' "$LOGFILE"; then
        _host=${REGISTRY%%/*}
        fail_with "The registry refused to serve the release images." \
                  "It answered unauthorized rather than 'no such tag', so" \
                  "${RESOLVED_TAG} is probably fine and the images are simply" \
                  "not readable without an account." \
                  "" \
                  "Every Warmbly image is meant to be public. If you did not" \
                  "pass --registry, that makes this our bug, not yours:" \
                  "please tell us at https://github.com/$REPO/issues" \
                  "" \
                  "If you did pass it, check it for a typo. A namespace that" \
                  "does not exist is refused in exactly the same way as a" \
                  "private one." \
                  "" \
                  "To get past it now: docker login $_host as someone who can" \
                  "read these images, or point --registry at a mirror you can."
    fi
    fail_with "Could not pull the release images." \
              "The tag ${RESOLVED_TAG} may not exist, or this host cannot" \
              "reach ${REGISTRY}." \
              "Releases: https://github.com/$REPO/releases"
}

# pull_images shows one row per image, flipping to a tick the moment that image
# lands. The pull itself runs in the background, so this is compose's own
# parallel pull with a readable face on it rather than a serial one.
pull_images() {
    _refs=$(cd "$DIR" && composec config --images 2>/dev/null | sort -u)
    if [ -z "$_refs" ]; then
        spin "Pulling images" sh -c "cd '$DIR' && $COMPOSE pull" || pull_failed
        return 0
    fi
    _total=$(printf '%s\n' "$_refs" | wc -l | tr -d ' ')

    ( cd "$DIR" && composec pull ) >"$LOGFILE" 2>&1 &
    _pid=$!
    _start=$(now_s)

    if [ "$USE_COLOR" = 0 ]; then
        say "  ... pulling $_total images"
        wait "$_pid" 2>/dev/null && _rc=0 || _rc=$?
        [ "$_rc" = 0 ] && say "  ok  pulled $_total images"
        [ "$_rc" = 0 ] || pull_failed
        return 0
    fi

    printf '%b' "$HIDE"
    _first=1; _f=1; _tick=0
    # The set of images already on disk is asked of the daemon about once a
    # second and then matched locally. Inspecting each image on every animation
    # frame would be a dozen daemon round trips ten times a second, which
    # competes with the pull it is drawing.
    _present=""
    while :; do
        kill -0 "$_pid" 2>/dev/null && _running=1 || _running=0
        if [ "$_tick" = 0 ]; then
            _present=$(dockerc image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null || true)
        fi
        _tick=$((_tick + 1)); [ "$_tick" -ge 8 ] && _tick=0

        [ "$_first" = 1 ] || printf '%b' "${ESC}[$((_total + 1))A"
        _first=0
        _have=0
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        for _ref in $_refs; do
            printf '%b' "${ESC}[2K"
            if printf '%s\n' "$_present" | grep -qxF "$_ref"; then
                _have=$((_have + 1))
                out "  ${GREEN}✓${R} ${DIM}$(short_ref "$_ref")${R}"
            else
                out "  ${SKY}${_frame}${R} ${GREY}$(short_ref "$_ref")${R}"
            fi
        done
        printf '%b' "${ESC}[2K"
        out "  $(bar "$_have" "$_total" 24)  ${WHITE}${_have}/${_total}${R} ${DIM}$(elapsed_since "$_start")${R}"
        [ "$_running" = 0 ] && break
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.12
    done
    printf '%b' "$SHOW"
    wait "$_pid" 2>/dev/null && _rc=0 || _rc=$?
    if [ "$_rc" != 0 ]; then
        pull_failed
    fi
}

# short_ref trims the registry namespace so the rows read as service names.
short_ref() {
    _r=$1
    case "$_r" in
        "$REGISTRY"/*) printf '%s' "${_r#"$REGISTRY"/}" ;;
        *) printf '%s' "$_r" ;;
    esac
}

# verify_digests checks what was actually pulled against the digests the
# release published, so "curl | sh" is checkable after the fact and not only
# before it. Never a warning that is really a failure: a mismatch stops the
# install, and a manifest that cannot be read says so and continues, because an
# older release, an air-gapped mirror and a private registry all legitimately
# have none.
verify_digests() {
    [ "$REGISTRY" = "$DEFAULT_REGISTRY" ] || {
        note "Images came from $REGISTRY, so the release manifest does not describe them; skipped."
        return 0
    }
    _man="$TMPDIR_RUN/images.json"
    if ! curl -fsSL "https://github.com/$REPO/releases/download/$RESOLVED_TAG/images.json" -o "$_man" 2>/dev/null; then
        note "No image manifest published for $RESOLVED_TAG, so the pulled digests were not checked."
        return 0
    fi

    _checked=0; _bad=""
    for _svc in backend consumer worker forms updater web admin tracking realtime; do
        _want=$(sed -n "s/.*\"${_svc}\": *\"\([^\"]*\)\".*/\1/p" "$_man" | head -1)
        [ -n "$_want" ] || continue
        _ref="$REGISTRY/$_svc:$RESOLVED_TAG"
        _got=$(dockerc image inspect "$_ref" --format '{{range .RepoDigests}}{{println .}}{{end}}' 2>/dev/null |
            sed -n "s|^${REGISTRY}/${_svc}@||p" | head -1)
        # Not pulled at all is not a mismatch: a core-only install has no
        # tracking, realtime or forms image and the manifest still lists them.
        [ -n "$_got" ] || continue
        _checked=$((_checked + 1))
        [ "$_got" = "$_want" ] || _bad="$_bad $_svc"
    done

    if [ -n "$_bad" ]; then
        fail_with "The images on this machine are not the ones release $RESOLVED_TAG published." \
                  "Mismatched:$_bad" \
                  "Nothing was started. Remove those images and run this again; if it happens twice," \
                  "do not run this instance and open an issue at https://github.com/$REPO/issues."
    fi
    if [ "$_checked" -gt 0 ]; then
        ok "Verified $_checked images against the $RESOLVED_TAG manifest"
    fi
}

start_stack() {
    ( cd "$DIR" && composec up -d --remove-orphans ) >"$LOGFILE" 2>&1 &
    _pid=$!
    if [ "$USE_COLOR" = 0 ]; then
        wait "$_pid" 2>/dev/null && _rc=0 || _rc=$?
    else
        printf '%b' "$HIDE"; _f=1
        while kill -0 "$_pid" 2>/dev/null; do
            _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
            printf '\r  %b%s%b Starting the stack' "$SKY" "$_frame" "$R"
            _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
            nap 0.08
        done
        printf '\r%b' "${ESC}[2K"; printf '%b' "$SHOW"
        wait "$_pid" 2>/dev/null && _rc=0 || _rc=$?
    fi
    if [ "$_rc" != 0 ]; then
        show_log
        fail_with "The stack did not start." "Read the whole log with:  cd $DIR && docker compose -p warmbly logs"
    fi
    ok "Containers created"
}

# wait_healthy watches the API come up. The first boot applies every migration,
# so this is the slow step and it says what it is waiting for rather than
# spinning silently.
wait_healthy() {
    _url="http://127.0.0.1:${PORT_BACKEND}/health"
    _start=$(now_s)
    _deadline=$(( _start + 300 ))
    _f=1; _tick=0
    [ "$USE_COLOR" = 1 ] && printf '%b' "$HIDE"
    while :; do
        # Probed about once a second; the frames in between are just animation.
        if [ "$_tick" = 0 ] && curl -fsS --max-time 2 "$_url" >/dev/null 2>&1; then
            clear_line
            ok "The API is answering"
            return 0
        fi
        if [ "$(now_s)" -gt "$_deadline" ]; then
            clear_line
            warn "The API has not answered in five minutes."
            note "It is probably still applying migrations, or one service is failing."
            note "Watch it:  cd $DIR && docker compose -p warmbly logs -f backend"
            return 1
        fi
        if [ "$USE_COLOR" = 1 ]; then
            _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
            printf '\r%b  %b%s%b %s %b(%s)%b' "${ESC}[2K" \
                "$SKY" "$_frame" "$R" "$(health_label)" "$DIM" "$(elapsed_since "$_start")" "$R"
            _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
            _tick=$((_tick + 1)); [ "$_tick" -ge 8 ] && _tick=0
            nap 0.12
        else
            sleep 2 2>/dev/null || true
        fi
    done
}

# health_label is the wait's status text, long enough to explain itself on a
# normal terminal and short enough never to wrap on a narrow one. Every
# character of a redrawn line has to fit or the \r only rewinds the last
# physical row and the rest is left on screen.
health_label() {
    if [ "$COLS" -ge 76 ]; then
        printf 'Waiting for the API, migrations run on this first boot'
    else
        printf 'Waiting for the API'
    fi
}

# clear_line wipes the one-line status a spinner leaves behind and puts the
# cursor back.
clear_line() {
    [ "$USE_COLOR" = 1 ] || return 0
    printf '\r%b%b' "${ESC}[2K" "$SHOW"
}

show_log() {
    [ -s "$LOGFILE" ] || return 0
    say ""
    out "  ${DIM}last lines of the log:${R}"
    tail -20 "$LOGFILE" | sed 's/^/    /'
    say ""
}

# ─────────────────────────────────────────────────────────────────────────
# The end of a successful run
# ─────────────────────────────────────────────────────────────────────────

claim_link() {
    if [ "$DEMO" = 1 ]; then
        printf '%s/setup/demo-link-not-a-real-token' "$URL_APP"
        return 0
    fi
    # setup-link mints a fresh single-use link, and only on an instance with no
    # accounts. On one that already has them it fails, which is the correct
    # answer: there is nothing left to claim.
    ( cd "$DIR" && composec exec -T backend warmblyctl setup-link 2>/dev/null ) |
        sed -n 's|.*\(https\{0,1\}://[^ ]*\)|\1|p' | head -1
}

finale() {
    say ""
    rule
    say ""
    out "  ${GREEN}${B}Warmbly ${RESOLVED_TAG} is running.${R}"
    say ""

    _link=$(claim_link || true)
    if [ -n "$_link" ]; then
        printf '%s\n' \
"${B}${WHITE}Claim this instance${R}" \
"${DIM}Single use, and it expires. Until an account exists it is the${R}" \
"${DIM}only way in.${R}" \
"" \
"${SKY}${_link}${R}" \
            | box "$GREEN"
    elif [ -n "$BOOTSTRAP_EMAIL" ]; then
        printf '%s\n' \
"${B}${WHITE}Sign in${R}" \
"${DIM}The first owner was created from the environment.${R}" \
"" \
"${SKY}${URL_APP}${R}  ${DIM}as ${BOOTSTRAP_EMAIL}${R}" \
            | box "$GREEN"
    else
        printf '%s\n' \
"${B}${WHITE}Sign in${R}" \
"${SKY}${URL_APP}${R}" \
"" \
"${DIM}This instance already has accounts, so no claim link was issued.${R}" \
            | box "$GREEN"
    fi

    say ""
    out "  ${B}Where things are${R}"
    out "    ${DIM}Dashboard${R}   $URL_APP"
    out "    ${DIM}Admin${R}       $URL_ADMIN"
    out "    ${DIM}API${R}         $URL_API"
    [ "$WANT_FORMS" = 1 ] && out "    ${DIM}Forms${R}       ${SCHEME}://$FORMS_DOMAIN"
    out "    ${DIM}Install${R}     $DIR"
    out "    ${DIM}Data${R}        $DATA_DESC"

    if [ "$TLS" = "caddy" ]; then
        say ""
        out "  ${B}DNS${R} ${DIM}each of these must resolve to this machine${R}"
        for _h in "$H_APP" "$H_ADMIN" "$H_API" "$H_WS" "$H_TRACK" "$H_FORMS"; do
            out "    ${DIM}A${R}  $_h"
        done
        note "Caddy retries until they do; until then the names return an error."
    fi

    if [ "$MAIL_MODE" = "log" ]; then
        say ""
        out "  ${AMBER}!${R} ${B}Platform mail goes to the log${R}"
        note "Password resets, login codes and invitations are printed, not sent."
        note "From $DIR:"
        note "  docker compose -p warmbly logs backend | grep -i code"
        note "Set MAIL_TRANSPORT=smtp in .env before anyone else uses this instance."
    fi

    say ""
    out "  ${B}Next${R}  ${DIM}from ${DIR}, where compose reads the .env${R}"
    out "    ${DIM}Health${R}    docker compose -p warmbly exec backend warmblyctl status"
    out "    ${DIM}Logs${R}      docker compose -p warmbly logs -f"
    out "    ${DIM}Back up${R}   docker compose -p warmbly exec backend warmblyctl backup"
    out "    ${DIM}Update${R}    the version pill in the admin panel, or compose pull + up -d"
    out "    ${DIM}Guide${R}     $DOCS/development/first-run/"
    out "    ${DIM}Mailboxes${R} $DOCS/guides/mailboxes/"
    say ""
    note "A mail server on this machine, such as Proton Bridge on 127.0.0.1,"
    note "connects with Security: None. That mode is offered only here, where"
    note "the worker and the relay share a host and no password reaches a wire."
    say ""
    if [ "$EXISTING" = 0 ]; then
        out "  ${RED}Keep a copy of $DIR/keys-backup.txt somewhere other than this machine.${R}"
        out "  ${DIM}Without those two keys a database backup cannot open a single mailbox.${R}"
        say ""
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# Demo
#
# --demo walks the whole install with every side effect removed: the wizard and
# the review are the real ones, and the pull, the start and the health wait are
# played rather than run. It exists because "what does it look like" is a fair
# question to ask before pointing a script at your server, and answering it
# should not require a server.
# ─────────────────────────────────────────────────────────────────────────

# The played phases are paced to be watchable, not to match a real install:
# a real pull is a minute or two and a first boot with migrations another
# half, which nobody wants to sit through to see what a screen looks like.
# One beat is roughly a second. WARMBLY_DEMO_FAST=1 collapses them while
# iterating on the UI itself.
DEMO_BEAT=14
[ -n "${WARMBLY_DEMO_FAST:-}" ] && DEMO_BEAT=3

demo_banner() {
    printf '%s\n' \
"${B}${WHITE}Demo. Nothing is installed and nothing is written.${R}" \
"" \
"${DIM}The questions, the keys and the review below are the real ones. The${R}" \
"${DIM}pull and the start at the end are played, not run: no Docker is${R}" \
"${DIM}touched, no image is pulled, and no file is created anywhere.${R}" \
"" \
"${DIM}The real thing:  curl -fsSL https://warmbly.com/install.sh | sh${R}" \
        | box "$AMBER"
    say ""
}

# demo_refs is the image list the chosen component set would pull.
demo_refs() {
    for _s in backend consumer worker web admin updater; do
        printf '%s/%s:%s\n' "$REGISTRY" "$_s" "$RESOLVED_TAG"
    done
    if [ "$COMPONENTS" = "full" ]; then
        for _s in tracking realtime forms; do
            printf '%s/%s:%s\n' "$REGISTRY" "$_s" "$RESOLVED_TAG"
        done
    fi
    [ "$BUNDLED_PG" = 1 ] && printf 'postgres:16-alpine\n'
    [ "$BUNDLED_REDIS" = 1 ] && printf 'redis:7-alpine\n'
    printf 'nats:2.10-alpine\n'
    [ "$WANT_CADDY" = 1 ] && printf 'caddy:2-alpine\n'
    return 0
}

# demo_pull is pull_images with the pull replaced by a clock. The rows fill in
# over a fixed number of frames rather than by asking the daemon anything.
demo_pull() {
    _refs=$(demo_refs)
    _total=$(printf '%s\n' "$_refs" | wc -l | tr -d ' ')
    _start=$(now_s)
    if [ "$USE_COLOR" = 0 ]; then
        say "  ... pulling $_total images"
        say "  ok  pulled $_total images"
        return 0
    fi
    # Paced off the beat and the image count together, so the bar moves at a
    # readable rate whether the answers selected six services or thirteen.
    _step=$(((DEMO_BEAT * 6 / _total) + 2))
    _frames=$((_total * _step))
    _n=0; _f=1; _first=1
    printf '%b' "$HIDE"
    while [ "$_n" -le "$_frames" ]; do
        _have=$((_n / _step))
        [ "$_have" -gt "$_total" ] && _have=$_total
        [ "$_first" = 1 ] || printf '%b' "${ESC}[$((_total + 1))A"
        _first=0
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        _i=0
        for _ref in $_refs; do
            _i=$((_i + 1))
            printf '%b' "${ESC}[2K"
            if [ "$_i" -le "$_have" ]; then
                out "  ${GREEN}✓${R} ${DIM}$(short_ref "$_ref")${R}"
            else
                out "  ${SKY}${_frame}${R} ${GREY}$(short_ref "$_ref")${R}"
            fi
        done
        printf '%b' "${ESC}[2K"
        out "  $(bar "$_have" "$_total" 24)  ${WHITE}${_have}/${_total}${R} ${DIM}$(elapsed_since "$_start")${R}"
        _n=$((_n + 1))
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.07
    done
    printf '%b' "$SHOW"
}

# demo_services is the container list the chosen answers would create, in the
# order compose brings them up.
demo_services() {
    [ "$BUNDLED_PG" = 1 ] && printf 'postgres\n'
    [ "$BUNDLED_REDIS" = 1 ] && printf 'redis\n'
    printf 'nats\nbackend\nconsumer\nworker\nweb\nadmin\n'
    if [ "$COMPONENTS" = "full" ]; then
        printf 'tracking\nrealtime\nforms\n'
    fi
    printf 'updater\n'
    [ "$WANT_CADDY" = 1 ] && printf 'caddy\n'
    return 0
}

# demo_start plays the container creation the way compose reports it: one row
# per service, each moving from pending to creating to started. This is the
# step that is over in a blink when it is only a spinner, and it is the one
# where an operator most wants to see what the answers actually built.
demo_start() {
    _svcs=$(demo_services)
    _total=$(printf '%s\n' "$_svcs" | wc -l | tr -d ' ')
    if [ "$USE_COLOR" = 0 ]; then
        for _s in $_svcs; do say "  ok  Container warmbly-${_s}-1  Started"; done
        ok "Containers created"
        return 0
    fi
    _step=$(((DEMO_BEAT * 5 / _total) + 2))
    _frames=$((_total * _step + _step))
    _n=0; _f=1; _first=1
    printf '%b' "$HIDE"
    while [ "$_n" -le "$_frames" ]; do
        _done=$((_n / _step))
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        [ "$_first" = 1 ] || printf '%b' "${ESC}[${_total}A"
        _first=0
        _i=0
        for _s in $_svcs; do
            _i=$((_i + 1))
            printf '%b' "${ESC}[2K"
            if [ "$_i" -le "$_done" ]; then
                out "  ${GREEN}✓${R} ${DIM}Container warmbly-${_s}-1${R}  ${DIM}Started${R}"
            elif [ "$_i" = $((_done + 1)) ]; then
                out "  ${SKY}${_frame}${R} ${WHITE}Container warmbly-${_s}-1${R}  ${DIM}Creating${R}"
            else
                out "    ${GREY}Container warmbly-${_s}-1${R}"
            fi
        done
        _n=$((_n + 1))
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.07
    done
    printf '%b' "$SHOW"
    ok "Containers created"
}

# demo_wait plays the wait_healthy screen, with a real clock. A first boot
# applies every migration and genuinely takes this long or longer, so this is
# the one phase where the demo being slow is the demo being honest.
demo_wait() {
    if [ "$USE_COLOR" = 0 ]; then
        ok "The API is answering"
        return 0
    fi
    _start=$(now_s)
    _frames=$((DEMO_BEAT * 10))
    _n=0; _f=1
    printf '%b' "$HIDE"
    while [ "$_n" -lt "$_frames" ]; do
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        printf '\r%b  %b%s%b %s %b(%s)%b' "${ESC}[2K" \
            "$SKY" "$_frame" "$R" "$(health_label)" "$DIM" "$(elapsed_since "$_start")" "$R"
        _n=$((_n + 1))
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.07
    done
    clear_line
    ok "The API is answering"
}

# demo_spin plays one labelled spinner for a fixed number of frames.
demo_spin() {
    _label=$1; _frames=$2
    if [ "$USE_COLOR" = 0 ]; then
        ok "$_label"
        return 0
    fi
    _n=0; _f=1
    printf '%b' "$HIDE"
    while [ "$_n" -lt "$_frames" ]; do
        _frame=$(printf '%s' "$SPIN_FRAMES" | cut -c"$_f")
        printf '\r  %b%s%b %s' "$SKY" "$_frame" "$R" "$_label"
        _n=$((_n + 1))
        _f=$((_f + 1)); [ "$_f" -gt 10 ] && _f=1
        nap 0.07
    done
    clear_line
    ok "$_label"
}

demo_install() {
    step "Installing Warmbly $RESOLVED_TAG into $DIR"
    say ""
    demo_spin "Wrote .env and docker-compose.yml" "$DEMO_BEAT"
    say ""
    demo_pull
    say ""
    demo_start
    demo_wait
    finale
    printf '%s\n' \
"${B}${WHITE}That was the demo.${R}" \
"" \
"${DIM}No file was written, no container was created, and the keys printed${R}" \
"${DIM}above were thrown away. To do it for real:${R}" \
"" \
"${SKY}curl -fsSL https://warmbly.com/install.sh | sh -s -- --wizard${R}" \
        | box "$AMBER"
    say ""
}

# ─────────────────────────────────────────────────────────────────────────
# Uninstall
# ─────────────────────────────────────────────────────────────────────────

uninstall() {
    banner
    [ -f "$DIR/docker-compose.yml" ] || fail_with "No Warmbly install at $DIR." "Point at it with --dir."
    step "Removing the Warmbly stack in $DIR"
    say ""
    if [ "$PURGE_DATA" = 0 ]; then
        info "Containers and networks only. Your data stays where it is."
        note "Data root: $(env_get "$DIR/.env" WARMBLY_PG_DATA | xargs dirname 2>/dev/null || echo 'see .env')"
        note "Add --purge-data to delete it too."
        say ""
        confirm "Stop and remove the containers?" yes || die "nothing was changed"
        ( cd "$DIR" && composec down --remove-orphans ) || true
        ok "Containers removed. $DIR and its data are untouched."
        return 0
    fi

    _pg=$(env_get "$DIR/.env" WARMBLY_PG_DATA)
    say ""
    printf '%s\n' \
"${RED}${B}This deletes every organization, mailbox, contact and message${R}" \
"${DIM}on this instance, and the keys that could have opened a backup of it.${R}" \
"" \
"${DIM}Removing:${R} ${WHITE}${DIR}${R}" \
"${DIM}         ${R} ${WHITE}${_pg:-the configured stores}${R} ${DIM}and the rest of the data root${R}" \
"" \
"${DIM}There is no undo, and no copy is made first.${R}" \
        | box "$RED"
    say ""
    if [ "$ASSUME_YES" = 0 ]; then
        confirm_phrase "Type 'delete everything' to confirm:" "delete everything" ||
            die "nothing was changed"
    fi
    ( cd "$DIR" && composec down -v --remove-orphans ) || true
    if have systemctl && [ -f /etc/systemd/system/warmbly-backup.timer ]; then
        need_sudo && {
            $SUDO systemctl disable --now warmbly-backup.timer >/dev/null 2>&1 || true
            $SUDO rm -f /etc/systemd/system/warmbly-backup.timer /etc/systemd/system/warmbly-backup.service
            $SUDO systemctl daemon-reload >/dev/null 2>&1 || true
        }
    fi
    case "$_pg" in
        /*)
            _root=$(dirname "$_pg")
            rm -rf "$_root" 2>/dev/null || { need_sudo && $SUDO rm -rf "$_root"; } || true
            ;;
    esac
    rm -rf "$DIR" 2>/dev/null || { need_sudo && $SUDO rm -rf "$DIR"; } || true
    say ""
    ok "Warmbly is gone."
    say ""
}

# ─────────────────────────────────────────────────────────────────────────
# Arguments
# ─────────────────────────────────────────────────────────────────────────

usage() {
    cat <<USAGE
$SCRIPT_NAME

  curl -fsSL https://warmbly.com/install.sh | sh
  curl -fsSL https://warmbly.com/install.sh | sh -s -- --wizard

Modes
  --wizard              Ask the full question set: where each store lives, what
                        is kept and for how long, how it is backed up. Every
                        answer defaults to the fast path, so pressing enter
                        through it installs exactly what the fast path does.
  -y, --yes             Never ask. Every unanswered question takes its default.
  --dry-run             Print the .env and compose file it would write, and stop.
  --demo                Walk the whole thing with nothing installed: the wizard,
                        the review, and a simulated pull and start. Writes no
                        file, pulls no image, needs no Docker. For seeing what
                        the install looks like before you run one.
  --print-env           Print the .env and stop.
  --uninstall           Stop and remove the containers. Refuses to touch data.
  --purge-data          With --uninstall: delete the data root as well.

Answers                                        environment variable
  --dir PATH            Install directory      WARMBLY_DIR          [$DEFAULT_DIR]
  --host HOST           Public hostname        WARMBLY_HOST         [localhost]
  --tls MODE            none|caddy|proxy       WARMBLY_TLS          [none]
  --data-root PATH      Or the word 'volumes'  WARMBLY_DATA_ROOT    [<dir>/data]
  --blobs MODE          filesystem|s3          WARMBLY_BLOBS        [filesystem]
  --database-url URL    Use an external Postgres      WARMBLY_DATABASE_URL
  --redis-url URL       Use an external Redis         WARMBLY_REDIS_URL
  --components SET      core|full              WARMBLY_COMPONENTS   [full]
  --version TAG         Release to install     WARMBLY_VERSION      [newest]
  --channel NAME        stable|dev             WARMBLY_CHANNEL      [stable]
  --registry PREFIX     Image namespace        WARMBLY_IMAGE_PREFIX [$DEFAULT_REGISTRY]
  --backup-dir PATH     Schedule backups into it     WARMBLY_BACKUP_DIR
  --backup-keep N       How many to keep             WARMBLY_BACKUP_KEEP  [14]
  --retention-preset P  default|minimal              (sets the five windows)
  --no-update-check     No outbound call at all      WARMBLY_UPDATE_CHECK

Other
  --force               Install into a directory this installer did not create
  --no-color            Plain output
  --clear               Redraw the screen at each wizard step instead of
                        appending. Nothing above the installer is erased either
                        way, but appending leaves the whole run scrollable.
  --no-clear            The default; accepted so older invocations keep working
  -h, --help            This

Everything it writes lives under the install directory. It asks for sudo only
to create that directory when it is not yours, to install Docker if you say
yes, and to install the backup timer.

Docs: $DOCS/development/install/
USAGE
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --wizard) WIZARD=1 ;;
            --demo) DEMO=1; WIZARD=1 ;;
            -y|--yes) ASSUME_YES=1 ;;
            --dry-run) DRY_RUN=1 ;;
            --print-env) PRINT_ENV=1 ;;
            --uninstall) MODE=uninstall ;;
            --purge-data) PURGE_DATA=1 ;;
            --force) FORCE=1 ;;
            --no-color) USE_COLOR=0 ;;
            --clear) USE_CLEAR=1 ;;
            --no-clear) USE_CLEAR=0 ;;
            -h|--help) usage; exit 0 ;;
            --dir) DIR=$(need_value "$@"); shift ;;
            --host) HOSTNAME_ANSWER=$(need_value "$@"); HOST_SET=1; shift ;;
            --tls) TLS=$(need_value "$@"); shift ;;
            --data-root) DATA_ROOT=$(need_value "$@"); DATA_ROOT_SET=1; shift ;;
            --blobs) BLOBS=$(need_value "$@"); shift ;;
            --database-url) EXTERNAL_DB=$(need_value "$@"); shift ;;
            --redis-url) EXTERNAL_REDIS=$(need_value "$@"); shift ;;
            --components) COMPONENTS=$(need_value "$@"); shift ;;
            --version) VERSION=$(need_value "$@"); VERSION_SET=1; shift ;;
            --channel) CHANNEL=$(need_value "$@"); shift ;;
            --registry) REGISTRY=$(need_value "$@"); shift ;;
            --backup-dir) BACKUP_DIR=$(need_value "$@"); shift ;;
            --backup-keep) BACKUP_KEEP=$(need_value "$@"); shift ;;
            --retention-preset)
                case "$(need_value "$@")" in
                    minimal)
                        SYNC_BACKFILL_DAYS=30; SYNC_BACKFILL_MESSAGES=2000
                        RET_ENGAGEMENT=30; RET_FORMS=30; RET_AUDIT=30
                        RET_WARMUP_MAIL=7; RET_WARMUP_EVENTS=30
                        ;;
                esac
                shift
                ;;
            --no-update-check) UPDATE_CHECK=false ;;
            --*) die "unknown option $1. Run with --help for the list." ;;
            *) die "unexpected argument $1. Every value goes with its flag, e.g. --dir /opt/warmbly." ;;
        esac
        shift
    done
    case "$TLS" in none|caddy|proxy) ;; *) die "--tls must be none, caddy or proxy" ;; esac
    case "$BLOBS" in filesystem|s3) ;; *) die "--blobs must be filesystem or s3" ;; esac
    case "$COMPONENTS" in core|full) ;; *) die "--components must be core or full" ;; esac
    case "$CHANNEL" in stable|dev) ;; *) die "--channel must be stable or dev" ;; esac
}

need_value() {
    if [ $# -lt 2 ] || [ -z "$2" ]; then
        die "$1 needs a value"
    fi
    printf '%s' "$2"
}

# ─────────────────────────────────────────────────────────────────────────
# main
#
# Everything above is a function; nothing runs until this line at the very
# bottom is reached. A download that is cut off half way therefore executes
# nothing at all, which is the honest answer to the objection people have to
# piping a script into a shell.
# ─────────────────────────────────────────────────────────────────────────

main() {
    parse_args "$@"
    setup_term
    TMPDIR_RUN=$(mktemp -d 2>/dev/null || printf '/tmp/warmbly-install.%s' "$$")
    mkdir -p "$TMPDIR_RUN"
    LOGFILE="$TMPDIR_RUN/install.log"
    : >"$LOGFILE"
    trap 'cleanup_term; rm -rf "$TMPDIR_RUN" 2>/dev/null || true' EXIT

    # The demo needs neither of these: it dials nothing and starts nothing.
    if [ "$DEMO" = 0 ]; then
        have curl || fail_with "curl is required and is not installed." \
                               "Install it and run this again (apt install curl / dnf install curl)."
        detect_platform
    fi

    if [ "$MODE" = "uninstall" ]; then
        check_docker
        uninstall
        return 0
    fi

    require_randomness

    if [ "$DRY_RUN" = 0 ] && [ "$PRINT_ENV" = 0 ]; then
        banner
    fi
    [ "$DEMO" = 1 ] && demo_banner

    # A directory that already holds an install is adopted before anything is
    # asked, so every default the wizard shows is this instance's own value.
    # A demo adopts nothing: it must not read an operator's real secrets, and
    # it must not show their hostname back to them as if it had installed
    # something.
    [ "$DEMO" = 1 ] || adopt_existing
    if [ "$DEMO" = 0 ] && [ "$EXISTING" = 0 ] && [ -d "$DIR" ] && [ -n "$(ls -A "$DIR" 2>/dev/null)" ] &&
       [ ! -f "$DIR/$MARKER" ] && [ "$FORCE" = 0 ] && [ "$WIZARD" = 0 ]; then
        fail_with "$DIR is not empty and this installer did not create it." \
                  "Pass --force to install into it anyway, or --dir to pick another path."
    fi

    if [ "$DRY_RUN" = 0 ] && [ "$PRINT_ENV" = 0 ] && [ "$DEMO" = 0 ]; then
        check_docker
    fi

    if [ -n "$VERSION" ]; then
        RESOLVED_TAG=$VERSION
    elif [ "$DEMO" = 1 ]; then
        # No network call in a demo: it would be the one thing the script does
        # for real, which is exactly what a demo should not do.
        RESOLVED_TAG="v0.0.0-demo"
    else
        info "Resolving the newest ${CHANNEL} release"
        resolve_version
        ok "Installing ${RESOLVED_TAG}, pinned in .env so this never moves under you"
    fi

    ensure_secrets

    offer_wizard

    if [ "$WIZARD" = 1 ] && [ "$INTERACTIVE" = 1 ]; then
        # The first step clears the screen, so nothing above it survives being
        # read unless the operator says when.
        say ""
        press_enter "Press enter to begin   ${WHITE}q${DIM} to quit"
        wizard
    elif [ "$WIZARD" = 1 ]; then
        warn "The wizard needs a terminal and there is none, so the defaults and any"
        note "flags or WARMBLY_* variables you passed are used instead."
    fi

    derive

    if [ "$PRINT_ENV" = 1 ]; then
        render_env
        return 0
    fi

    if [ "$DRY_RUN" = 1 ]; then
        step "This is what would be written. Nothing was touched."
        write_file "$DIR/.env" "$(render_env)" 0600
        write_file "$DIR/docker-compose.yml" "$(render_compose)" 0644
        [ "$WANT_CADDY" = 1 ] && write_file "$DIR/Caddyfile" "$(render_caddyfile)" 0644
        [ -n "$BACKUP_DIR" ] && write_file "$DIR/backup.sh" "$(render_backup_script)" 0755
        say ""
        summary_rows | box "$SKY"
        say ""
        return 0
    fi

    if [ "$WIZARD" = 1 ] && [ "$INTERACTIVE" = 1 ]; then
        review
        clear_screen
        banner
        [ "$DEMO" = 1 ] && demo_banner
    fi

    if [ "$DEMO" = 1 ]; then
        say ""
        press_enter "Press enter to watch it install   ${WHITE}q${DIM} to quit"
        demo_install
        return 0
    fi

    step "Installing Warmbly $RESOLVED_TAG into $DIR"
    say ""
    prepare_dirs
    write_all
    ok "Wrote .env and docker-compose.yml"
    say ""
    pull_images
    verify_digests
    say ""
    start_stack
    wait_healthy || true
    finale
}

main "$@"
