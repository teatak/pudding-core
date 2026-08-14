import AppKit
import ApplicationServices
import Foundation

final class AccessibilityService {
  private struct ElementRecord {
    let element: AXUIElement
    let snapshot: ObservedElementSnapshot
  }

  func listApplications(current applications: [ScreenCaptureApplication]? = nil) -> [RunningApplicationSnapshot] {
    let trusted = AXIsProcessTrusted()
    let runningApplications = applications.map { current in
      current.compactMap { NSRunningApplication(processIdentifier: $0.pid) }
    } ?? NSWorkspace.shared.runningApplications
    var seenPIDs = Set<pid_t>()
    return runningApplications
      .filter { !$0.isTerminated && $0.activationPolicy == .regular }
      .filter { seenPIDs.insert($0.processIdentifier).inserted }
      .compactMap { app in
        guard let bundleID = app.bundleIdentifier else { return nil }
        let windows = trusted ? windowSnapshots(for: app.processIdentifier) : []
        return RunningApplicationSnapshot(
          bundleID: bundleID,
          name: app.localizedName ?? bundleID,
          pid: app.processIdentifier,
          active: app.isActive,
          controllable: AppPolicy.allows(bundleID: bundleID, pid: app.processIdentifier),
          windows: windows
        )
      }
      .sorted {
        if $0.active != $1.active { return $0.active }
        return $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
      }
  }

  func observe(
    bundleID: String,
    windowID: UInt32?,
    targetWindow: CapturableWindowSnapshot?,
    maxElements: Int
  ) throws -> ObservationSnapshot {
    try requireAccessibility()
    let app = try runningApplication(bundleID: bundleID)
    let collection = try collectElements(
      pid: app.processIdentifier,
      windowID: windowID,
      targetWindow: targetWindow,
      maxElements: maxElements
    )
    return ObservationSnapshot(
      bundleID: bundleID,
      windowID: windowID,
      name: app.localizedName ?? bundleID,
      pid: app.processIdentifier,
      observedAt: Date(),
      truncated: collection.truncated,
      windows: collection.windows,
      elements: collection.records.map(\.snapshot)
    )
  }

  func act(
    bundleID: String,
    windowID: UInt32,
    targetWindow: CapturableWindowSnapshot?,
    elementID: String,
    action: ElementAction,
    value: String?
  ) throws -> ActionSnapshot {
    try requireAccessibility()
    let app = try runningApplication(bundleID: bundleID)
    let collection = try collectElements(
      pid: app.processIdentifier,
      windowID: windowID,
      targetWindow: targetWindow,
      maxElements: 1_000
    )
    let matches = collection.records.filter { $0.snapshot.elementID == elementID }
    guard let record = matches.first else {
      throw HelperError.elementNotFound(elementID)
    }
    guard matches.count == 1 else {
      throw HelperError.ambiguousElement(elementID)
    }

    switch action {
    case .press:
      guard record.snapshot.actions.contains(ElementAction.press.rawValue) else {
        throw HelperError.elementNotActionable("AXPress is unavailable")
      }
      let error = AXUIElementPerformAction(record.element, kAXPressAction as CFString)
      guard error == .success else {
        throw HelperError.actionFailed("AXPress returned \(error.rawValue)")
      }
    case .setValue:
      guard let value else {
        throw HelperError.elementNotActionable("value is required")
      }
      guard !record.snapshot.secure else {
        throw HelperError.elementNotActionable("secure fields cannot be written by C0 helper")
      }
      var settable = DarwinBoolean(false)
      let check = AXUIElementIsAttributeSettable(
        record.element,
        kAXValueAttribute as CFString,
        &settable
      )
      guard check == .success, settable.boolValue else {
        throw HelperError.elementNotActionable("AXValue is not settable")
      }
      let error = AXUIElementSetAttributeValue(
        record.element,
        kAXValueAttribute as CFString,
        value as CFString
      )
      guard error == .success else {
        throw HelperError.actionFailed("set AXValue returned \(error.rawValue)")
      }
      let actual = ValueSanitizer.string(
        copyAttribute(record.element, kAXValueAttribute as CFString),
        secure: false,
        limit: max(500, value.count)
      )
      guard actual == value else {
        throw HelperError.actionFailed("AXValue verification failed")
      }
    }

    return ActionSnapshot(
      bundleID: bundleID,
      elementID: elementID,
      action: action,
      completed: true
    )
  }

  private func requireAccessibility() throws {
    guard AXIsProcessTrusted() else {
      throw HelperError.permissionRequired("accessibility")
    }
  }

  private func runningApplication(bundleID: String) throws -> NSRunningApplication {
    guard
      let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
        .first(where: { !$0.isTerminated })
    else {
      throw HelperError.appNotFound(bundleID)
    }
    guard AppPolicy.allows(bundleID: bundleID, pid: app.processIdentifier) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    return app
  }

  private func windowSnapshots(for pid: pid_t) -> [ApplicationWindowSnapshot] {
    let appElement = AXUIElementCreateApplication(pid)
    return windows(of: appElement).enumerated().map { index, window in
      ApplicationWindowSnapshot(
        index: index,
        windowID: windowID(of: window),
        title: attributeString(window, kAXTitleAttribute as CFString),
        frame: frame(of: window)
      )
    }
  }

  private func collectElements(
    pid: pid_t,
    windowID requestedWindowID: UInt32?,
    targetWindow: CapturableWindowSnapshot?,
    maxElements: Int
  ) throws -> (windows: [ApplicationWindowSnapshot], records: [ElementRecord], truncated: Bool) {
    let appElement = AXUIElementCreateApplication(pid)
    let allWindows = windows(of: appElement)
    let appWindows: [AXUIElement]
    if let requestedWindowID {
      let candidates = allWindows.enumerated().map { index, window in
        ApplicationWindowSnapshot(
          index: index,
          windowID: windowID(of: window),
          title: attributeString(window, kAXTitleAttribute as CFString),
          frame: frame(of: window)
        )
      }
      guard
        let resolvedIndex = WindowResolver.resolveIndex(
          requestedWindowID: requestedWindowID,
          targetWindow: targetWindow,
          candidates: candidates
        )
      else {
        throw HelperError.windowNotFound(requestedWindowID)
      }
      appWindows = [allWindows[resolvedIndex]]
    } else {
      appWindows = allWindows
    }
    let windowSnapshots = appWindows.enumerated().map { index, window in
      ApplicationWindowSnapshot(
        index: index,
        windowID: windowID(of: window) ?? requestedWindowID,
        title: attributeString(window, kAXTitleAttribute as CFString),
        frame: frame(of: window)
      )
    }

    var records: [ElementRecord] = []
    var truncated = false
    for (windowIndex, window) in appWindows.enumerated() {
      let currentWindowID = windowID(of: window) ?? requestedWindowID
      var queue: [(element: AXUIElement, depth: Int, path: [Int])] = [(window, 0, [])]
      var cursor = 0
      while cursor < queue.count {
        if records.count >= maxElements {
          truncated = true
          break
        }
        let (element, depth, path) = queue[cursor]
        cursor += 1
        let role = attributeString(element, kAXRoleAttribute as CFString)
        let subrole = attributeString(element, kAXSubroleAttribute as CFString)
        let identifier = attributeString(element, kAXIdentifierAttribute as CFString)
        let label = attributeString(element, kAXTitleAttribute as CFString)
        let elementFrame = frame(of: element)
        let elementID = ElementIdentity.make(
          windowIndex: windowIndex,
          path: path,
          role: role,
          subrole: subrole,
          identifier: identifier,
          label: label,
          frame: elementFrame
        )
        let secure = subrole == (kAXSecureTextFieldSubrole as String)
        let actions = supportedActions(element, secure: secure)
        let snapshot = ObservedElementSnapshot(
          elementID: elementID,
          windowIndex: windowIndex,
          windowID: currentWindowID,
          role: role,
          subrole: subrole,
          label: label,
          description: attributeString(element, kAXDescriptionAttribute as CFString),
          value: ValueSanitizer.string(
            copyAttribute(element, kAXValueAttribute as CFString),
            secure: secure
          ),
          secure: secure,
          enabled: attributeBool(element, kAXEnabledAttribute as CFString),
          focused: attributeBool(element, kAXFocusedAttribute as CFString),
          frame: elementFrame,
          actions: actions
        )
        records.append(ElementRecord(element: element, snapshot: snapshot))

        if depth < 8 {
          for (childIndex, child) in children(of: element).enumerated() {
            queue.append((child, depth + 1, path + [childIndex]))
          }
        }
      }
      if truncated { break }
    }
    return (windowSnapshots, records, truncated)
  }

  private func windows(of app: AXUIElement) -> [AXUIElement] {
    copyAttribute(app, kAXWindowsAttribute as CFString) as? [AXUIElement] ?? []
  }

  private func children(of element: AXUIElement) -> [AXUIElement] {
    copyAttribute(element, kAXChildrenAttribute as CFString) as? [AXUIElement] ?? []
  }

  private func supportedActions(_ element: AXUIElement, secure: Bool) -> [String] {
    var actions: [String] = []
    var raw: CFArray?
    if AXUIElementCopyActionNames(element, &raw) == .success,
      let names = raw as? [String],
      names.contains(kAXPressAction as String)
    {
      actions.append(ElementAction.press.rawValue)
    }

    if !secure {
      var settable = DarwinBoolean(false)
      if AXUIElementIsAttributeSettable(
        element,
        kAXValueAttribute as CFString,
        &settable
      ) == .success, settable.boolValue {
        actions.append(ElementAction.setValue.rawValue)
      }
    }
    return actions
  }

  private func copyAttribute(_ element: AXUIElement, _ attribute: CFString) -> Any? {
    var raw: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, attribute, &raw) == .success else {
      return nil
    }
    return raw
  }

  private func attributeString(_ element: AXUIElement, _ attribute: CFString) -> String? {
    copyAttribute(element, attribute) as? String
  }

  private func attributeBool(_ element: AXUIElement, _ attribute: CFString) -> Bool? {
    if let value = copyAttribute(element, attribute) as? Bool {
      return value
    }
    return nil
  }

  private func windowID(of window: AXUIElement) -> UInt32? {
    let value = copyAttribute(window, "AXWindowNumber" as CFString)
    if let number = value as? NSNumber {
      return number.uint32Value
    }
    if let number = value as? UInt32 {
      return number
    }
    return nil
  }

  private func frame(of element: AXUIElement) -> FrameSnapshot? {
    guard let positionValue = copyAttribute(element, kAXPositionAttribute as CFString),
      let sizeValue = copyAttribute(element, kAXSizeAttribute as CFString),
      CFGetTypeID(positionValue as CFTypeRef) == AXValueGetTypeID(),
      CFGetTypeID(sizeValue as CFTypeRef) == AXValueGetTypeID()
    else {
      return nil
    }
    let positionAX = unsafeBitCast(positionValue as CFTypeRef, to: AXValue.self)
    let sizeAX = unsafeBitCast(sizeValue as CFTypeRef, to: AXValue.self)
    guard AXValueGetType(positionAX) == .cgPoint,
      AXValueGetType(sizeAX) == .cgSize
    else {
      return nil
    }
    var position = CGPoint.zero
    var size = CGSize.zero
    guard AXValueGetValue(positionAX, .cgPoint, &position),
      AXValueGetValue(sizeAX, .cgSize, &size)
    else {
      return nil
    }
    return FrameSnapshot(
      x: position.x,
      y: position.y,
      width: size.width,
      height: size.height
    )
  }
}
