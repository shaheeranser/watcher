#!/bin/sh
# Watcher installer.
#
#   curl -fsSL https://raw.githubusercontent.com/shaheeranser/watcher/main/install.sh | sudo sh
#
# Downloads the release artifact for this OS/architecture, verifies it against
# the published checksum, and installs the binary. No Go toolchain or build step
# is involved. Everything is fail-fast: any error aborts non-zero and leaves no
# partial install.
#
# Overrides (environment or the matching flag after `sh -s --`):
#   WATCHER_VERSION            release tag, or "latest" (default)
#   WATCHER_INSTALL_DIR        target bin directory (default /usr/local/bin)
#   WATCHER_RELEASE_BASE_URL   release base URL (default the GitHub repo)
#   WATCHER_UNIT_DIR           where the systemd unit goes (default /etc/systemd/system)
#   WATCHER_OS / WATCHER_ARCH  override detection (for cross-installs and tests)
#   --uninstall                remove the binary and unit
#   --purge                    with --uninstall, also remove the config file
#   --service                  also install and enable the systemd unit

set -eu

OWNER="shaheeranser"
REPO="watcher"

RELEASE_BASE="${WATCHER_RELEASE_BASE_URL:-https://github.com/${OWNER}/${REPO}/releases}"
VERSION="${WATCHER_VERSION:-latest}"
INSTALL_DIR="${WATCHER_INSTALL_DIR:-/usr/local/bin}"
UNIT_DIR="${WATCHER_UNIT_DIR:-/etc/systemd/system}"
UNINSTALL=0
PURGE=0
SERVICE=0

UNIT_NAME="watcher.service"
CONFIG_PATH="${WATCHER_CONFIG:-$HOME/.config/watcher/config.toml}"
MARKER_START="# >>> watcher installer >>>"
MARKER_END="# <<< watcher installer <<<"

die() {
	echo "watcher installer: $*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Install Watcher from a release, or remove it.

Usage:
  curl -fsSL <url> | sudo sh [-- <flags>]

Flags:
  --version VERSION   release tag to install (default: latest)
  --dir DIR           install the binary into DIR (default: /usr/local/bin)
  --release-base URL  release base URL to fetch from
  --service           also install and enable the systemd unit
  --uninstall         remove the binary and unit
  --purge             with --uninstall, also remove the config file
  -h, --help          show this help
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
	--version=*) VERSION="${1#*=}"; shift ;;
	--dir) [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR="$2"; shift 2 ;;
	--dir=*) INSTALL_DIR="${1#*=}"; shift ;;
	--release-base) [ $# -ge 2 ] || die "--release-base needs a value"; RELEASE_BASE="$2"; shift 2 ;;
	--release-base=*) RELEASE_BASE="${1#*=}"; shift ;;
	--service) SERVICE=1; shift ;;
	--uninstall) UNINSTALL=1; shift ;;
	--purge) PURGE=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown option: $1 (try --help)" ;;
	esac
done

[ "$PURGE" = 1 ] && UNINSTALL=1

# fetch downloads url to dest using curl or wget.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "need curl or wget to download the release"
	fi
}

# sha256 prints the SHA-256 of a file.
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need sha256sum or shasum to verify the download"
	fi
}

# verify checks that file's digest matches the entry for name in the checksums
# file, aborting on any mismatch or missing entry (INST-PKG-3).
verify() {
	file="$1"
	name="$2"
	want=$(awk -v a="$name" '{ n=$2; sub(/^\*/, "", n); if (n == a) print $1 }' "$CHECKSUMS")
	[ -n "$want" ] || die "no published checksum for $name"
	got=$(sha256 "$file")
	[ "$want" = "$got" ] || die "checksum mismatch for $name: expected $want, got $got"
}

remove_path_block() {
	profile="$HOME/.profile"
	[ -f "$profile" ] || return 0
	if grep -qF "$MARKER_START" "$profile"; then
		sed -i.bak "/^${MARKER_START}\$/,/^${MARKER_END}\$/d" "$profile" 2>/dev/null || true
		rm -f "$profile.bak"
	fi
}

uninstall() {
	rm -f "$INSTALL_DIR/watcher" || die "cannot remove $INSTALL_DIR/watcher (try sudo)"
	if command -v systemctl >/dev/null 2>&1; then
		systemctl disable --now watcher >/dev/null 2>&1 || true
	fi
	rm -f "$UNIT_DIR/$UNIT_NAME" || die "cannot remove $UNIT_DIR/$UNIT_NAME (try sudo)"
	if command -v systemctl >/dev/null 2>&1; then
		systemctl daemon-reload >/dev/null 2>&1 || true
	fi
	remove_path_block
	if [ "$PURGE" = 1 ]; then
		rm -f "$CONFIG_PATH" || die "cannot remove config $CONFIG_PATH"
		echo "Removed the config file at $CONFIG_PATH."
	else
		echo "Left the config file at $CONFIG_PATH (use --purge to remove it)."
	fi
	echo "Uninstalled watcher."
}

if [ "$UNINSTALL" = 1 ]; then
	uninstall
	exit 0
fi

os="${WATCHER_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
case "$os" in
linux | darwin) ;;
*) die "unsupported operating system: $os (only linux and darwin builds are published)" ;;
esac

arch="${WATCHER_ARCH:-$(uname -m)}"
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $arch (only amd64 and arm64 builds are published)" ;;
esac

asset="watcher_${os}_${arch}.tar.gz"
if [ "$VERSION" = latest ]; then
	release_url="${RELEASE_BASE}/latest/download"
else
	release_url="${RELEASE_BASE}/download/${VERSION}"
fi

tmp=$(mktemp -d) || die "cannot create a temporary directory"
trap 'rm -rf "$tmp"' EXIT INT TERM

CHECKSUMS="$tmp/checksums.txt"
fetch "${release_url}/checksums.txt" "$CHECKSUMS" || die "cannot download checksums from ${release_url}"
fetch "${release_url}/${asset}" "$tmp/${asset}" || die "cannot download ${asset} (is ${VERSION} published for ${os}/${arch}?)"
verify "$tmp/${asset}" "$asset"

tar -xzf "$tmp/${asset}" -C "$tmp" || die "cannot extract ${asset}"
[ -f "$tmp/watcher" ] || die "${asset} did not contain a watcher binary"

mkdir -p "$INSTALL_DIR" 2>/dev/null || die "cannot create $INSTALL_DIR (try sudo)"
cp "$tmp/watcher" "$INSTALL_DIR/watcher" || die "cannot write to $INSTALL_DIR (try sudo, or set WATCHER_INSTALL_DIR)"
chmod 0755 "$INSTALL_DIR/watcher"

# Make the binary reachable from PATH. A system directory is normally already
# on PATH; a per-user one is added through a marked block the uninstall reverses.
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	profile="$HOME/.profile"
	if [ -f "$profile" ] && grep -qF "$MARKER_START" "$profile"; then
		:
	else
		{
			echo ""
			echo "$MARKER_START"
			echo "export PATH=\"$INSTALL_DIR:\$PATH\""
			echo "$MARKER_END"
		} >>"$profile"
		echo "Added $INSTALL_DIR to PATH in $profile; restart your shell or run: export PATH=\"$INSTALL_DIR:\$PATH\""
	fi
	;;
esac

if [ "$SERVICE" = 1 ]; then
	if ! command -v systemctl >/dev/null 2>&1; then
		echo "systemctl not found; skipping the service install." >&2
	else
		fetch "${release_url}/${UNIT_NAME}" "$tmp/${UNIT_NAME}" || die "cannot download ${UNIT_NAME}"
		verify "$tmp/${UNIT_NAME}" "$UNIT_NAME"
		mkdir -p "$UNIT_DIR" 2>/dev/null || die "cannot create $UNIT_DIR (try sudo)"
		cp "$tmp/${UNIT_NAME}" "$UNIT_DIR/${UNIT_NAME}" || die "cannot write $UNIT_DIR/${UNIT_NAME} (try sudo)"
		systemctl daemon-reload || die "systemctl daemon-reload failed"
		systemctl enable --now watcher || die "systemctl enable --now watcher failed"
		echo "Installed and enabled the systemd unit."
	fi
fi

echo "Installed watcher ${VERSION} to ${INSTALL_DIR}/watcher."
echo "Next: run 'watcher onboard' to configure it."
