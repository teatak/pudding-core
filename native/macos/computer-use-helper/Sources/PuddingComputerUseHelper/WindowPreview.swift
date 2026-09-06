import AppKit
import CoreImage
import Foundation
@preconcurrency import ScreenCaptureKit

struct WindowPreviewTarget: Codable {
  let bundleID: String
  let windowID: UInt32
  let pid: Int32?
}

private struct WindowPreviewFrame: Encodable {
  let type = "frame"
  let pid: Int32
  let windowID: UInt32
  let name: String
  let title: String
  let width: Int
  let height: Int
  let jpeg: String
}

// A separate read-only process keeps continuous capture out of the action queue.
final class WindowPreview: NSObject, SCStreamOutput, SCStreamDelegate, @unchecked Sendable {
  private let frames = DispatchQueue(label: "pudding.window-preview.frames", qos: .utility)
  private let context = CIContext(options: [.cacheIntermediates: false])
  private var window: SCWindow?

  func run() async throws {
    guard let line = readLine(), line.utf8.count <= 4096 else {
      throw ArgumentError.missingOption("preview target")
    }
    let target = try JSONDecoder().decode(WindowPreviewTarget.self, from: Data(line.utf8))
    guard !target.bundleID.isEmpty, target.bundleID.utf8.count <= 512, target.windowID > 0,
      target.pid == nil || target.pid! > 0 else {
      throw ArgumentError.invalidOption("preview target", "invalid window identity")
    }
    guard CGPreflightScreenCaptureAccess() else { throw HelperError.permissionRequired("screen_recording") }
    let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
    guard let window = content.windows.first(where: {
      $0.windowID == target.windowID && $0.owningApplication?.bundleIdentifier == target.bundleID
        && (target.pid == nil || $0.owningApplication?.processID == target.pid)
    }), let owner = window.owningApplication else { throw HelperError.windowNotFound(target.windowID) }
    guard AppPolicy.allows(bundleID: target.bundleID, pid: owner.processID) else {
      throw HelperError.appNotAllowed(target.bundleID)
    }
    self.window = window
    let scale = min(1, 640 / max(1, window.frame.width), 480 / max(1, window.frame.height))
    let configuration = SCStreamConfiguration()
    configuration.width = max(1, Int(window.frame.width * scale))
    configuration.height = max(1, Int(window.frame.height * scale))
    configuration.minimumFrameInterval = CMTime(value: 1, timescale: 5)
    configuration.queueDepth = 3
    configuration.showsCursor = true
    configuration.capturesAudio = false
    configuration.ignoreShadowsSingleWindow = true
    let stream = SCStream(filter: SCContentFilter(desktopIndependentWindow: window), configuration: configuration, delegate: self)
    try stream.addStreamOutput(self, type: .screen, sampleHandlerQueue: frames)
    try await stream.startCapture()
    // EOF also releases capture if Electron exits unexpectedly.
    await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
      DispatchQueue.global(qos: .utility).async {
        _ = FileHandle.standardInput.readDataToEndOfFile()
        continuation.resume()
      }
    }
    try await stream.stopCapture()
  }

  func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of outputType: SCStreamOutputType) {
    guard outputType == .screen, sampleBuffer.isValid,
      let info = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, createIfNecessary: false) as? [[SCStreamFrameInfo: Any]],
      let status = info.first?[.status] as? Int, status == SCFrameStatus.complete.rawValue,
      let rectData = info.first?[.contentRect] as? [String: Any],
      let contentRect = CGRect(dictionaryRepresentation: rectData as CFDictionary),
      let scaleFactor = info.first?[.scaleFactor] as? CGFloat,
      let pixels = sampleBuffer.imageBuffer, let window, let owner = window.owningApplication else { return }
    autoreleasepool {
      let image = CIImage(cvPixelBuffer: pixels)
      // ScreenCaptureKit's content rect follows live window resizing within the
      // fixed capture buffer. Convert surface points/top-left to Core Image pixels.
      let crop = Self.cropRect(contentRect: contentRect, scaleFactor: scaleFactor, extent: image.extent)
      guard !crop.isEmpty, let cgImage = context.createCGImage(image, from: crop),
        let jpeg = NSBitmapImageRep(cgImage: cgImage).representation(using: .jpeg, properties: [.compressionFactor: 0.65]) else { return }
      try? writeJSON(WindowPreviewFrame(pid: owner.processID, windowID: window.windowID,
        name: owner.applicationName, title: window.title ?? "", width: cgImage.width, height: cgImage.height,
        jpeg: jpeg.base64EncodedString()))
    }
  }

  static func cropRect(contentRect: CGRect, scaleFactor: CGFloat, extent: CGRect) -> CGRect {
    CGRect(x: contentRect.minX * scaleFactor, y: extent.height - contentRect.maxY * scaleFactor,
      width: contentRect.width * scaleFactor, height: contentRect.height * scaleFactor)
      .integral.intersection(extent)
  }

  func stream(_ stream: SCStream, didStopWithError error: Error) {
    // The parent drops the stale image and reports a stopped preview; no capture retry.
    exit(1)
  }
}
