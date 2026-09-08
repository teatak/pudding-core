import AppKit
import CoreVideo
import Testing
@testable import PuddingComputerUseHelper

@Test func previewCapturePreservesAlphaAndSizeLimits() {
  let configuration = WindowPreview.streamConfiguration(windowSize: CGSize(width: 1280, height: 960))
  #expect(configuration.pixelFormat == kCVPixelFormatType_32BGRA)
  #expect(!configuration.shouldBeOpaque)
  #expect(configuration.ignoreShadowsSingleWindow)
  #expect(configuration.width == 640)
  #expect(configuration.height == 480)
}

@Test func previewPNGPreservesTransparentAndSemitransparentPixels() throws {
  let context = try #require(CGContext(data: nil, width: 32, height: 32, bitsPerComponent: 8,
    bytesPerRow: 128, space: CGColorSpaceCreateDeviceRGB(),
    bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
  context.clear(CGRect(x: 0, y: 0, width: 32, height: 32))
  context.setFillColor(CGColor(red: 1, green: 0, blue: 0, alpha: 0.5))
  context.fill(CGRect(x: 8, y: 8, width: 16, height: 16))
  let image = try #require(context.makeImage())
  let data = try #require(WindowPreview.pngData(image))
  #expect(Array(data.prefix(8)) == [137, 80, 78, 71, 13, 10, 26, 10])
  let decoded = try #require(NSBitmapImageRep(data: data))
  #expect(decoded.hasAlpha)
  let corner = try #require(decoded.colorAt(x: 0, y: 0))
  #expect(corner.alphaComponent == 0)
  let center = try #require(decoded.colorAt(x: 16, y: 16)?.usingColorSpace(.deviceRGB))
  #expect(abs(center.alphaComponent - 0.5) < 0.01)
  #expect(center.redComponent > 0.99)
}

@Test func previewCropsResizedWindowContentAtRetinaScale() {
  let extent = CGRect(x: 0, y: 0, width: 640, height: 480)
  // A wide window is letterboxed vertically in the stream; Core Image uses bottom-left coordinates.
  let wide = WindowPreview.cropRect(contentRect: CGRect(x: 0, y: 30, width: 320, height: 160), scaleFactor: 2, extent: extent)
  #expect(wide == CGRect(x: 0, y: 100, width: 640, height: 320))
  let portrait = WindowPreview.cropRect(contentRect: CGRect(x: 100, y: 0, width: 120, height: 240), scaleFactor: 2, extent: extent)
  #expect(portrait == CGRect(x: 200, y: 0, width: 240, height: 480))
  let rounded = WindowPreview.cropRect(contentRect: CGRect(x: 0, y: 0, width: 320.1, height: 240.1), scaleFactor: 2, extent: extent)
  #expect(rounded == extent)
}
