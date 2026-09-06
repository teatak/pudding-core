import AppKit
import ApplicationServices
import Foundation

struct KeyboardInput: Equatable {
  let action: String
  let key: String?
  let modifiers: [String]
  let value: String?

  static let keyCodes: [String: CGKeyCode] = [
    "a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
    "b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19,
    "3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28,
    "0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "enter": 36, "l": 37,
    "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42, ",": 43, "/": 44, "n": 45, "m": 46,
    ".": 47, "tab": 48, "space": 49, "`": 50, "backspace": 51, "escape": 53,
    "home": 115, "pageup": 116, "delete": 117, "end": 119, "pagedown": 121,
    "left": 123, "right": 124, "down": 125, "up": 126,
    "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98,
    "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
  ]
  static let modifierFlags: [String: CGEventFlags] = [
    "command": .maskCommand, "shift": .maskShift, "option": .maskAlternate, "control": .maskControl,
  ]

  static func validated(action: String, key: String?, modifiers: [String]?, value: String?) throws
    -> KeyboardInput
  {
    let modifiers = modifiers ?? []
    guard Set(modifiers).count == modifiers.count,
      modifiers.allSatisfy({ modifierFlags[$0] != nil })
    else {
      throw ArgumentError.invalidOption("modifiers", "use unique command, shift, option, control")
    }
    if action == "press_key" {
      guard let key, keyCodes[key.lowercased()] != nil, value == nil else {
        throw ArgumentError.invalidOption("key", "press_key requires a supported key and no value")
      }
      return KeyboardInput(action: action, key: key.lowercased(), modifiers: modifiers, value: nil)
    }
    guard ["type_text", "paste"].contains(action), key == nil, modifiers.isEmpty,
      let value, !value.isEmpty, value.unicodeScalars.count <= 20_000
    else {
      throw ArgumentError.invalidOption(
        "value", "type_text/paste require 1–20000 characters and no key/modifiers")
    }
    return KeyboardInput(action: action, key: nil, modifiers: [], value: value)
  }
}

final class KeyboardService {
  func perform(bundleID: String, target: CapturableWindowSnapshot, input: KeyboardInput) throws
    -> KeyboardSnapshot
  {
    guard AXIsProcessTrusted() else { throw HelperError.permissionRequired("accessibility") }
    guard AppPolicy.allows(bundleID: bundleID, pid: target.pid) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    let application = AXUIElementCreateApplication(target.pid)
    try AccessibilityService.enableElectronAccessibility(pid: target.pid, application: application)
    try validateTarget(
      bundleID: bundleID, target: target, application: application,
      editable: input.action != "press_key")
    guard let source = CGEventSource(stateID: .privateState) else {
      throw HelperError.actionFailed("cannot create keyboard event source")
    }
    switch input.action {
    case "press_key":
      let flags = input.modifiers.reduce(CGEventFlags()) {
        $0.union(KeyboardInput.modifierFlags[$1]!)
      }
      try postKey(
        source: source, pid: target.pid, key: KeyboardInput.keyCodes[input.key!]!, flags: flags)
    case "type_text":
      // Committed Unicode text is separate from physical keys, which use the current IME.
      var emitted = false
      for character in input.value! {
        do {
          try validateTarget(
            bundleID: bundleID, target: target, application: application, editable: true)
        } catch {
          if emitted {
            throw HelperError.actionFailed(
              "text input interrupted after partial delivery: \(error.localizedDescription)")
          }
          throw error
        }
        let units = Array(String(character).utf16)
        for down in [true, false] {
          guard let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: down)
          else {
            throw HelperError.actionFailed("cannot create text event")
          }
          event.flags = []
          event.keyboardSetUnicodeString(stringLength: units.count, unicodeString: units)
          event.postToPid(target.pid)
          emitted = true
        }
      }
    case "paste":
      // This explicit clipboard operation leaves the supplied value in the clipboard.
      NSPasteboard.general.clearContents()
      guard NSPasteboard.general.setString(input.value!, forType: .string) else {
        throw HelperError.actionFailed("cannot write clipboard text")
      }
      try postKey(source: source, pid: target.pid, key: 9, flags: .maskCommand)
    default: throw ArgumentError.invalidOption("action", input.action)
    }
    return KeyboardSnapshot(
      bundleID: bundleID, elementID: "", action: input.action, completed: true, key: input.key,
      modifiers: input.modifiers)
  }

  private func validateTarget(
    bundleID: String, target: CapturableWindowSnapshot, application: AXUIElement, editable: Bool
  ) throws {
    guard let app = NSRunningApplication(processIdentifier: target.pid), !app.isTerminated,
      app.bundleIdentifier == bundleID
    else {
      throw HelperError.appNotFound(bundleID)
    }
    guard NSWorkspace.shared.frontmostApplication?.processIdentifier == target.pid else {
      throw HelperError.appNotForeground(
        bundleID, NSWorkspace.shared.frontmostApplication?.localizedName ?? "unknown")
    }
    guard let focusedWindow = elementAttribute(application, kAXFocusedWindowAttribute),
      let focused = elementAttribute(application, kAXFocusedUIElementAttribute)
    else {
      throw HelperError.elementNotActionable("keyboard input requires a focused control and window")
    }
    guard AccessibilityService().matchesWindow(focusedWindow, target: target) else {
      throw HelperError.windowNotFound(target.windowID)
    }
    guard attribute(focused, kAXSubroleAttribute) as? String != kAXSecureTextFieldSubrole as String
    else {
      throw HelperError.elementNotActionable("secure fields cannot be operated")
    }
    if editable {
      let role = attribute(focused, kAXRoleAttribute) as? String ?? ""
      guard
        [kAXTextFieldRole as String, kAXTextAreaRole as String, kAXComboBoxRole as String].contains(
          role),
        (attribute(focused, kAXEnabledAttribute) as? Bool) == true
      else {
        throw HelperError.elementNotActionable("text input requires a focused enabled text control")
      }
    }
  }

  private func postKey(source: CGEventSource, pid: pid_t, key: CGKeyCode, flags: CGEventFlags)
    throws
  {
    guard let down = CGEvent(keyboardEventSource: source, virtualKey: key, keyDown: true),
      let up = CGEvent(keyboardEventSource: source, virtualKey: key, keyDown: false)
    else {
      throw HelperError.actionFailed("cannot create key events")
    }
    down.flags = flags
    up.flags = flags
    down.postToPid(pid)
    up.postToPid(pid)
  }

  private func elementAttribute(_ element: AXUIElement, _ name: String) -> AXUIElement? {
    guard let value = attribute(element, name),
      CFGetTypeID(value as CFTypeRef) == AXUIElementGetTypeID()
    else { return nil }
    return unsafeBitCast(value as CFTypeRef, to: AXUIElement.self)
  }

  private func attribute(_ element: AXUIElement, _ name: String) -> Any? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else {
      return nil
    }
    return value
  }
}

struct KeyboardSnapshot: Encodable {
  let bundleID: String
  let elementID: String
  let action: String
  let completed: Bool
  let key: String?
  let modifiers: [String]
}
