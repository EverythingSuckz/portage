#!/bin/sh
# Installs portage on macOS or Linux:
#   curl -fsSL https://go.wrench.rocks/portage/install.sh | sh
#
# Optional settings:
#   PORTAGE_VERSION=v0.2.0      install a specific release instead of the latest
#   PORTAGE_INSTALL_DIR=<dir>   install somewhere other than ~/.local/bin
#   PORTAGE_NO_APPS=1           don't add portage to your AI apps
set -eu

repo="EverythingSuckz/portage"
dir="${PORTAGE_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "portage: $*" >&2
	exit 1
}

case "${PORTAGE_OS:-$(uname -s)}" in
Linux | linux) os=linux ;;
Darwin | darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN*) fail "on Windows, run this in PowerShell instead: irm https://go.wrench.rocks/portage/install.ps1 | iex" ;;
*) fail "unsupported system: $(uname -s)" ;;
esac

case "${PORTAGE_ARCH:-$(uname -m)}" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported processor: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

version="${PORTAGE_VERSION:-}"
if [ -z "$version" ]; then
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "couldn't reach GitHub to find the latest release"
	version="${latest##*/}"
fi
case "$version" in
v*) ;;
*) fail "couldn't work out the latest release (got \"$version\")" ;;
esac

file="portage_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading portage $version for $os/$arch..."
curl -fsSL -o "$tmp/$file" "$base/$file" || fail "download failed: $base/$file"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "couldn't download checksums.txt"

want=$(grep " $file\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$want" ] || fail "$file is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)
else
	fail "sha256sum or shasum is required to check the download"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $file; not installing"

tar -xzf "$tmp/$file" -C "$tmp" portage
mkdir -p "$dir"
mv -f "$tmp/portage" "$dir/portage"
chmod +x "$dir/portage"
echo "Installed $dir/portage"

case ":$PATH:" in
*":$dir:"*) ;;
*)
	echo
	echo "$dir is not on your PATH. To run portage from a terminal, add this to your shell profile:"
	echo "  export PATH=\"$dir:\$PATH\""
	;;
esac

if [ "${PORTAGE_NO_APPS:-}" != "1" ]; then
	echo
	echo "Adding portage to your AI apps..."
	"$dir/portage" install || true
fi
