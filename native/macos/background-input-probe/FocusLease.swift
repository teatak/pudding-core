// Test-only per-process input ownership. One locked journal is shared by the
// caller and worker. The worker writes while alive; the caller may recover only
// after waitpid proves it has exited. Neither process keeps a second phase copy.
import Foundation
import AppKit
import ApplicationServices
import Darwin

enum FocusPhase: UInt8 {
  case idle, activated, pressed, released
}

struct ProcessStamp: Codable, Equatable {
  let seconds: UInt64
  let microseconds: UInt64

  static func read(_ pid: pid_t) throws -> ProcessStamp {
    var info = proc_bsdinfo()
    let size = Int32(MemoryLayout<proc_bsdinfo>.size)
    try require(proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size,
      "could not read target process creation identity")
    return ProcessStamp(seconds: info.pbi_start_tvsec, microseconds: info.pbi_start_tvusec)
  }
}

final class FocusLease {
  let descriptor: Int32
  private init(_ descriptor: Int32) { self.descriptor = descriptor }
  // Never LOCK_UN: an inherited descriptor must keep the same lock alive when
  // either process dies. The kernel unlocks only after the last close.
  deinit { close(descriptor) }

  func phase() throws -> FocusPhase {
    var byte: UInt8 = 255
    guard pread(descriptor, &byte, 1, 0) == 1, let phase = FocusPhase(rawValue: byte)
    else { throw ProbeError("invalid focus recovery journal; no input allowed") }
    return phase
  }

  func set(_ phase: FocusPhase) throws {
    var byte = phase.rawValue
    try require(pwrite(descriptor, &byte, 1, 0) == 1, "could not record focus recovery intent")
  }

  static func acquire(at path: String, waitSeconds: Double = 3) throws -> FocusLease {
    let fd = open(path, O_RDWR | O_CREAT | O_CLOEXEC | O_NOFOLLOW, S_IRUSR | S_IWUSR)
    try require(fd >= 0, "could not open focus ownership journal")
    let lease = FocusLease(fd)
    var info = stat()
    try require(fstat(fd, &info) == 0 && info.st_uid == getuid() && info.st_nlink == 1
      && (info.st_mode & S_IFMT) == S_IFREG, "invalid focus ownership file")
    let deadline = ProcessInfo.processInfo.systemUptime + waitSeconds
    var announced = false
    while flock(fd, LOCK_EX | LOCK_NB) != 0 {
      try require(errno == EWOULDBLOCK || errno == EINTR, "could not acquire target input ownership")
      try require(ProcessInfo.processInfo.systemUptime < deadline, "target input busy; no events posted")
      if !announced { emit(["kind": "lease-wait"]); announced = true }
      RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.01))
    }
    try require(fstat(fd, &info) == 0 && (info.st_size == 0 || info.st_size == 1), "invalid focus journal size")
    if info.st_size == 0 { try lease.set(.idle) }
    try require(try lease.phase() == .idle,
      "target has unresolved input state from a prior owner; no events posted")
    emit(["kind": "lease-acquired"])
    return lease
  }

  static func inherited() throws -> FocusLease {
    var info = stat()
    let result = fstat(3, &info)
    try require(result == 0 && info.st_uid == getuid() && info.st_nlink == 1
      && (info.st_mode & S_IFMT) == S_IFREG && info.st_size == 1,
      "shared focus journal missing: result=\(result) errno=\(errno) size=\(info.st_size) mode=\(info.st_mode)")
    let lease = FocusLease(3)
    try require(try lease.phase() == .idle, "worker must start with an idle journal")
    return lease
  }
}

func restoreFocus(_ request: Request, lease: FocusLease, originalApp: NSRunningApplication?, stamp: ProcessStamp) throws {
  let phase = try lease.phase()
  if phase == .idle { return }
  if let app = NSRunningApplication(processIdentifier: request.target.pid), let originalApp,
    !originalApp.isTerminated, isFocusTarget(app, request: request),
    try ProcessStamp.read(request.target.pid) == stamp {
    if phase == .pressed, windowInfo(request.target.pid, request.target.windowID, external: request.realApp != nil) != nil {
      guard let symbol = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGEventSetWindowLocation"),
        let point = request.target.points["click"]?.point
      else { throw ProbeError("could not construct pending mouse release") }
      let setter = unsafeBitCast(symbol, to: WindowLocationSetter.self)
      let up = try buildEvent(variant: .windowLocal, type: .leftMouseUp, target: request.target,
        point: point, count: 1, setter: setter)
      up.postToPid(request.target.pid)
      try lease.set(.released)
      emit(["kind": "cleanup", "event": "button-up", "recoveryPID": getpid()])
      RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.05))
    }
    // The user may really activate this app while recovery is in progress.
    // Do not undo that activation; refresh NSWorkspace via the run loop first.
    RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.01))
    if NSWorkspace.shared.frontmostApplication?.processIdentifier != request.target.pid {
      try focusNotification(activated: false).postToPid(request.target.pid)
      emit(["kind": "focus-notification", "activated": false, "targetPID": request.target.pid])
      RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.05))
    }
  }
  // A terminated/replaced target has no remaining state that we may act on.
  try lease.set(.idle)
  emit(["kind": "lease-restored", "previousPhase": phase.rawValue, "recoveryPID": getpid()])
}

func requireInputOwner(_ descriptor: Int32) throws {
  var entry = pollfd(fd: descriptor, events: Int16(POLLIN | POLLHUP), revents: 0)
  let result = poll(&entry, 1, 0)
  try require(result == 0, "input owner disconnected or control pipe changed")
}

func readFocusRequest() throws -> Data {
  var data = Data()
  var byte: UInt8 = 0
  // Exactly one bounded header; EOF after it is cancellation, not another action.
  while data.count < 65_536 {
    guard read(STDIN_FILENO, &byte, 1) == 1 else { throw ProbeError("input owner disconnected before request") }
    if byte == 10 { return data }
    data.append(byte)
  }
  throw ProbeError("focus request is too large")
}

func sendWithFocusLease(_ data: Data, notifyOnly: Bool = false) throws {
  signal(SIGPIPE, SIG_IGN) // Diagnostics must not kill the remaining recovery owner.
  let request = try JSONDecoder().decode(Request.self, from: data)
  _ = try validate(request)
  try require(request.variant == "appkit-window-local" && request.action == "click", "focus lease is ordinary-click only")
  try require(!notifyOnly || request.realApp != nil, "notification isolation is real-app smoke only")
  guard let originalApp = NSRunningApplication(processIdentifier: request.target.pid)
  else { throw ProbeError("owned target process missing") }
  try require(isFocusTarget(originalApp, request: request), "owned target executable/bundle identity missing")
  let stamp = try ProcessStamp.read(request.target.pid)
  let directory = Bundle.main.executableURL!.deletingLastPathComponent()
  let journal = directory.appendingPathComponent("focus-\(request.target.pid)-\(stamp.seconds)-\(stamp.microseconds).state")
  let lease = try FocusLease.acquire(at: journal.path)
  try require(!originalApp.isTerminated && (try ProcessStamp.read(request.target.pid)) == stamp,
    "target changed while waiting for input ownership")
  var envelope = try JSONSerialization.jsonObject(with: data) as! [String: Any]
  envelope["processStart"] = ["seconds": stamp.seconds, "microseconds": stamp.microseconds]
  envelope["notifyOnly"] = notifyOnly // CLI-owned diagnostic mode, never accepted from caller JSON.
  let header = try JSONSerialization.data(withJSONObject: envelope)
  try require(header.count < 65_536, "focus request is too large")
  var control: [Int32] = [0, 0]
  try require(pipe(&control) == 0, "could not create worker control pipe")
  defer { close(control[0]); close(control[1]) }
  let worker = try spawnFocusWorker(control: control[0], journal: lease.descriptor)
  emit(["kind": "lease-start", "ownerPID": getpid(), "leasePID": worker])
  let payload = header + Data([10])
  try payload.withUnsafeBytes { bytes in
    var offset = 0
    while offset < bytes.count {
      let n = write(control[1], bytes.baseAddress!.advanced(by: offset), bytes.count - offset)
      if n < 0 && errno == EINTR { continue }
      try require(n > 0, "worker request pipe closed")
      offset += n
    }
  }
  var status: Int32 = 0
  while true {
    let result = waitpid(worker, &status, WNOHANG)
    if result == worker { break }
    try require(result == 0 || (result == -1 && errno == EINTR), "could not await owned input worker")
    RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.01))
  }
  emit(["kind": "lease-worker-exit", "waitStatus": status])
  let unfinished = try lease.phase() != .idle
  if unfinished { try restoreFocus(request, lease: lease, originalApp: originalApp, stamp: stamp) }
  try require(status == 0 && !unfinished,
    "input worker interrupted; recovery completed, action outcome may be partial; do not replay")
  withExtendedLifetime(lease) {} // Keep ownership through worker exit and recovery.
}

private func spawnFocusWorker(control: Int32, journal: Int32, command: String = "focus-probe-worker") throws -> pid_t {
  var actions: posix_spawn_file_actions_t?
  var attributes: posix_spawnattr_t?
  try require(posix_spawn_file_actions_init(&actions) == 0, "could not create worker descriptor actions")
  defer { posix_spawn_file_actions_destroy(&actions) }
  try require(posix_spawnattr_init(&attributes) == 0, "could not create worker attributes")
  defer { posix_spawnattr_destroy(&attributes) }
  try require(posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT)) == 0,
    "could not isolate worker descriptors")
  // Only these descriptors survive exec. In particular the control writer must
  // remain caller-only so its death is visible as EOF to the worker.
  for (source, target) in [(control, Int32(0)), (STDOUT_FILENO, Int32(1)),
    (STDERR_FILENO, Int32(2)), (journal, Int32(3))] {
    // dup2(fd, fd) does not clear FD_CLOEXEC on macOS. Explicit inheritance
    // is required when the journal already occupies the desired descriptor.
    let result = source == target ? posix_spawn_file_actions_addinherit_np(&actions, source)
      : posix_spawn_file_actions_adddup2(&actions, source, target)
    try require(result == 0, "could not inherit worker descriptor")
  }
  let executable = Bundle.main.executableURL!.path
  var arguments: [UnsafeMutablePointer<CChar>?] = [executable, command].map {
    $0.withCString { strdup($0) }
  } + [nil]
  var environment: [UnsafeMutablePointer<CChar>?] = ProcessInfo.processInfo.environment.map { key, value in
    "\(key)=\(value)".withCString { strdup($0) }
  } + [nil]
  defer { arguments.forEach { free($0) }; environment.forEach { free($0) } }
  var pid: pid_t = 0
  let result = posix_spawn(&pid, executable, &actions, &attributes, &arguments, &environment)
  try require(result == 0, "could not spawn input worker: \(result)")
  return pid
}

func focusLeaseSelfTest() throws {
  let stamp = try ProcessStamp.read(getpid())
  try require(try ProcessStamp.read(getpid()) == stamp, "process creation identity is not stable")
  var descriptors: [Int32] = [0, 0]
  try require(pipe(&descriptors) == 0, "could not create test control pipe")
  defer { close(descriptors[0]) }
  try requireInputOwner(descriptors[0])
  close(descriptors[1])
  var rejected = false
  do { try requireInputOwner(descriptors[0]) } catch { rejected = true }
  try require(rejected, "closed control pipe must cancel input")

  let directory = FileManager.default.temporaryDirectory.appendingPathComponent("focus-lease-test-\(UUID().uuidString)")
  try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false)
  defer { try? FileManager.default.removeItem(at: directory) }
  let path = directory.appendingPathComponent("journal").path
  var lease: FocusLease? = try FocusLease.acquire(at: path, waitSeconds: 0)
  let duplicate = dup(lease!.descriptor)
  try require(duplicate >= 0, "could not duplicate test ownership")
  lease = nil
  rejected = false
  do { _ = try FocusLease.acquire(at: path, waitSeconds: 0) } catch { rejected = true }
  close(duplicate)
  try require(rejected, "shared descriptor did not retain exclusive ownership")
  lease = try FocusLease.acquire(at: path, waitSeconds: 0)
  var control: [Int32] = [0, 0]
  try require(pipe(&control) == 0, "could not create descriptor test pipe")
  defer { close(control[0]) }
  let worker = try spawnFocusWorker(control: control[0], journal: lease!.descriptor, command: "focus-lease-descriptor-test")
  var newline: UInt8 = 10
  let wrote = write(control[1], &newline, 1)
  close(control[1]) // Child must not inherit this writer, or EOF cannot arrive.
  var status: Int32 = 0
  let waited = waitpid(worker, &status, 0)
  try require(wrote == 1 && waited == worker && status == 0 && (try lease!.phase()) == .activated,
    "exec did not retain the shared journal and caller-only control writer")
  for phase in [FocusPhase.activated, .pressed, .released, .idle] {
    try lease!.set(phase)
    try require(try lease!.phase() == phase, "journal lost authoritative recovery phase")
  }
  try lease!.set(.pressed)
  lease = nil
  rejected = false
  do { _ = try FocusLease.acquire(at: path, waitSeconds: 0) } catch { rejected = true }
  try require(rejected, "unresolved state must reject new input, not replay or silently clear")
}

func focusLeaseDescriptorTest() throws {
  let lease = try FocusLease.inherited()
  _ = try readFocusRequest()
  let deadline = ProcessInfo.processInfo.systemUptime + 1
  while (try? requireInputOwner(STDIN_FILENO)) != nil {
    try require(ProcessInfo.processInfo.systemUptime < deadline, "worker inherited a control writer")
    RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.01))
  }
  try lease.set(.activated) // No event is sent; parent checks the very same byte.
}
