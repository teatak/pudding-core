import Foundation

enum AppPolicy {
  private static let blockedBundleIDs: Set<String> = [
    "com.1password.1password",
    "com.agilebits.onepassword7",
    "com.apple.SecurityAgent",
    "com.apple.Terminal",
    "com.apple.keychainaccess",
    "com.apple.systempreferences",
    "com.bitwarden.desktop",
    "com.googlecode.iterm2",
    "com.teatak.pudding",
    "com.teatak.pudding.computer-use-helper",
    "com.teatak.pudding.dev",
    "com.teatak.pudding.dev.computer-use-helper",
    "dev.warp.Warp-Stable",
    "org.keepassxc.keepassxc",
  ]

  static func allows(bundleID: String) -> Bool {
    !blockedBundleIDs.contains(bundleID)
  }

  static func allows(bundleID: String, pid: pid_t, parentPID: pid_t = getppid()) -> Bool {
    pid != parentPID && allows(bundleID: bundleID)
  }
}
