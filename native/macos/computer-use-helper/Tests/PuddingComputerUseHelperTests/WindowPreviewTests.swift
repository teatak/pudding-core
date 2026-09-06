import CoreGraphics
import Testing
@testable import PuddingComputerUseHelper

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
