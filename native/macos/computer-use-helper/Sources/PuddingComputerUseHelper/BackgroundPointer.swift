// Explicit background pointer delivery. No global input, real activation, observation
// tokens, or fallback. The caller and a same-executable worker share one journal;
// only the surviving owner may release input after the other has exited.
import AppKit
import ApplicationServices
import Darwin
import Foundation

struct BackgroundInputError: Error, LocalizedError {
  let detail: ErrorDetail
  init(_ message: String, outcome: ActionOutcome = .notStarted,
       code: String = "computer_background_unavailable") {
    detail = ErrorDetail(code: code, message: String(message.prefix(1_024)), permission: nil,
      retryable: false, outcome: outcome)
  }
  init(detail: ErrorDetail) { self.detail = detail }
  var errorDescription: String? { detail.message }
}

struct BackgroundProcessStamp: Codable, Equatable {
  let seconds: UInt64
  let microseconds: UInt64
  static func read(_ pid: pid_t) throws -> Self {
    var info = proc_bsdinfo()
    let size = Int32(MemoryLayout<proc_bsdinfo>.size)
    guard proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size else {
      throw BackgroundInputError("Target process identity is unavailable")
    }
    return Self(seconds: info.pbi_start_tvsec, microseconds: info.pbi_start_tvusec)
  }

  static func executablePath(_ pid: pid_t) throws -> String {
    // proc_info.h defines PROC_PIDPATHINFO_MAXSIZE as 4 * MAXPATHLEN;
    // the compound macro is not imported by Swift.
    var buffer = [UInt8](repeating: 0, count: 4 * Int(MAXPATHLEN))
    guard proc_pidpath(pid, &buffer, UInt32(buffer.count)) > 0 else {
      throw BackgroundInputError("Target executable identity is unavailable")
    }
    let path = String(decoding: buffer.prefix(while: { $0 != 0 }), as: UTF8.self)
    return URL(fileURLWithPath: path).resolvingSymlinksInPath().path
  }

  func matches(pid: pid_t, executable: String) throws -> Bool {
    do {
      guard try Self.read(pid) == self else { return false }
      return try Self.executablePath(pid) == URL(fileURLWithPath: executable).resolvingSymlinksInPath().path
    } catch {
      // Missing app metadata is not proof of process exit. Only ESRCH permits
      // abandoning recovery; other identity failures keep the journal unresolved.
      if kill(pid, 0) != 0 && errno == ESRCH { return false }
      throw error
    }
  }
}

struct BackgroundPointerRequest: Codable {
  let bundleID: String
  let pid: pid_t
  let stamp: BackgroundProcessStamp
  let executable: String
  let windowID: UInt32
  let frame: FrameSnapshot
  let input: PointerInput
  let foregroundPID: pid_t

  func validate() throws {
    guard pid > 0, windowID > 0, foregroundPID > 0,
      !bundleID.isEmpty, executable.hasPrefix("/"),
      AppPolicy.allows(bundleID: bundleID, pid: pid),
      frame.x.isFinite, frame.y.isFinite, frame.width.isFinite, frame.height.isFinite else {
      throw BackgroundInputError("Invalid background target identity or window geometry")
    }
    let validated = try PointerInput.validated(action: input.action, x: input.x, y: input.y,
      toX: input.toX, toY: input.toY, button: input.button, clickCount: input.clickCount,
      deltaX: input.deltaX, deltaY: input.deltaY, delivery: input.delivery)
    guard input.delivery == "background", validated == input else {
      throw BackgroundInputError("Invalid background pointer parameters")
    }
    _ = try PointerCoordinatePolicy.globalPoint(frame: frame, x: input.x, y: input.y)
    if let x = input.toX, let y = input.toY {
      _ = try PointerCoordinatePolicy.globalPoint(frame: frame, x: x, y: y)
    }
  }

  func sameProcess() throws -> Bool {
    // The app/window binding was resolved before creating this request. During
    // delivery/recovery, use kernel identity instead of NSRunningApplication's
    // transient metadata (which can misreport an unchanged, live target).
    try stamp.matches(pid: pid, executable: executable)
  }

  func currentFrame(includeOffscreen: Bool = false,
                    copyWindowInfo: (CGWindowListOption, CGWindowID) -> CFArray? = CGWindowListCopyWindowInfo) throws -> CGRect? {
    // One authoritative window lookup, including hidden/minimized windows.
    // Visibility is required for new input, not for releasing a held button.
    guard let windows = copyWindowInfo([.optionAll, .excludeDesktopElements], kCGNullWindowID)
      as? [[String: Any]] else {
      throw BackgroundInputError("Target window state is unavailable")
    }
    guard let info = windows.first(where: {
      ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == windowID
    }) else { return nil }
    guard let owner = info[kCGWindowOwnerPID as String] as? NSNumber else {
      throw BackgroundInputError("Target window owner is unavailable")
    }
    guard owner.int32Value == pid else { return nil }
    guard let bounds = info[kCGWindowBounds as String] as? NSDictionary,
      let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary),
      let layer = info[kCGWindowLayer as String] as? NSNumber else {
      throw BackgroundInputError("Target window geometry is unavailable")
    }
    // WindowServer omits kCGWindowIsOnscreen for hidden windows.
    let onScreen = (info[kCGWindowIsOnscreen as String] as? NSNumber)?.boolValue == true
    return includeOffscreen || (onScreen && layer.intValue == 0) ? frame : nil
  }

  func check() throws {
    guard AXIsProcessTrusted(), CGPreflightPostEventAccess() else {
      throw HelperError.permissionRequired("accessibility")
    }
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    guard try sameProcess(), try currentFrame() == CGRect(x: frame.x, y: frame.y, width: frame.width, height: frame.height) else {
      throw BackgroundInputError("Target process or window geometry changed; no further input sent")
    }
    guard NSWorkspace.shared.frontmostApplication?.processIdentifier == foregroundPID else {
      throw BackgroundInputError("Foreground changed during background delivery; do not replay or activate automatically")
    }
  }
}

enum BackgroundInputPhase: UInt8 { case idle, activated, pressed, released }

final class BackgroundInputJournal {
  static var directory: URL {
    URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("pudding-computer-input")
  }
  let descriptor: Int32
  init(descriptor: Int32) { self.descriptor = descriptor }
  // No LOCK_UN: inherited descriptors must retain ownership after caller death.
  deinit { close(descriptor) }

  func phase() throws -> BackgroundInputPhase {
    try checkpoint().phase
  }
  func checkpoint() throws -> (phase: BackgroundInputPhase, step: Int) {
    var byte: UInt8 = 255
    guard pread(descriptor, &byte, 1, 0) == 1,
      let phase = BackgroundInputPhase(rawValue: byte & 3),
      phase == .pressed || byte <= 3 else {
      throw BackgroundInputError("Invalid input recovery journal; no further input allowed", outcome: .unknown)
    }
    return (phase, Int(byte >> 2))
  }
  func set(_ phase: BackgroundInputPhase, step: Int = 0) throws {
    // One atomic byte: low two bits are the phase, high six identify the
    // button/position to release. Old non-idle journals remain rejected at acquire.
    guard (0...63).contains(step), phase == .pressed || step == 0 else {
      throw BackgroundInputError("Invalid input recovery step", outcome: .unknown)
    }
    var byte = phase.rawValue | UInt8(step << 2)
    guard pwrite(descriptor, &byte, 1, 0) == 1 else {
      throw BackgroundInputError("Could not record input recovery phase", outcome: .unknown)
    }
  }
  static func acquire(pid: pid_t, stamp: BackgroundProcessStamp? = nil,
                      directory: URL = BackgroundInputJournal.directory) throws -> BackgroundInputJournal {
    let stamp = try stamp ?? BackgroundProcessStamp.read(pid)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
      attributes: [.posixPermissions: 0o700])
    var directoryInfo = stat()
    guard lstat(directory.path, &directoryInfo) == 0, directoryInfo.st_uid == getuid(),
      directoryInfo.st_mode & S_IFMT == S_IFDIR, directoryInfo.st_mode & 0o077 == 0 else {
      throw BackgroundInputError("Invalid input ownership directory")
    }
    let path = directory.appendingPathComponent("\(pid)-\(stamp.seconds)-\(stamp.microseconds).state").path
    let fd = open(path, O_RDWR | O_CREAT | O_CLOEXEC | O_NOFOLLOW, S_IRUSR | S_IWUSR)
    guard fd >= 0 else { throw BackgroundInputError("Cannot open input ownership journal") }
    let journal = BackgroundInputJournal(descriptor: fd)
    try journal.checkFile()
    guard flock(fd, LOCK_EX | LOCK_NB) == 0 else {
      throw BackgroundInputError("Target input is still owned by another operation; no input sent", code: "computer_input_busy")
    }
    var info = stat()
    guard fstat(fd, &info) == 0, info.st_size == 0 || info.st_size == 1 else {
      throw BackgroundInputError("Invalid input journal size")
    }
    if info.st_size == 0 { try journal.set(.idle) }
    guard try journal.phase() == .idle else {
      throw BackgroundInputError("Unresolved input from a prior owner; no input sent. Restart the target app before continuing")
    }
    return journal
  }
  func checkFile() throws {
    var info = stat()
    guard fstat(descriptor, &info) == 0, info.st_uid == getuid(), info.st_nlink == 1,
      info.st_mode & S_IFMT == S_IFREG, info.st_mode & 0o077 == 0 else {
      throw BackgroundInputError("Invalid shared input journal")
    }
  }
}

// A testable transaction; posting success does not assert a UI effect. Recovery
// up may itself finish a click, so every interrupted transaction is uncertain.
enum BackgroundPointerPost: Equatable { case activate, step(Int), release(Int), deactivate }

final class BackgroundPointerTransaction {
  let journal: BackgroundInputJournal
  let events: BackgroundPointerEvents
  let check: () throws -> Void
  let post: (BackgroundPointerPost) throws -> Void
  let wait: (Double) -> Void
  let sameProcess: () throws -> Bool
  let windowExists: () throws -> Bool
  let targetIsForeground: () -> Bool

  init(journal: BackgroundInputJournal, events: BackgroundPointerEvents, check: @escaping () throws -> Void,
       post: @escaping (BackgroundPointerPost) throws -> Void,
       wait: @escaping (Double) -> Void, sameProcess: @escaping () throws -> Bool,
       windowExists: @escaping () throws -> Bool, targetIsForeground: @escaping () -> Bool) {
    self.journal = journal; self.events = events; self.check = check; self.post = post; self.wait = wait
    self.sameProcess = sameProcess; self.windowExists = windowExists; self.targetIsForeground = targetIsForeground
  }

  func run() throws {
    try check()
    do {
      try journal.set(.activated); try post(.activate); wait(0.1)
      try check()
      for (index, step) in events.steps.enumerated() {
        if step.release != nil { try journal.set(.pressed, step: index) }
        try post(.step(index))
        if step.release == nil { try journal.set(.released) }
        wait(step.delay)
        try check()
      }
    } catch {
      // Even activation may have affected target internal state. Never replay.
      do { try recover() } catch {
        throw BackgroundInputError("Background input recovery failed; no replay: \(error.localizedDescription)", outcome: .unknown)
      }
      throw BackgroundInputError("Background input interrupted; effect may be partial; no replay: \(error.localizedDescription)", outcome: .unknown)
    }
    do { try recover() }
    catch { throw BackgroundInputError("Background input recovery failed; no replay: \(error.localizedDescription)", outcome: .unknown) }
  }

  func recover() throws {
    let checkpoint = try journal.checkpoint()
    guard checkpoint.phase != .idle else { return }
    if try sameProcess() {
      if checkpoint.phase == .pressed, try windowExists() {
        _ = try events.event(.release(checkpoint.step))
        try post(.release(checkpoint.step)); try journal.set(.released); wait(0.05)
      }
      wait(0.01)
      // Never undo a real user activation or send input to a reused PID.
      if try sameProcess(), !targetIsForeground() { try post(.deactivate); wait(0.05) }
    }
    try journal.set(.idle)
  }
}

struct BackgroundPointerEvents {
  struct Step {
    let event: CGEvent
    let release: CGEvent?
    let delay: Double
  }
  let activation: CGEvent
  let deactivation: CGEvent
  let steps: [Step]
  typealias WindowLocationSetter = @convention(c) (CGEvent, CGPoint) -> Void

  func event(_ post: BackgroundPointerPost) throws -> CGEvent {
    switch post {
    case .activate: return activation
    case .deactivate: return deactivation
    case .step(let index):
      guard steps.indices.contains(index) else { throw BackgroundInputError("Invalid pointer step", outcome: .unknown) }
      return steps[index].event
    case .release(let index):
      guard steps.indices.contains(index), let release = steps[index].release else {
        throw BackgroundInputError("Invalid pointer recovery step", outcome: .unknown)
      }
      return release
    }
  }

  static func make(request: BackgroundPointerRequest) throws -> BackgroundPointerEvents {
    try request.validate()
    guard let symbol = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGEventSetWindowLocation") else {
      throw BackgroundInputError("Window-local event API unavailable on this macOS version; no foreground fallback")
    }
    let setter = unsafeBitCast(symbol, to: WindowLocationSetter.self)
    let input = request.input
    let point = try PointerCoordinatePolicy.globalPoint(frame: request.frame, x: input.x, y: input.y)
    func locate(_ event: CGEvent, at point: CGPoint) {
      event.flags = []; event.location = point
      event.setIntegerValueField(.mouseEventWindowUnderMousePointer, value: Int64(request.windowID))
      event.setIntegerValueField(.mouseEventWindowUnderMousePointerThatCanHandleThisEvent, value: Int64(request.windowID))
      setter(event, CGPoint(x: point.x - request.frame.x, y: point.y - request.frame.y))
    }
    func mouse(_ type: NSEvent.EventType, at point: CGPoint, button: PointerButton = .left, count: Int = 1) throws -> CGEvent {
      guard let event = NSEvent.mouseEvent(with: type, location: point, modifierFlags: [],
        timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: Int(request.windowID),
        context: nil, eventNumber: count, clickCount: count, pressure: 1)?.cgEvent else {
        throw BackgroundInputError("Could not construct window-local pointer event")
      }
      locate(event, at: point)
      event.setIntegerValueField(.mouseEventSubtype, value: 3)
      event.setIntegerValueField(.mouseEventButtonNumber, value: button == .left ? 0 : 1)
      event.setIntegerValueField(.mouseEventClickState, value: Int64(count))
      return event
    }
    func notification(_ subtype: NSEvent.EventSubtype) throws -> CGEvent {
      guard let event = NSEvent.otherEvent(with: .appKitDefined, location: .zero, modifierFlags: [],
        timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: 0, context: nil,
        subtype: subtype.rawValue, data1: 0, data2: 0)?.cgEvent else {
        throw BackgroundInputError("Could not construct target activation notification")
      }
      return event
    }
    var steps: [Step] = []
    switch input.action {
    case .click:
      let button = input.button!, count = input.clickCount!
      for click in 1...count {
        let up = try mouse(button == .left ? .leftMouseUp : .rightMouseUp, at: point, button: button, count: click)
        steps.append(Step(event: try mouse(button == .left ? .leftMouseDown : .rightMouseDown, at: point, button: button, count: click), release: up, delay: 0.05))
        steps.append(Step(event: up, release: nil, delay: click < count ? 0.08 : 0.05))
      }
    case .drag:
      let end = try PointerCoordinatePolicy.globalPoint(frame: request.frame, x: input.toX!, y: input.toY!)
      steps.append(Step(event: try mouse(.leftMouseDown, at: point), release: try mouse(.leftMouseUp, at: point), delay: 0.04))
      for step in 1...8 {
        let fraction = Double(step) / 8
        let next = CGPoint(x: point.x + (end.x - point.x) * fraction, y: point.y + (end.y - point.y) * fraction)
        steps.append(Step(event: try mouse(.leftMouseDragged, at: next), release: try mouse(.leftMouseUp, at: next), delay: 0.012))
      }
      steps.append(Step(event: try mouse(.leftMouseUp, at: end), release: nil, delay: 0.05))
    case .scroll:
      guard let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2,
        wheel1: Int32(-input.deltaY!), wheel2: Int32(-input.deltaX!), wheel3: 0) else {
        throw BackgroundInputError("Could not construct window-local scroll")
      }
      locate(event, at: point)
      // Target-directed scroll uses a separate window-number field (also used
      // by the isolated native probe), not a global mouse move or HID event.
      event.setIntegerValueField(CGEventField(rawValue: 51)!, value: Int64(request.windowID))
      steps.append(Step(event: event, release: nil, delay: 0.05))
    }
    return BackgroundPointerEvents(activation: try notification(.applicationActivated),
      deactivation: try notification(.applicationDeactivated), steps: steps)
  }

  static func transaction(request: BackgroundPointerRequest, journal: BackgroundInputJournal,
                          ownerCheck: @escaping () throws -> Void = {}) throws -> BackgroundPointerTransaction {
    let events = try make(request: request) // Construct all events before posting any.
    return BackgroundPointerTransaction(journal: journal, events: events,
      check: { try ownerCheck(); try request.check() },
      post: { post in
        guard AXIsProcessTrusted(), CGPreflightPostEventAccess() else {
          throw HelperError.permissionRequired("accessibility")
        }
        guard try request.sameProcess() else { throw BackgroundInputError("Target process changed before posting") }
        let event = try events.event(post)
        event.postToPid(request.pid)
      }, wait: { RunLoop.current.run(until: Date(timeIntervalSinceNow: $0)) },
      sameProcess: { try request.sameProcess() }, windowExists: { try request.currentFrame(includeOffscreen: true) != nil },
      targetIsForeground: { NSWorkspace.shared.frontmostApplication?.processIdentifier == request.pid })
  }
}

struct BackgroundWorkerResult: Codable { let completed: Bool; let error: ErrorDetail? }

enum BackgroundPointerWorker {
  static let command = "background-pointer-worker"
  static func requireOwner() throws {
    var entry = pollfd(fd: STDIN_FILENO, events: Int16(POLLIN | POLLHUP), revents: 0)
    guard poll(&entry, 1, 0) == 0 else {
      throw BackgroundInputError("Input owner disconnected")
    }
  }
  @MainActor static func run() {
    signal(SIGPIPE, SIG_IGN)
    NSApplication.shared.setActivationPolicy(.prohibited)
    let result: BackgroundWorkerResult
    do {
      var data = Data(), byte: UInt8 = 0
      while data.count <= 4_096 {
        guard read(STDIN_FILENO, &byte, 1) == 1 else { throw BackgroundInputError("Missing background request") }
        if byte == 10 { break }
        data.append(byte)
      }
      guard data.count <= 4_096 else { throw BackgroundInputError("Background request too large") }
      let request = try JSONDecoder().decode(BackgroundPointerRequest.self, from: data)
      try request.validate()
      let journal = BackgroundInputJournal(descriptor: 3)
      try journal.checkFile()
      guard try journal.phase() == .idle else { throw BackgroundInputError("Worker input journal is not idle") }
      try BackgroundPointerEvents.transaction(request: request, journal: journal, ownerCheck: requireOwner).run()
      result = BackgroundWorkerResult(completed: true, error: nil)
    } catch { result = BackgroundWorkerResult(completed: false, error: errorDetail(for: error)) }
    if let data = try? JSONEncoder.pudding.encode(result) {
      // The owner can disappear after recovery. The throwing Swift API reports
      // EPIPE instead of raising an uncaught NSFileHandleOperationException.
      try? FileHandle.standardOutput.write(contentsOf: data)
    }
  }
}

@MainActor enum BackgroundPointerService {
  static func perform(bundleID: String, target: CapturableWindowSnapshot, input: PointerInput) throws -> PointerSnapshot {
    guard let app = NSRunningApplication(processIdentifier: target.pid), app.bundleIdentifier == bundleID,
      let foreground = NSWorkspace.shared.frontmostApplication?.processIdentifier else {
      throw BackgroundInputError("Background target identity or foreground state unavailable")
    }
    let request = BackgroundPointerRequest(bundleID: bundleID, pid: target.pid,
      stamp: try BackgroundProcessStamp.read(target.pid), executable: try BackgroundProcessStamp.executablePath(target.pid),
      windowID: target.windowID, frame: target.frame, input: input, foregroundPID: foreground)
    try request.validate(); try request.check()
    // One machine-wide per-user target lock, including surviving workers after
    // Helper restart. Temporary recovery metadata is not session authorization.
    let journal = try BackgroundInputJournal.acquire(pid: request.pid, stamp: request.stamp)
    let recovery = try BackgroundPointerEvents.transaction(request: request, journal: journal)
    try runWorker(request: request, journal: journal, recovery: recovery)
    return PointerSnapshot(bundleID: bundleID, elementID: "", action: input.action.rawValue, completed: true,
      x: input.x, y: input.y, toX: input.toX, toY: input.toY, button: input.button?.rawValue, clickCount: input.clickCount,
      deltaX: input.deltaX, deltaY: input.deltaY, delivery: input.delivery)
  }

  private static func runWorker(request: BackgroundPointerRequest, journal: BackgroundInputJournal,
                         recovery: BackgroundPointerTransaction) throws {
    let payload = try JSONEncoder.pudding.encode(request) + Data([10])
    guard payload.count <= 4_096 else { throw BackgroundInputError("Background request too large") }
    var control: [Int32] = [0, 0], response: [Int32] = [0, 0]
    guard pipe(&control) == 0 else { throw BackgroundInputError("Cannot create input owner pipe") }
    defer { close(control[0]); close(control[1]) }
    guard pipe(&response) == 0 else { throw BackgroundInputError("Cannot create input response pipe") }
    defer { close(response[0]) }
    let worker: pid_t
    do { worker = try spawnWorker(control: control[0], response: response[1], journal: journal.descriptor) }
    catch { close(response[1]); throw error }
    close(response[1])
    // Every path after spawning must reap the worker before caller-side recovery.
    signal(SIGPIPE, SIG_IGN)
    let written = payload.withUnsafeBytes { write(control[1], $0.baseAddress, $0.count) }
    if written != payload.count { kill(worker, SIGKILL) }
    let deadline = ProcessInfo.processInfo.systemUptime + 5
    var status: Int32 = 0
    while true {
      let result = waitpid(worker, &status, WNOHANG)
      if result == worker { break }
      if result < 0 && errno != EINTR {
        // Without proof of exit we must not compete with a live cleanup owner.
        throw BackgroundInputError("Cannot establish worker exit; no replay", outcome: .unknown)
      }
      if ProcessInfo.processInfo.systemUptime >= deadline { kill(worker, SIGKILL) }
      RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.01))
    }
    let unfinished = try journal.phase() != .idle
    if unfinished {
      do { try recovery.recover() }
      catch { throw BackgroundInputError("Worker recovery failed: \(error.localizedDescription)", outcome: .unknown) }
    }
    var bytes = [UInt8](repeating: 0, count: 4_096)
    let count = read(response[0], &bytes, bytes.count)
    guard status == 0, !unfinished, count > 0, count < bytes.count,
      let result = try? JSONDecoder().decode(BackgroundWorkerResult.self, from: Data(bytes.prefix(max(0, count)))) else {
      throw BackgroundInputError("Background worker interrupted; effect may be partial; do not replay", outcome: .unknown)
    }
    if let error = result.error { throw BackgroundInputError(detail: error) }
    guard result.completed else {
      throw BackgroundInputError("Invalid background worker result; do not replay", outcome: .unknown)
    }
    withExtendedLifetime(journal) {}
  }

  private static func spawnWorker(control: Int32, response: Int32, journal: Int32) throws -> pid_t {
    var actions: posix_spawn_file_actions_t?, attributes: posix_spawnattr_t?
    guard posix_spawn_file_actions_init(&actions) == 0 else { throw BackgroundInputError("Cannot create worker actions") }
    defer { posix_spawn_file_actions_destroy(&actions) }
    guard posix_spawnattr_init(&attributes) == 0 else { throw BackgroundInputError("Cannot create worker attributes") }
    defer { posix_spawnattr_destroy(&attributes) }
    guard posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT)) == 0 else {
      throw BackgroundInputError("Cannot isolate background worker")
    }
    for (source, target) in [(control, Int32(0)), (response, Int32(1)), (STDERR_FILENO, Int32(2)), (journal, Int32(3))] {
      let result = source == target ? posix_spawn_file_actions_addinherit_np(&actions, source)
        : posix_spawn_file_actions_adddup2(&actions, source, target)
      guard result == 0 else { throw BackgroundInputError("Cannot inherit worker descriptor") }
    }
    guard let executable = Bundle.main.executableURL?.path else { throw BackgroundInputError("Helper executable unavailable") }
    var arguments: [UnsafeMutablePointer<CChar>?] = [executable, BackgroundPointerWorker.command].map { $0.withCString { strdup($0) } } + [nil]
    var environment: [UnsafeMutablePointer<CChar>?] = ProcessInfo.processInfo.environment.map { key, value in
      "\(key)=\(value)".withCString { strdup($0) }
    } + [nil]
    defer { arguments.forEach { free($0) }; environment.forEach { free($0) } }
    var pid: pid_t = 0
    guard posix_spawn(&pid, executable, &actions, &attributes, &arguments, &environment) == 0 else {
      throw BackgroundInputError("Could not spawn background worker")
    }
    return pid
  }
}
