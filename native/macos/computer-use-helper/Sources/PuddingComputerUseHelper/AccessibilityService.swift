import AppKit
import ApplicationServices
import Foundation

enum SubmitDispatch: Equatable {
  case accessibilityConfirm
  case returnKey
}

enum SubmitPolicy {
  static func resolve(
    secure: Bool,
    enabled: Bool?,
    focused: Bool?,
    editableText: Bool,
    supportsAccessibilityConfirm: Bool
  ) -> SubmitDispatch? {
    guard !secure, enabled == true, focused == true, editableText else {
      return nil
    }
    return supportsAccessibilityConfirm ? .accessibilityConfirm : .returnKey
  }
}

final class AccessibilityService {
  @discardableResult
  static func enableElectronAccessibility(pid: pid_t, application: AXUIElement) throws -> Bool {
    guard let url = NSRunningApplication(processIdentifier: pid)?.bundleURL,
      FileManager.default.fileExists(
        atPath: url.appendingPathComponent("Contents/Frameworks/Electron Framework.framework").path)
    else { return false }
    var current: CFTypeRef?
    if AXUIElementCopyAttributeValue(application, "AXManualAccessibility" as CFString, &current)
      == .success, current as? Bool == true
    {
      return false
    }
    let result = AXUIElementSetAttributeValue(
      application, "AXManualAccessibility" as CFString, kCFBooleanTrue)
    guard result == .success else {
      throw HelperError.elementNotActionable(
        "Electron accessibility activation failed: \(result.rawValue)")
    }
    return true
  }

  static func uniqueTextRange(text: String, selection: String) throws -> NSRange {
    let source = text as NSString
    let found = source.range(of: selection)
    guard !selection.isEmpty, found.location != NSNotFound else {
      throw HelperError.elementNotActionable("selected text was not found")
    }
    let remaining = NSRange(
      location: found.location + 1, length: source.length - found.location - 1)
    guard source.range(of: selection, range: remaining).location == NSNotFound else {
      throw HelperError.ambiguousElement(
        "select_text matches more than once; supply a unique text span")
    }
    return found
  }

  private struct ElementRecord {
    let element: AXUIElement
    let parent: AXUIElement?
    let elementID: String
    let role: String?
    let subrole: String?
    let label: String?
    let identityStable: Bool

    var secure: Bool { subrole == (kAXSecureTextFieldSubrole as String) }
  }

  private struct ElementPosition {
    let element: AXUIElement
    let parent: AXUIElement?
    let path: [Int]
    let parentID: String?
    let identityStable: Bool
  }

  private struct ObservationChild {
    let element: AXUIElement
    let pathIndex: Int
    let identityStable: Bool
  }

  func observe(
    bundleID: String,
    windowID: UInt32,
    targetWindow: CapturableWindowSnapshot,
    maxElements: Int
  ) async throws -> ObservationSnapshot {
    try requireAccessibility()
    let app = try runningApplication(bundleID: bundleID, pid: targetWindow.pid)
    let collection = try await collectElements(
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
      elements: collection.records.map { snapshot(for: $0, windowID: windowID) }
    )
  }

  func act(
    bundleID: String,
    windowID: UInt32,
    targetWindow: CapturableWindowSnapshot,
    elementID: String,
    action: ElementAction,
    value: String?
  ) async throws -> ActionSnapshot {
    try requireAccessibility()
    let app = try runningApplication(bundleID: bundleID, pid: targetWindow.pid)
    let collection = try await collectElements(
      pid: app.processIdentifier,
      windowID: windowID,
      targetWindow: targetWindow,
      maxElements: 1_000
    )
    let matches = collection.records.filter { $0.elementID == elementID }
    guard let record = matches.first else {
      throw HelperError.elementNotFound(elementID)
    }
    guard matches.count == 1 else {
      throw HelperError.ambiguousElement(elementID)
    }
    guard !record.secure else {
      throw HelperError.elementNotActionable("secure fields cannot be operated")
    }
    guard record.identityStable else {
      throw HelperError.elementNotActionable("the element has no stable identity")
    }

    switch action {
    case .focus:
      guard attributeIsSettable(record.element, kAXFocusedAttribute as CFString) else {
        throw HelperError.elementNotActionable("control cannot receive focus")
      }
      guard
        AXUIElementSetAttributeValue(
          record.element, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success
      else {
        throw HelperError.actionFailed("control rejected focus")
      }
      guard
        try await verifyChange({
          self.attributeBool(record.element, kAXFocusedAttribute as CFString) == true
        })
      else {
        throw HelperError.actionFailed("control did not receive focus")
      }
    case .selectText:
      guard let value, !value.isEmpty,
        let text = copyAttribute(record.element, kAXValueAttribute as CFString) as? String,
        attributeIsSettable(record.element, kAXSelectedTextRangeAttribute as CFString)
      else {
        throw HelperError.elementNotActionable(
          "select_text requires text and a selectable text control")
      }
      let range = try Self.uniqueTextRange(text: text, selection: value)
      var selected = CFRange(location: range.location, length: range.length)
      guard let axRange = AXValueCreate(.cfRange, &selected),
        AXUIElementSetAttributeValue(
          record.element, kAXSelectedTextRangeAttribute as CFString, axRange) == .success
      else {
        throw HelperError.actionFailed("cannot set selected text range")
      }
      guard
        try await verifyChange({
          guard
            let result = self.copyAttribute(
              record.element, kAXSelectedTextRangeAttribute as CFString),
            CFGetTypeID(result as CFTypeRef) == AXValueGetTypeID()
          else { return false }
          var actual = CFRange()
          return AXValueGetValue(
            unsafeBitCast(result as CFTypeRef, to: AXValue.self), .cfRange, &actual)
            && actual.location == selected.location && actual.length == selected.length
        })
      else {
        throw HelperError.actionFailed("selected text range did not match")
      }
    case .press:
      guard actionNames(record.element).contains(kAXPressAction as String) else {
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
        limit: max(500, value.utf8.count)
      )
      guard actual == value else {
        throw HelperError.actionFailed("AXValue verification failed")
      }
    case .select:
      guard
        supportedActions(record.element, parent: record.parent, secure: false)
          .contains(ElementAction.select.rawValue)
      else {
        throw HelperError.elementNotActionable("AX selection is unavailable")
      }
      try select(record)
    case .submit:
      guard
        let dispatch = submitDispatch(
          record.element,
          secure: record.secure
        )
      else {
        throw HelperError.elementNotActionable(
          "submit requires a focused, enabled, editable text control"
        )
      }
      switch dispatch {
      case .accessibilityConfirm:
        let error = AXUIElementPerformAction(record.element, kAXConfirmAction as CFString)
        guard error == .success else {
          throw HelperError.actionFailed("AXConfirm returned \(error.rawValue)")
        }
      case .returnKey:
        try postReturnKey(to: app.processIdentifier)
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

  private func runningApplication(bundleID: String, pid: pid_t) throws -> NSRunningApplication {
    guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated,
      app.bundleIdentifier == bundleID
    else {
      throw HelperError.appNotFound(bundleID)
    }
    guard AppPolicy.allows(bundleID: bundleID, pid: app.processIdentifier) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    return app
  }

  private func verifyChange(_ matches: () -> Bool) async throws -> Bool {
    let deadline = Date().addingTimeInterval(1)
    while !matches(), Date() < deadline { try await Task.sleep(for: .milliseconds(20)) }
    return matches()
  }

  private func collectElements(
    pid: pid_t,
    windowID requestedWindowID: UInt32,
    targetWindow: CapturableWindowSnapshot,
    maxElements: Int
  ) async throws -> (
    windows: [ApplicationWindowSnapshot], records: [ElementRecord], truncated: Bool
  ) {
    let appElement = AXUIElementCreateApplication(pid)
    let enabled = try Self.enableElectronAccessibility(pid: pid, application: appElement)
    var collection = try collectElementsOnce(
      pid: pid, windowID: requestedWindowID, targetWindow: targetWindow, maxElements: maxElements)
    // Electron builds its renderer AX tree asynchronously on first enablement.
    // Re-read state only; never resend an action or keep a separate activation flag.
    if enabled {
      let deadline = Date().addingTimeInterval(2)
      while !collection.records.contains(where: { $0.role == "AXWebArea" }), Date() < deadline {
        try await Task.sleep(for: .milliseconds(50))
        collection = try collectElementsOnce(
          pid: pid, windowID: requestedWindowID, targetWindow: targetWindow,
          maxElements: maxElements)
      }
    }
    return collection
  }

  private func collectElementsOnce(
    pid: pid_t, windowID requestedWindowID: UInt32,
    targetWindow: CapturableWindowSnapshot, maxElements: Int
  ) throws -> (windows: [ApplicationWindowSnapshot], records: [ElementRecord], truncated: Bool) {
    let appElement = AXUIElementCreateApplication(pid)
    let allWindows = windows(of: appElement)
    let candidates = allWindows.enumerated().map { index, window in
      self.windowSnapshot(window, index: index)
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
    let window = allWindows[resolvedIndex]
    let windowSnapshot = ApplicationWindowSnapshot(
      index: 0,
      windowID: windowID(of: window) ?? requestedWindowID,
      title: candidates[resolvedIndex].title,
      frame: candidates[resolvedIndex].frame
    )
    var records: [ElementRecord] = []
    let root = ElementPosition(
      element: window, parent: nil, path: [], parentID: nil, identityStable: true
    )
    let truncated = ElementTraversal.walk(root: root, limit: maxElements) { position in
      let element = position.element
      let role = attributeString(element, kAXRoleAttribute as CFString)
      let subrole = attributeString(element, kAXSubroleAttribute as CFString)
      let identifier = boundedAttributeString(element, kAXIdentifierAttribute as CFString)
      let label = boundedAttributeString(element, kAXTitleAttribute as CFString)
      let elementID = ElementIdentity.make(
        windowIndex: 0, path: position.path, role: role, subrole: subrole,
        identifier: identifier, label: label, parentID: position.parentID
      )
      records.append(
        ElementRecord(
          element: element, parent: position.parent, elementID: elementID,
          role: role, subrole: subrole, label: label, identityStable: position.identityStable
        ))
      return observationChildren(of: element, role: role).map { child in
        ElementPosition(
          element: child.element, parent: element,
          path: position.path + [child.pathIndex], parentID: elementID,
          identityStable: position.identityStable && child.identityStable
        )
      }
    }
    return ([windowSnapshot], records, truncated)
  }

  // Full AX values and presentation attributes are read only for an explicit observe.
  // Actions share the identity traversal, then query capabilities on their target only.
  private func snapshot(for record: ElementRecord, windowID: UInt32) -> ObservedElementSnapshot {
    let element = record.element
    let rawValue = record.secure ? nil : copyAttribute(element, kAXValueAttribute as CFString)
    return ObservedElementSnapshot(
      elementID: record.elementID, windowIndex: 0, windowID: windowID,
      role: record.role, subrole: record.subrole, label: record.label,
      description: boundedAttributeString(element, kAXDescriptionAttribute as CFString),
      value: ValueSanitizer.string(rawValue, secure: record.secure),
      valueTruncated: ValueSanitizer.isTruncated(rawValue, secure: record.secure),
      secure: record.secure,
      enabled: attributeBool(element, kAXEnabledAttribute as CFString),
      focused: attributeBool(element, kAXFocusedAttribute as CFString),
      selected: selectedState(element, parent: record.parent), frame: frame(of: element),
      actions: record.identityStable
        ? supportedActions(element, parent: record.parent, secure: record.secure) : []
    )
  }

  private func windows(of app: AXUIElement) -> [AXUIElement] {
    copyAttribute(app, kAXWindowsAttribute as CFString) as? [AXUIElement] ?? []
  }

  private func children(of element: AXUIElement) -> [AXUIElement] {
    copyAttribute(element, kAXChildrenAttribute as CFString) as? [AXUIElement] ?? []
  }

  private func observationChildren(of element: AXUIElement, role: String?) -> [ObservationChild] {
    if role == (kAXTableRole as String) || role == (kAXOutlineRole as String),
      let rows = elementArrayAttribute(element, kAXVisibleRowsAttribute as CFString)
    {
      let components = ElementIdentity.visibleRowPathComponents(
        indices: rows.map { attributeInt($0, kAXIndexAttribute as CFString) }
      )
      return zip(rows, components).map { row, component in
        ObservationChild(
          element: row,
          pathIndex: component.index,
          identityStable: component.stable
        )
      }
    }
    if let visible = elementArrayAttribute(element, kAXVisibleChildrenAttribute as CFString) {
      return visible.enumerated().map {
        ObservationChild(element: $0.element, pathIndex: $0.offset, identityStable: true)
      }
    }
    return children(of: element).enumerated().map {
      ObservationChild(element: $0.element, pathIndex: $0.offset, identityStable: true)
    }
  }

  private func elementArrayAttribute(
    _ element: AXUIElement,
    _ attribute: CFString
  ) -> [AXUIElement]? {
    var raw: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, attribute, &raw) == .success else {
      return nil
    }
    return raw as? [AXUIElement]
  }

  private func supportedActions(
    _ element: AXUIElement,
    parent: AXUIElement?,
    secure: Bool
  ) -> [String] {
    guard !secure else { return [] }
    var actions: [String] = []
    let names = actionNames(element)
    if names.contains(kAXPressAction as String) {
      actions.append(ElementAction.press.rawValue)
    }

    if attributeIsSettable(element, kAXValueAttribute as CFString) {
      actions.append(ElementAction.setValue.rawValue)
    }
    if attributeIsSettable(element, kAXFocusedAttribute as CFString) {
      actions.append(ElementAction.focus.rawValue)
    }
    if attributeIsSettable(element, kAXSelectedTextRangeAttribute as CFString) {
      actions.append(ElementAction.selectText.rawValue)
    }
    if attributeIsSettable(element, kAXSelectedAttribute as CFString)
      || parent.map({ attributeIsSettable($0, kAXSelectedRowsAttribute as CFString) }) == true
    {
      actions.append(ElementAction.select.rawValue)
    }
    if submitDispatch(element, secure: secure) != nil {
      actions.append(ElementAction.submit.rawValue)
    }
    return actions
  }

  private func submitDispatch(
    _ element: AXUIElement,
    secure: Bool
  ) -> SubmitDispatch? {
    let role = attributeString(element, kAXRoleAttribute as CFString)
    let editableText =
      (role == (kAXTextFieldRole as String)
        || role == (kAXComboBoxRole as String))
      && attributeIsSettable(element, kAXValueAttribute as CFString)
    return SubmitPolicy.resolve(
      secure: secure,
      enabled: attributeBool(element, kAXEnabledAttribute as CFString),
      focused: attributeBool(element, kAXFocusedAttribute as CFString),
      editableText: editableText,
      supportsAccessibilityConfirm: actionNames(element).contains(kAXConfirmAction as String)
    )
  }

  private func actionNames(_ element: AXUIElement) -> [String] {
    var raw: CFArray?
    guard AXUIElementCopyActionNames(element, &raw) == .success else {
      return []
    }
    return raw as? [String] ?? []
  }

  private func postReturnKey(to pid: pid_t) throws {
    guard let source = CGEventSource(stateID: .privateState),
      let keyDown = CGEvent(keyboardEventSource: source, virtualKey: 36, keyDown: true),
      let keyUp = CGEvent(keyboardEventSource: source, virtualKey: 36, keyDown: false)
    else {
      throw HelperError.elementNotActionable("Return key event could not be created")
    }
    keyDown.flags = []
    keyUp.flags = []
    keyDown.setIntegerValueField(.keyboardEventAutorepeat, value: 0)
    keyUp.setIntegerValueField(.keyboardEventAutorepeat, value: 0)
    keyDown.postToPid(pid)
    keyUp.postToPid(pid)
  }

  private func attributeIsSettable(_ element: AXUIElement, _ attribute: CFString) -> Bool {
    var settable = DarwinBoolean(false)
    return AXUIElementIsAttributeSettable(element, attribute, &settable) == .success
      && settable.boolValue
  }

  private func select(_ record: ElementRecord) throws {
    if attributeIsSettable(record.element, kAXSelectedAttribute as CFString) {
      let error = AXUIElementSetAttributeValue(
        record.element,
        kAXSelectedAttribute as CFString,
        kCFBooleanTrue
      )
      guard error == .success else {
        throw HelperError.actionFailed("set AXSelected returned \(error.rawValue)")
      }
      guard attributeBool(record.element, kAXSelectedAttribute as CFString) == true else {
        throw HelperError.actionFailed("AXSelected verification failed")
      }
      return
    }

    guard let parent = record.parent,
      attributeIsSettable(parent, kAXSelectedRowsAttribute as CFString)
    else {
      throw HelperError.elementNotActionable("AX selection is unavailable")
    }
    let rows = [record.element] as CFArray
    let error = AXUIElementSetAttributeValue(
      parent,
      kAXSelectedRowsAttribute as CFString,
      rows
    )
    guard error == .success else {
      throw HelperError.actionFailed("set AXSelectedRows returned \(error.rawValue)")
    }
    guard selectedRows(parent).contains(where: { CFEqual($0, record.element) }) else {
      throw HelperError.actionFailed("AXSelectedRows verification failed")
    }
  }

  private func selectedState(_ element: AXUIElement, parent: AXUIElement?) -> Bool? {
    if let selected = attributeBool(element, kAXSelectedAttribute as CFString) {
      return selected
    }
    guard let parent,
      elementArrayAttribute(parent, kAXSelectedRowsAttribute as CFString) != nil
    else {
      return nil
    }
    return selectedRows(parent).contains(where: { CFEqual($0, element) })
  }

  private func selectedRows(_ element: AXUIElement) -> [AXUIElement] {
    elementArrayAttribute(element, kAXSelectedRowsAttribute as CFString) ?? []
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

  private func boundedAttributeString(
    _ element: AXUIElement,
    _ attribute: CFString,
    limit: Int = 96
  ) -> String? {
    ValueSanitizer.bounded(attributeString(element, attribute), limit: limit)
  }

  private func attributeBool(_ element: AXUIElement, _ attribute: CFString) -> Bool? {
    if let value = copyAttribute(element, attribute) as? Bool {
      return value
    }
    return nil
  }

  private func attributeInt(_ element: AXUIElement, _ attribute: CFString) -> Int? {
    if let value = copyAttribute(element, attribute) as? NSNumber {
      return value.intValue
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

  func matchesWindow(_ window: AXUIElement, target: CapturableWindowSnapshot) -> Bool {
    WindowResolver.resolveIndex(
      requestedWindowID: target.windowID, targetWindow: target,
      candidates: [windowSnapshot(window, index: 0)]) == 0
  }

  private func windowSnapshot(_ window: AXUIElement, index: Int) -> ApplicationWindowSnapshot {
    ApplicationWindowSnapshot(
      index: index, windowID: windowID(of: window),
      title: boundedAttributeString(window, kAXTitleAttribute as CFString), frame: frame(of: window)
    )
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
