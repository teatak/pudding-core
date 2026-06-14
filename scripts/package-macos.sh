#!/usr/bin/env bash
# 打包 macOS 桌面壳为 unsigned .app + .dmg(当前架构,release 通道:9669 / ~/.pudding)。
# 不含签名/公证——本机/内部自用;发给别人首次打开需右键 → 打开(绕 Gatekeeper)。
# 用法: scripts/package-macos.sh v0.1.0
set -euo pipefail
cd "$(dirname "$0")/.."
MODULE="github.com/teatak/pudding-core"

version="${1:-}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9._-]+)?$ ]]; then
  echo "usage: $0 v0.1.0   (version must look like v0.1.0)" >&2
  exit 2
fi
ver="${version#v}"

goos="$(go env GOOS)"; goarch="$(go env GOARCH)"
if [[ "$goos" != "darwin" ]]; then
  echo "macOS packaging requires GOOS=darwin, got $goos" >&2
  exit 1
fi

# 1) 前端 embed + release 通道二进制(cgo:mattn-sqlite + 壳 AppKit)
echo ">> building web embed + release binary ($goos/$goarch)"
make embed
CGO_ENABLED=1 go build \
  -ldflags "-X $MODULE/internal/buildinfo.channel=release" \
  -o bin/pudding-desktop ./cmd/pudding-desktop

# 2) 组 .app bundle
workdir="$(mktemp -d "${TMPDIR:-/tmp}/pudding-pkg.XXXXXX")"
trap 'rm -rf "$workdir"' EXIT
app="$workdir/Pudding.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp bin/pudding-desktop "$app/Contents/MacOS/pudding-desktop"
chmod 0755 "$app/Contents/MacOS/pudding-desktop"
cp build/macos/AppIcon.icns "$app/Contents/Resources/AppIcon.icns"

cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleExecutable</key><string>pudding-desktop</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleIdentifier</key><string>com.teatak.pudding</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Pudding</string>
  <key>CFBundleDisplayName</key><string>Pudding</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$ver</string>
  <key>CFBundleVersion</key><string>$ver</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 Teatak. All rights reserved.</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>
  <key>CFBundleLocalizations</key>
  <array><string>en</string><string>zh-Hans</string><string>zh-Hant</string></array>
</dict>
</plist>
EOF
plutil -lint "$app/Contents/Info.plist" >/dev/null

# 3) DMG(直接 UDZO 压缩只读盘;带 Applications 软链拖拽安装)
dist_dir="$PWD/dist"; mkdir -p "$dist_dir"
name="Pudding-$version-$goarch"
dmg="$dist_dir/$name.dmg"
rm -f "$dmg" "$dmg.sha256"
stage="$workdir/stage"; mkdir -p "$stage"
cp -R "$app" "$stage/Pudding.app"
ln -s /Applications "$stage/Applications"
hdiutil create -volname "Pudding" -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null
(cd "$dist_dir" && shasum -a 256 "$name.dmg" > "$name.dmg.sha256")

# 同时在 dist 留一份 .app,便于直接拷进 /Applications 跑
rm -rf "$dist_dir/Pudding.app"
cp -R "$app" "$dist_dir/Pudding.app"

echo ">> done (unsigned):"
echo "   $dmg"
echo "   $dist_dir/Pudding.app"
echo ">> unsigned: to open on another Mac, right-click → Open, or: xattr -dr com.apple.quarantine Pudding.app"
