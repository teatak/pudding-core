// swift-tools-version: 6.0

import PackageDescription

let package = Package(
  name: "PuddingComputerUseHelper",
  platforms: [
    .macOS(.v14)
  ],
  products: [
    .executable(
      name: "PuddingComputerUseHelper",
      targets: ["PuddingComputerUseHelperCLI"]
    ),
    .executable(
      name: "PuddingComputerUseFixture",
      targets: ["PuddingComputerUseFixture"]
    )
  ],
  targets: [
    // Tests link the implementation without the product's @main entry point.
    .target(
      name: "PuddingComputerUseHelper",
      exclude: ["Info.plist"]
    ),
    .executableTarget(
      name: "PuddingComputerUseHelperCLI",
      dependencies: ["PuddingComputerUseHelper"]
    ),
    .executableTarget(
      name: "PuddingComputerUseFixture",
      exclude: ["Info.plist"]
    ),
    .testTarget(
      name: "PuddingComputerUseHelperTests",
      dependencies: ["PuddingComputerUseHelper"]
    ),
  ]
)
