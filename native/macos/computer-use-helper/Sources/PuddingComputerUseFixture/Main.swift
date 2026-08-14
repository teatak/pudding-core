import AppKit

private let fixtureBundleID = "com.teatak.pudding.computer-use-fixture"

@MainActor
final class FixtureAppDelegate: NSObject, NSApplicationDelegate {
  private var primaryWindow: NSWindow?
  private var secondaryWindow: NSWindow?
  private var count = 0
  private let countValue = NSTextField(labelWithString: "0")

  func applicationDidFinishLaunching(_ notification: Notification) {
    guard Bundle.main.bundleIdentifier == fixtureBundleID else {
      NSApp.terminate(nil)
      return
    }
    primaryWindow = makePrimaryWindow()
    secondaryWindow = makeSecondaryWindow()
    primaryWindow?.makeKeyAndOrderFront(nil)
    secondaryWindow?.orderFront(nil)
  }

  func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    false
  }

  func applicationShouldHandleReopen(
    _ sender: NSApplication,
    hasVisibleWindows flag: Bool
  ) -> Bool {
    guard !flag else { return true }
    primaryWindow?.makeKeyAndOrderFront(nil)
    secondaryWindow?.orderFront(nil)
    return true
  }

  @objc private func increment(_ sender: NSButton) {
    count += 1
    countValue.stringValue = String(count)
  }

  @objc private func closeWindows(_ sender: NSButton) {
    primaryWindow?.close()
    secondaryWindow?.close()
  }

  private func makePrimaryWindow() -> NSWindow {
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 520, height: 500),
      styleMask: [.titled, .closable, .miniaturizable, .resizable],
      backing: .buffered,
      defer: false
    )
    window.title = "Computer Use Fixture"
    window.center()
    window.isReleasedWhenClosed = false

    let content = NSView(frame: NSRect(x: 0, y: 0, width: 520, height: 500))
    window.contentView = content

    let heading = NSTextField(labelWithString: "Deterministic Computer Use Fixture")
    heading.frame = NSRect(x: 24, y: 448, width: 420, height: 24)
    heading.font = .boldSystemFont(ofSize: 17)
    identify(heading, id: "fixture.heading", label: "Fixture heading")
    content.addSubview(heading)

    let inputLabel = NSTextField(labelWithString: "Editable value")
    inputLabel.frame = NSRect(x: 24, y: 405, width: 130, height: 22)
    content.addSubview(inputLabel)

    let input = NSTextField(string: "initial")
    input.frame = NSRect(x: 160, y: 401, width: 320, height: 28)
    identify(input, id: "fixture.text-input", label: "Fixture text input")
    content.addSubview(input)

    let secureLabel = NSTextField(labelWithString: "Secure value")
    secureLabel.frame = NSRect(x: 24, y: 360, width: 130, height: 22)
    content.addSubview(secureLabel)

    let secureInput = NSSecureTextField(string: "fixture-secret")
    secureInput.frame = NSRect(x: 160, y: 356, width: 320, height: 28)
    identify(secureInput, id: "fixture.secure-input", label: "Fixture secure input")
    content.addSubview(secureInput)

    let increment = NSButton(title: "Increment", target: self, action: #selector(increment(_:)))
    increment.frame = NSRect(x: 24, y: 306, width: 120, height: 32)
    increment.bezelStyle = .rounded
    identify(increment, id: "fixture.increment", label: "Increment")
    content.addSubview(increment)

    let countLabel = NSTextField(labelWithString: "Count")
    countLabel.frame = NSRect(x: 170, y: 311, width: 55, height: 22)
    content.addSubview(countLabel)

    countValue.frame = NSRect(x: 228, y: 311, width: 60, height: 22)
    identify(countValue, id: "fixture.count", label: "Fixture count")
    content.addSubview(countValue)

    let closeWindows = NSButton(
      title: "Close Windows",
      target: self,
      action: #selector(closeWindows(_:))
    )
    closeWindows.frame = NSRect(x: 330, y: 306, width: 150, height: 32)
    closeWindows.bezelStyle = .rounded
    identify(closeWindows, id: "fixture.close-windows", label: "Close Windows")
    content.addSubview(closeWindows)

    let checkbox = NSButton(checkboxWithTitle: "Fixture checkbox", target: nil, action: nil)
    checkbox.frame = NSRect(x: 24, y: 260, width: 180, height: 24)
    identify(checkbox, id: "fixture.checkbox", label: "Fixture checkbox")
    content.addSubview(checkbox)

    let scrollView = NSScrollView(frame: NSRect(x: 24, y: 24, width: 456, height: 210))
    scrollView.hasVerticalScroller = true
    scrollView.borderType = .bezelBorder
    identify(scrollView, id: "fixture.scroll", label: "Fixture scroll area")
    let document = NSView(frame: NSRect(x: 0, y: 0, width: 430, height: 520))
    for index in 0..<20 {
      let row = NSTextField(labelWithString: "Deterministic row \(index + 1)")
      row.frame = NSRect(x: 12, y: 488 - (index * 24), width: 300, height: 20)
      identify(row, id: "fixture.row.\(index + 1)", label: "Fixture row \(index + 1)")
      document.addSubview(row)
    }
    scrollView.documentView = document
    content.addSubview(scrollView)
    return window
  }

  private func makeSecondaryWindow() -> NSWindow {
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 360, height: 180),
      styleMask: [.titled, .closable, .miniaturizable, .resizable],
      backing: .buffered,
      defer: false
    )
    window.title = "Computer Use Fixture Secondary"
    window.setFrameOrigin(NSPoint(x: 120, y: 120))
    window.isReleasedWhenClosed = false
    let label = NSTextField(labelWithString: "Second deterministic window")
    label.frame = NSRect(x: 24, y: 78, width: 280, height: 24)
    identify(label, id: "fixture.secondary-label", label: "Fixture secondary window")
    window.contentView?.addSubview(label)
    return window
  }

  private func identify(_ view: NSView, id: String, label: String) {
    view.identifier = NSUserInterfaceItemIdentifier(id)
    view.setAccessibilityIdentifier(id)
    view.setAccessibilityLabel(label)
  }
}

@main
struct PuddingComputerUseFixture {
  @MainActor
  static func main() {
    let application = NSApplication.shared
    let delegate = FixtureAppDelegate()
    application.setActivationPolicy(.regular)
    application.delegate = delegate
    application.run()
    withExtendedLifetime(delegate) {}
  }
}
