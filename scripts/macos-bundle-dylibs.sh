#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <mach-o-binary> <lib-dir>" >&2
  exit 2
fi

ROOT_BINARY="$1"
LIB_DIR="$2"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "macos-bundle-dylibs only supports macOS." >&2
  exit 1
fi
if [[ ! -f "$ROOT_BINARY" ]]; then
  echo "binary not found: $ROOT_BINARY" >&2
  exit 1
fi

mkdir -p "$LIB_DIR"

canonical_path() {
  local path="$1"
  local dir
  dir="$(cd "$(dirname "$path")" && pwd -P)"
  printf "%s/%s\n" "$dir" "$(basename "$path")"
}

is_system_dep() {
  local dep="$1"
  [[ "$dep" == /System/* || "$dep" == /usr/lib/* ]]
}

list_rpaths() {
  local file="$1"
  otool -l "$file" | awk '
    /cmd LC_RPATH/ { in_rpath = 1; next }
    in_rpath && /path / { print $2; in_rpath = 0 }
  '
}

dylib_id() {
  otool -D "$1" 2>/dev/null | sed -n '2p'
}

list_load_deps() {
  local file="$1"
  local id
  id="$(dylib_id "$file")"
  otool -L "$file" | awk 'NR > 1 { print $1 }' | while IFS= read -r dep; do
    [[ -n "$id" && "$dep" == "$id" ]] && continue
    printf "%s\n" "$dep"
  done
}

expand_relative_token() {
  local token_path="$1"
  local from_file="$2"
  local executable_dir
  executable_dir="$(dirname "$ROOT_BINARY")"

  case "$token_path" in
    @loader_path)
      printf "%s\n" "$(dirname "$from_file")"
      ;;
    @loader_path/*)
      printf "%s/%s\n" "$(dirname "$from_file")" "${token_path#@loader_path/}"
      ;;
    @executable_path)
      printf "%s\n" "$executable_dir"
      ;;
    @executable_path/*)
      printf "%s/%s\n" "$executable_dir" "${token_path#@executable_path/}"
      ;;
    *)
      printf "%s\n" "$token_path"
      ;;
  esac
}

resolve_dep() {
  local dep="$1"
  local from_file="$2"

  case "$dep" in
    @rpath/*)
      local rel="${dep#@rpath/}"
      local rpath expanded candidate
      while IFS= read -r rpath; do
        expanded="$(expand_relative_token "$rpath" "$from_file")"
        candidate="$expanded/$rel"
        if [[ -f "$candidate" ]]; then
          canonical_path "$candidate"
          return 0
        fi
      done < <(list_rpaths "$from_file")
      while IFS= read -r rpath; do
        expanded="$(expand_relative_token "$rpath" "$ROOT_BINARY")"
        candidate="$expanded/$rel"
        if [[ -f "$candidate" ]]; then
          canonical_path "$candidate"
          return 0
        fi
      done < <(list_rpaths "$ROOT_BINARY")
      candidate="$LIB_DIR/$rel"
      if [[ -f "$candidate" ]]; then
        canonical_path "$candidate"
        return 0
      fi
      return 1
      ;;
    @loader_path/*|@executable_path/*)
      local resolved
      resolved="$(expand_relative_token "$dep" "$from_file")"
      [[ -f "$resolved" ]] || return 1
      canonical_path "$resolved"
      ;;
    /*)
      [[ -f "$dep" ]] || return 1
      canonical_path "$dep"
      ;;
    *)
      return 1
      ;;
  esac
}

has_rpath() {
  local file="$1"
  local needle="$2"
  local existing_rpath
  while IFS= read -r existing_rpath; do
    [[ "$existing_rpath" == "$needle" ]] && return 0
  done < <(list_rpaths "$file")
  return 1
}

add_rpath_once() {
  local file="$1"
  local wanted_rpath="$2"
  has_rpath "$file" "$wanted_rpath" && return 0
  install_name_tool -add_rpath "$wanted_rpath" "$file"
}

delete_nonportable_rpaths() {
  local file="$1"
  local existing_rpath
  while IFS= read -r existing_rpath; do
    case "$existing_rpath" in
      /Users/*|/opt/homebrew/*|/usr/local/*)
        install_name_tool -delete_rpath "$existing_rpath" "$file" 2>/dev/null || true
        ;;
    esac
  done < <(list_rpaths "$file")
}

declare -a COPIED_BASES=()
declare -a COPIED_TARGETS=()
declare -a SOURCE_PATHS=()
declare -a SCAN_QUEUE=("$ROOT_BINARY")

copied_index() {
  local base="$1"
  local i
  for i in "${!COPIED_BASES[@]}"; do
    if [[ "${COPIED_BASES[$i]}" == "$base" ]]; then
      printf "%s\n" "$i"
      return 0
    fi
  done
  return 1
}

while ((${#SCAN_QUEUE[@]} > 0)); do
  current="${SCAN_QUEUE[0]}"
  SCAN_QUEUE=("${SCAN_QUEUE[@]:1}")

  while IFS= read -r dep; do
    [[ -z "$dep" ]] && continue
    is_system_dep "$dep" && continue

    if ! resolved="$(resolve_dep "$dep" "$current")"; then
      echo "failed to resolve dependency $dep from $current" >&2
      exit 1
    fi

    base="$(basename "$resolved")"
    target="$LIB_DIR/$base"

    existing_index="$(copied_index "$base" || true)"
    if [[ -n "$existing_index" ]]; then
      if [[ "$resolved" == "$(canonical_path "$target")" ]]; then
        continue
      fi
      if [[ "${SOURCE_PATHS[$existing_index]}" != "$resolved" ]]; then
        echo "conflicting dylib basename: $base" >&2
        echo "  ${SOURCE_PATHS[$existing_index]}" >&2
        echo "  $resolved" >&2
        exit 1
      fi
      continue
    fi

    cp -f "$resolved" "$target"
    chmod u+w "$target"
    COPIED_BASES+=("$base")
    COPIED_TARGETS+=("$target")
    SOURCE_PATHS+=("$resolved")
    SCAN_QUEUE+=("$resolved")
  done < <(list_load_deps "$current")
done

chmod u+w "$ROOT_BINARY"
delete_nonportable_rpaths "$ROOT_BINARY"
add_rpath_once "$ROOT_BINARY" "@loader_path/../lib"

for target in "${COPIED_TARGETS[@]}"; do
  base="$(basename "$target")"
  install_name_tool -id "@rpath/$base" "$target" 2>/dev/null || true
  delete_nonportable_rpaths "$target"
  add_rpath_once "$target" "@loader_path"
done

patch_loads() {
  local file="$1"
  local dep base
  while IFS= read -r dep; do
    [[ -z "$dep" ]] && continue
    is_system_dep "$dep" && continue
    base="$(basename "$dep")"
    [[ -f "$LIB_DIR/$base" ]] || continue
    [[ "$dep" == "@rpath/$base" ]] && continue
    install_name_tool -change "$dep" "@rpath/$base" "$file" 2>/dev/null || true
  done < <(list_load_deps "$file")
}

patch_loads "$ROOT_BINARY"
for target in "${COPIED_TARGETS[@]}"; do
  patch_loads "$target"
done

if command -v codesign >/dev/null 2>&1; then
  for target in "${COPIED_TARGETS[@]}"; do
    codesign --force --sign - "$target" >/dev/null 2>&1
  done
  codesign --force --sign - "$ROOT_BINARY" >/dev/null 2>&1
fi

echo "Bundled ${#COPIED_TARGETS[@]} dylib(s) into $LIB_DIR"
