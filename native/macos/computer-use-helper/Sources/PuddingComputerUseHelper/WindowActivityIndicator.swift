import CoreMedia
import Foundation
@preconcurrency import ScreenCaptureKit

@MainActor
final class WindowActivityIndicator {
  static let shared = WindowActivityIndicator()
  private static let idleReleaseDelay = Duration.seconds(5)

  private struct Target: Equatable {
    let bundleID: String
    let windowID: UInt32
  }

  private final class FrameSink: NSObject, SCStreamOutput, SCStreamDelegate,
    @unchecked Sendable
  {
    func stream(
      _ stream: SCStream,
      didOutputSampleBuffer sampleBuffer: CMSampleBuffer,
      of outputType: SCStreamOutputType
    ) {}

    func stream(_ stream: SCStream, didStopWithError error: any Error) {}
  }

  private let frameSink = FrameSink()
  private var stream: SCStream?
  private var target: Target?
  private var activityID: UInt64 = 0
  private var delayedStop: Task<Void, Never>?

  func begin(bundleID: String, windowID: UInt32) async -> UInt64 {
    activityID &+= 1
    let currentActivityID = activityID
    delayedStop?.cancel()
    delayedStop = nil

    let requestedTarget = Target(bundleID: bundleID, windowID: windowID)
    if target == requestedTarget, stream != nil {
      return currentActivityID
    }

    await stopCurrentStream()
    do {
      let content = try await SCShareableContent.excludingDesktopWindows(
        true,
        onScreenWindowsOnly: true
      )
      guard let window = content.windows.first(where: {
        $0.windowID == windowID
          && $0.owningApplication?.bundleIdentifier == bundleID
      }) else {
        return currentActivityID
      }
      guard let display = content.displays.max(by: {
        intersectionArea($0.frame, window.frame) < intersectionArea($1.frame, window.frame)
      }), intersectionArea(display.frame, window.frame) > 0
      else {
        return currentActivityID
      }

      let configuration = SCStreamConfiguration()
      configuration.width = 2
      configuration.height = 2
      configuration.minimumFrameInterval = CMTime(value: 1, timescale: 2)
      configuration.queueDepth = 1
      configuration.showsCursor = false
      configuration.capturesAudio = false

      let nextStream = SCStream(
        filter: SCContentFilter(display: display, including: [window]),
        configuration: configuration,
        delegate: frameSink
      )
      try nextStream.addStreamOutput(
        frameSink,
        type: .screen,
        sampleHandlerQueue: DispatchQueue(label: "com.teatak.pudding.computer-use-indicator")
      )
      try await nextStream.startCapture()
      stream = nextStream
      target = requestedTarget
    } catch {
      stream = nil
      target = nil
    }
    return currentActivityID
  }

  func finish(_ finishingActivityID: UInt64) {
    guard finishingActivityID == activityID else { return }
    delayedStop?.cancel()
    delayedStop = Task { [weak self] in
      try? await Task.sleep(for: Self.idleReleaseDelay)
      guard !Task.isCancelled else { return }
      await self?.stop(ifActivityID: finishingActivityID)
    }
  }

  private func stop(ifActivityID finishingActivityID: UInt64) async {
    guard finishingActivityID == activityID else { return }
    delayedStop = nil
    await stopCurrentStream()
  }

  private func stopCurrentStream() async {
    delayedStop?.cancel()
    delayedStop = nil
    guard let activeStream = stream else {
      target = nil
      return
    }
    stream = nil
    target = nil
    try? await activeStream.stopCapture()
  }

  private func intersectionArea(_ left: CGRect, _ right: CGRect) -> CGFloat {
    let intersection = left.intersection(right)
    guard !intersection.isNull, !intersection.isEmpty else { return 0 }
    return intersection.width * intersection.height
  }
}
