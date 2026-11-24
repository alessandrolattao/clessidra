#!/bin/sh
# Install the latest clessidra release for Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/alessandrolattao/clessidra/main/install.sh | sh
#
# INSTALL_DIR picks the directory, ~/.local/bin by default.
set -eu

repo="alessandrolattao/clessidra"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "clessidra: $*" >&2
	exit 1
}

[ "$(uname -s)" = Linux ] || fail "Linux only: on macOS Claude Code keeps its login in the Keychain, which clessidra does not read"

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no release for $(uname -m), build it with: go install github.com/$repo@latest" ;;
esac

for cmd in curl tar sha256sum; do
	command -v "$cmd" >/dev/null || fail "$cmd not found"
done

# releases/latest redirects to the page of the latest tag, so no API call and no rate limit.
latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
version="${latest##*/}"
case "$version" in
v*) ;;
*) fail "cannot find the latest release" ;;
esac

name="clessidra-$version-linux-$arch"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
(cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c -) >/dev/null || fail "$name.tar.gz does not match checksums.txt"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$install_dir"
install -m 0755 "$tmp/$name/clessidra" "$install_dir/clessidra"

echo "clessidra $version installed in $install_dir/clessidra"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "$install_dir is not in your PATH, add it to run clessidra" >&2 ;;
esac
