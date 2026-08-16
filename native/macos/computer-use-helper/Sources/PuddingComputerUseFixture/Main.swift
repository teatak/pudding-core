import AppKit

private let fixtureBundleID = "com.teatak.pudding.computer-use-fixture"

final class PointerGestureView: NSView {
  var report: ((String) -> Void)?
  private var dragging = false

  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

  override func mouseDown(with event: NSEvent) {
    dragging = false
    report?(event.clickCount >= 2 ? "double click" : "left click")
  }

  override func rightMouseDown(with event: NSEvent) {
    report?("right click")
  }

  override func mouseDragged(with event: NSEvent) {
    dragging = true
    report?("dragged")
  }

  override func mouseUp(with event: NSEvent) {
    guard dragging else { return }
    dragging = false
    let location = convert(event.locationInWindow, from: nil)
    report?(location.x >= bounds.midX ? "drag released right" : "drag released left")
  }

  override func scrollWheel(with event: NSEvent) {
    if event.scrollingDeltaY < 0 {
      report?("scrolled down")
    } else if event.scrollingDeltaY > 0 {
      report?("scrolled up")
    } else {
      report?(event.scrollingDeltaX < 0 ? "scrolled right" : "scrolled left")
    }
  }
}

@MainActor
final class FixtureAppDelegate: NSObject, NSApplicationDelegate, NSTableViewDataSource,
  NSTableViewDelegate
{
  private var primaryWindow: NSWindow?
  private var secondaryWindow: NSWindow?
  private var count = 0
  private let countValue = NSTextField(labelWithString: "0")
  private let confirmedValue = NSTextField(labelWithString: "not confirmed")
  private let selectedValue = NSTextField(labelWithString: "no row selected")
  private let pointerValue = NSTextField(labelWithString: "no pointer action")
  private let rowCount = 500
  private weak var tableView: NSTableView?

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

  @objc private func confirmInput(_ sender: NSTextField) {
    confirmedValue.stringValue = sender.stringValue
    tableView?.scrollRowToVisible(250)
  }

  func numberOfRows(in tableView: NSTableView) -> Int {
    rowCount
  }

  func tableView(
    _ tableView: NSTableView,
    viewFor tableColumn: NSTableColumn?,
    row: Int
  ) -> NSView? {
    let identifier = NSUserInterfaceItemIdentifier("fixture.table-cell")
    let field = tableView.makeView(withIdentifier: identifier, owner: self) as? NSTextField
      ?? NSTextField(labelWithString: "")
    field.identifier = identifier
    field.stringValue = "Deterministic row \(row + 1)"
    field.setAccessibilityIdentifier("fixture.row.\(row + 1)")
    field.setAccessibilityLabel("Fixture row \(row + 1)")
    return field
  }

  func tableViewSelectionDidChange(_ notification: Notification) {
    guard let tableView = notification.object as? NSTableView else { return }
    selectedValue.stringValue = tableView.selectedRow >= 0
      ? "selected row \(tableView.selectedRow + 1)"
      : "no row selected"
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
    input.target = self
    input.action = #selector(confirmInput(_:))
    content.addSubview(input)
    window.initialFirstResponder = input

    confirmedValue.frame = NSRect(x: 160, y: 382, width: 320, height: 18)
    identify(confirmedValue, id: "fixture.confirmed-value", label: "Fixture confirmed value")
    content.addSubview(confirmedValue)

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

    selectedValue.frame = NSRect(x: 24, y: 238, width: 200, height: 18)
    identify(selectedValue, id: "fixture.selected-value", label: "Fixture selected value")
    content.addSubview(selectedValue)

    let pointerTarget = PointerGestureView(frame: NSRect(x: 228, y: 252, width: 252, height: 36))
    pointerTarget.wantsLayer = true
    pointerTarget.layer?.backgroundColor = NSColor.controlAccentColor.withAlphaComponent(0.15).cgColor
    pointerTarget.layer?.cornerRadius = 8
    pointerTarget.setAccessibilityElement(true)
    pointerTarget.setAccessibilityRole(.group)
    identify(pointerTarget, id: "fixture.pointer-target", label: "Fixture pointer target")
    pointerTarget.report = { [weak self] value in
      self?.pointerValue.stringValue = value
    }
    content.addSubview(pointerTarget)

    pointerValue.frame = NSRect(x: 228, y: 234, width: 252, height: 18)
    identify(pointerValue, id: "fixture.pointer-value", label: "Fixture pointer value")
    content.addSubview(pointerValue)

    let scrollView = NSScrollView(frame: NSRect(x: 24, y: 24, width: 456, height: 210))
    scrollView.hasVerticalScroller = true
    scrollView.borderType = .bezelBorder
    identify(scrollView, id: "fixture.scroll", label: "Fixture scroll area")
    let table = NSTableView(frame: scrollView.bounds)
    let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("fixture.table-column"))
    column.title = "Rows"
    column.width = 430
    table.addTableColumn(column)
    table.headerView = nil
    table.rowHeight = 24
    table.allowsMultipleSelection = false
    table.dataSource = self
    table.delegate = self
    tableView = table
    identify(table, id: "fixture.table", label: "Fixture table")
    scrollView.documentView = table
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
