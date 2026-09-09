// Explicit, limited background delivery. No global input, activation, observation
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

enum BackgroundClickPolicy {
  // Evidence is for these system applications, not arbitrary apps with similar
  // bundle IDs. In particular Feishu's foreground-stealing case is not enabled.
  static func allows(bundleID: String, bundlePath: String, executable: String) -> Bool {
    let name: String
    switch bundleID {
    case "com.apple.calculator": name = "Calculator"
    case "com.apple.mail": name = "Mail"
    case "com.apple.iCal": name = "Calendar"
    default: return false
    }
    let path = "/System/Applications/\(name).app"
    return bundlePath == path && executable == "\(path)/Contents/MacOS/\(name)"
  }
}

struct BackgroundProcessStamp: Codable, Equatable {
  let seconds: UInt64
  let microseconds: UInt64
  static func read(_ pid: pid_t) throws -> Self {
    var info = proc_bsdinfo()
    let size = Int32(MemoryLayout<proc_bsdinfo>.size)
    guard proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size else {
      throw BackgroundInputError("Target process identity is unavailable; no input sent")
    }
    return Self(seconds: info.pbi_start_tvsec, microseconds: info.pbi_start_tvusec)
  }
}

struct BackgroundClickRequest: Codable {
  let bundleID: String
  let pid: pid_t
  let stamp: BackgroundProcessStamp
  let bundlePath: String
  let executable: String
  let windowID: UInt32
  let frame: FrameSnapshot
  let x: Double
  let y: Double
  let foregroundPID: pid_t

  func validate() throws {
    guard pid > 0, windowID > 0, foregroundPID > 0,
      BackgroundClickPolicy.allows(bundleID: bundleID, bundlePath: bundlePath, executable: executable),
      AppPolicy.allows(bundleID: bundleID, pid: pid),
      frame.x.isFinite, frame.y.isFinite, frame.width.isFinite, frame.height.isFinite else {
      throw BackgroundInputError("Background single-click preview supports only system Calculator, Mail and Calendar")
    }
    _ = try PointerCoordinatePolicy.globalPoint(frame: frame, x: x, y: y)
  }

  func sameProcess() -> Bool {
    guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated,
      app.bundleIdentifier == bundleID, app.bundleURL?.path == bundlePath,
      app.executableURL?.path == executable else { return false }
    return (try? BackgroundProcessStamp.read(pid)) == stamp
  }

  func currentFrame() -> CGRect? {
    let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], 0)
      as? [[String: Any]] ?? []
    guard let info = windows.first(where: {
      ($0[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid &&
      ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == windowID &&
      ($0[kCGWindowLayer as String] as? NSNumber)?.intValue == 0
    }), let bounds = info[kCGWindowBounds as String] as? NSDictionary else { return nil }
    return CGRect(dictionaryRepresentation: bounds as CFDictionary)
  }

  func check() throws {
    guard AXIsProcessTrusted(), CGPreflightPostEventAccess() else {
      throw HelperError.permissionRequired("accessibility")
    }
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    guard sameProcess(), currentFrame() == CGRect(x: frame.x, y: frame.y, width: frame.width, height: frame.height) else {
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
    var byte: UInt8 = 255
    guard pread(descriptor, &byte, 1, 0) == 1, let phase = BackgroundInputPhase(rawValue: byte) else {
      throw BackgroundInputError("Invalid input recovery journal; no further input allowed", outcome: .unknown)
    }
    return phase
  }
  func set(_ phase: BackgroundInputPhase) throws {
    var byte = phase.rawValue
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
final class BackgroundClickTransaction {
  let journal: BackgroundInputJournal
  let check: () throws -> Void
  let post: (BackgroundInputPhase) throws -> Void
  let wait: (Double) -> Void
  let sameProcess: () -> Bool
  let windowExists: () -> Bool
  let targetIsForeground: () -> Bool

  init(journal: BackgroundInputJournal, check: @escaping () throws -> Void,
       post: @escaping (BackgroundInputPhase) throws -> Void,
       wait: @escaping (Double) -> Void, sameProcess: @escaping () -> Bool,
       windowExists: @escaping () -> Bool, targetIsForeground: @escaping () -> Bool) {
    self.journal = journal; self.check = check; self.post = post; self.wait = wait
    self.sameProcess = sameProcess; self.windowExists = windowExists; self.targetIsForeground = targetIsForeground
  }

  func run() throws {
    try check()
    do {
      try journal.set(.activated); try post(.activated); wait(0.1)
      try check()
      try journal.set(.pressed); try post(.pressed); wait(0.05)
      try check()
      try post(.released); try journal.set(.released); wait(0.05)
      try check()
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
    let phase = try journal.phase()
    guard phase != .idle else { return }
    if sameProcess() {
      if phase == .pressed, windowExists() {
        try post(.released); try journal.set(.released); wait(0.05)
      }
      wait(0.01)
      // Never undo a real user activation or send input to a reused PID.
      if sameProcess(), !targetIsForeground() { try post(.idle); wait(0.05) }
    }
    try journal.set(.idle)
  }
}

enum BackgroundClickEvents {
  typealias WindowLocationSetter = @convention(c) (CGEvent, CGPoint) -> Void

  static func make(request: BackgroundClickRequest) throws -> [BackgroundInputPhase: CGEvent] {
    guard let symbol = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGEventSetWindowLocation") else {
      throw BackgroundInputError("Window-local event API unavailable on this macOS version; no foreground fallback")
    }
    let setter = unsafeBitCast(symbol, to: WindowLocationSetter.self)
    let point = try PointerCoordinatePolicy.globalPoint(frame: request.frame, x: request.x, y: request.y)
    var events: [BackgroundInputPhase: CGEvent] = [:]
    for (phase, type): (BackgroundInputPhase, NSEvent.EventType) in [(.pressed, .leftMouseDown), (.released, .leftMouseUp)] {
      guard let event = NSEvent.mouseEvent(with: type, location: point, modifierFlags: [],
        timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: Int(request.windowID),
        context: nil, eventNumber: 1, clickCount: 1, pressure: 1)?.cgEvent else {
        throw BackgroundInputError("Could not construct window-local click")
      }
      event.flags = []; event.location = point
      event.setIntegerValueField(.mouseEventWindowUnderMousePointer, value: Int64(request.windowID))
      event.setIntegerValueField(.mouseEventWindowUnderMousePointerThatCanHandleThisEvent, value: Int64(request.windowID))
      event.setIntegerValueField(.mouseEventSubtype, value: 3)
      event.setIntegerValueField(.mouseEventButtonNumber, value: 0)
      event.setIntegerValueField(.mouseEventClickState, value: 1)
      setter(event, CGPoint(x: point.x - request.frame.x, y: point.y - request.frame.y))
      events[phase] = event
    }
    for (phase, subtype): (BackgroundInputPhase, NSEvent.EventSubtype) in [(.activated, .applicationActivated), (.idle, .applicationDeactivated)] {
      guard let event = NSEvent.otherEvent(with: .appKitDefined, location: .zero, modifierFlags: [],
        timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: 0, context: nil,
        subtype: subtype.rawValue, data1: 0, data2: 0)?.cgEvent else {
        throw BackgroundInputError("Could not construct target activation notification")
      }
      events[phase] = event
    }
    return events
  }

  static func transaction(request: BackgroundClickRequest, journal: BackgroundInputJournal,
                          ownerCheck: @escaping () throws -> Void = {}) throws -> BackgroundClickTransaction {
    let events = try make(request: request) // Construct all events before posting any.
    return BackgroundClickTransaction(journal: journal,
      check: { try ownerCheck(); try request.check() },
      post: { phase in
        guard AXIsProcessTrusted(), CGPreflightPostEventAccess() else {
          throw HelperError.permissionRequired("accessibility")
        }
        guard request.sameProcess() else { throw BackgroundInputError("Target process changed before posting") }
        guard let event = events[phase] else { throw BackgroundInputError("Missing background event") }
        event.postToPid(request.pid)
      }, wait: { RunLoop.current.run(until: Date(timeIntervalSinceNow: $0)) },
      sameProcess: { request.sameProcess() }, windowExists: { request.currentFrame() != nil },
      targetIsForeground: { NSWorkspace.shared.frontmostApplication?.processIdentifier == request.pid })
  }
}

struct BackgroundWorkerResult: Codable { let completed: Bool; let error: ErrorDetail? }

enum BackgroundClickWorker {
  static let command = "background-click-worker"
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
      let request = try JSONDecoder().decode(BackgroundClickRequest.self, from: data)
      try request.validate()
      let journal = BackgroundInputJournal(descriptor: 3)
      try journal.checkFile()
      guard try journal.phase() == .idle else { throw BackgroundInputError("Worker input journal is not idle") }
      try BackgroundClickEvents.transaction(request: request, journal: journal, ownerCheck: requireOwner).run()
      result = BackgroundWorkerResult(completed: true, error: nil)
    } catch { result = BackgroundWorkerResult(completed: false, error: errorDetail(for: error)) }
    if let data = try? JSONEncoder.pudding.encode(result) { FileHandle.standardOutput.write(data) }
  }
}

@MainActor enum BackgroundClickService {
  static func perform(bundleID: String, target: CapturableWindowSnapshot, input: PointerInput) throws -> PointerSnapshot {
    guard input.delivery == "background", input.action == .click, input.button == .left, input.clickCount == 1 else {
      throw HelperError.invalidPointerInput("background delivery accepts only a single left click")
    }
    guard let app = NSRunningApplication(processIdentifier: target.pid),
      let bundlePath = app.bundleURL?.path, let executable = app.executableURL?.path,
      let foreground = NSWorkspace.shared.frontmostApplication?.processIdentifier else {
      throw BackgroundInputError("Background target identity or foreground state unavailable")
    }
    let request = BackgroundClickRequest(bundleID: bundleID, pid: target.pid,
      stamp: try BackgroundProcessStamp.read(target.pid), bundlePath: bundlePath, executable: executable,
      windowID: target.windowID, frame: target.frame, x: input.x, y: input.y, foregroundPID: foreground)
    try request.validate(); try request.check()
    // One machine-wide per-user target lock, including surviving workers after
    // Helper restart. Temporary recovery metadata is not session authorization.
    let journal = try BackgroundInputJournal.acquire(pid: request.pid, stamp: request.stamp)
    let recovery = try BackgroundClickEvents.transaction(request: request, journal: journal)
    try runWorker(request: request, journal: journal, recovery: recovery)
    return PointerSnapshot(bundleID: bundleID, elementID: "", action: "click", completed: true,
      x: input.x, y: input.y, toX: nil, toY: nil, button: "left", clickCount: 1,
      deltaX: nil, deltaY: nil, delivery: input.delivery)
  }

  private static func runWorker(request: BackgroundClickRequest, journal: BackgroundInputJournal,
                         recovery: BackgroundClickTransaction) throws {
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
    var arguments: [UnsafeMutablePointer<CChar>?] = [executable, BackgroundClickWorker.command].map { $0.withCString { strdup($0) } } + [nil]
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
