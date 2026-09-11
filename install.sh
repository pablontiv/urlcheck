#!/bin/sh
# instala urlcheck en Linux o macOS
#   curl -fsSL https://raw.githubusercontent.com/pablontiv/urlcheck/main/install.sh | sh
#   PREFIX=/usr/local/bin sh install.sh
set -eu

REPO="${URLCHECK_REPO:-pablontiv/urlcheck}"
PREFIX="${PREFIX:-$HOME/.local/bin}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/urlcheck"
TAG="${URLCHECK_VERSION:-latest}"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/main"

info() { printf 'urlcheck-install: %s\n' "$*"; }
die() { printf 'urlcheck-install: error: %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "hace falta '$1'"
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux)  goos=linux ;;
  darwin) goos=darwin ;;
  *) die "SO no soportado: $os (solo linux/darwin)" ;;
esac
case "$arch" in
  x86_64|amd64)   goarch=amd64 ;;
  aarch64|arm64)  goarch=arm64 ;;
  *) die "arch no soportada: $arch (solo amd64/arm64)" ;;
esac

asset="urlcheck-${goos}-${goarch}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT HUP

download() {
  url=$1
  dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    die "hace falta curl o wget"
  fi
}

api_get() {
  url=$1
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -H "Accept: application/vnd.github+json" "$url"
  else
    wget -qO- --header="Accept: application/vnd.github+json" "$url"
  fi
}

install_list() {
  mkdir -p "$CONFIG_DIR"
  if [ ! -f "$CONFIG_DIR/services.txt" ]; then
    info "bajando services.txt → $CONFIG_DIR/services.txt"
    if download "${RAW_BASE}/services.txt" "$CONFIG_DIR/services.txt"; then
      :
    else
      info "no pude bajar services.txt (¿repo privado?). Cópialo a $CONFIG_DIR/services.txt"
    fi
  else
    info "ya existe $CONFIG_DIR/services.txt (no se pisa)"
  fi
}

install_bin() {
  mkdir -p "$PREFIX"
  src=$1
  chmod +x "$src"
  dest="$PREFIX/urlcheck"
  cp "$src" "$dest"
  info "instalado $dest"
}

try_release() {
  if [ "$TAG" = latest ]; then
    endpoint="https://api.github.com/repos/${REPO}/releases/latest"
  else
    endpoint="https://api.github.com/repos/${REPO}/releases/tags/${TAG}"
  fi
  json=$(api_get "$endpoint" 2>/dev/null) || return 1
  printf '%s' "$json" | grep -q '"tag_name"' || return 1

  url=$(printf '%s' "$json" | tr '"' '\n' | grep "/${asset}$" | head -n1)
  if [ -z "$url" ]; then
    url=$(printf '%s' "$json" | tr '"' '\n' | grep "${asset}" | grep 'https://' | head -n1)
  fi
  [ -n "$url" ] || return 1

  info "bajando $asset desde release"
  download "$url" "$tmp/$asset" || return 1
  install_bin "$tmp/$asset"
}

try_source() {
  need git
  need go
  info "no hay release; compilando desde source"
  git clone --depth 1 "https://github.com/${REPO}.git" "$tmp/src"
  (cd "$tmp/src" && go build -trimpath -ldflags="-s -w" -o "$tmp/urlcheck" .)
  install_bin "$tmp/urlcheck"
}

if try_release; then
  :
elif try_source; then
  :
else
  die "no pude instalar: publica un tag v* o ten git+go"
fi

install_list

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    info "añade $PREFIX a PATH, por ejemplo:"
    info "  echo 'export PATH=\"$PREFIX:\$PATH\"' >> ~/.bashrc"
    ;;
esac

info "listo. prueba: urlcheck -list $CONFIG_DIR/services.txt"
info "o, si services.txt está en el cwd: urlcheck"
