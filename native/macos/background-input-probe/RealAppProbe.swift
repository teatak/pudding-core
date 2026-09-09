// Explicit user-authorized Mail/Feishu/Calendar smoke, not an arbitrary-app input API.
import AppKit
import ApplicationServices

enum RealApp: String, Decodable, CaseIterable {
  case mail, feishu, calendar

  private var identity: (id: String, path: String, name: String) {
    switch self {
    case .mail: return ("com.apple.mail", "/System/Applications/Mail.app", "Mail")
    case .feishu: return ("com.electron.lark", "/Applications/Lark.app", "Feishu")
    case .calendar: return ("com.apple.iCal", "/System/Applications/Calendar.app", "Calendar")
    }
  }
  var bundleID: String { identity.id }
  var bundlePath: String { identity.path }
  var executable: String { bundlePath + "/Contents/MacOS/" + identity.name }

  func accepts(bundleID: String?, bundlePath: String?, executable: String) -> Bool {
    bundleID == self.bundleID && bundlePath == self.bundlePath && executable == self.executable
  }

  func matches(_ app: NSRunningApplication, executable: String) -> Bool {
    accepts(bundleID: app.bundleIdentifier, bundlePath: app.bundleURL?.path, executable: executable)
      && app.executableURL?.path == executable && !app.isTerminated
  }

  func windows() throws -> [String: Any] {
    try require(CGPreflightScreenCaptureAccess(), "screen permission missing; no request issued")
    let apps = NSWorkspace.shared.runningApplications.filter { matches($0, executable: executable) }
    guard apps.count == 1, let app = apps.first else { throw ProbeError("running test app missing or ambiguous") }
    let candidates = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], 0) as? [[String: Any]] ?? []
    let windows: [[String: Any]] = candidates.compactMap { info in
      guard (info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == app.processIdentifier,
        (info[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
        let bounds = info[kCGWindowBounds as String] as? NSDictionary,
        let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary), frame.width > 100, frame.height > 100
      else { return nil }
      return ["pid": app.processIdentifier, "executable": executable, "windowID": info[kCGWindowNumber as String]!,
        "title": info[kCGWindowName as String] ?? "", "active": app.isActive, "points": [String: String](),
        "frame": ["x": frame.minX, "y": frame.minY, "width": frame.width, "height": frame.height]]
    }
    return ["realApp": rawValue, "pid": app.processIdentifier, "windows": windows,
      "foregroundPID": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0]
  }
}

// Identity policy shared by the caller, worker and crash restoration.
func isFocusTarget(_ app: NSRunningApplication, request: Request) -> Bool {
  if let realApp = request.realApp { return realApp.matches(app, executable: request.target.executable) }
  return isFixture(app, executable: request.target.executable)
}

func realAppSelfTest(target: Target) throws {
  for app in RealApp.allCases {
    try require(app.accepts(bundleID: app.bundleID, bundlePath: app.bundlePath, executable: app.executable), "real app identity rejected")
    try require(!app.accepts(bundleID: app.bundleID, bundlePath: "/tmp/Copy.app", executable: app.executable)
      && !app.accepts(bundleID: "unknown", bundlePath: app.bundlePath, executable: app.executable)
      && !app.accepts(bundleID: app.bundleID, bundlePath: app.bundlePath, executable: "/tmp/other"), "real app identity widened")
    let request = Request(target: target, guardPID: 200, variant: "appkit-window-local", action: "click", realApp: app)
    var rejected = false
    do { try send(request) } catch let error as ProbeError {
      try require(error.description == "real-app smoke requires the shared ordinary-click route", error.description)
      rejected = true
    }
    try require(rejected, "real app entered an unowned diagnostic route")
    for (variant, action) in [("appkit-command", "click"), ("quartz", "click"),
      ("appkit-window-local", "drag"), ("appkit-window-local", "scroll")] {
      rejected = false
      do {
        try send(Request(target: target, guardPID: 200, variant: variant, action: action, realApp: app), syntheticActivation: true)
      } catch let error as ProbeError {
        try require(error.description == "synthetic activation probe only permits one ordinary click", error.description)
        rejected = true
      }
      try require(rejected, "real-app smoke accepted an unsupported action")
    }
  }
  try require((try? JSONDecoder().decode(RealApp.self, from: Data("\"arbitrary\"".utf8))) == nil, "arbitrary app accepted")
}
