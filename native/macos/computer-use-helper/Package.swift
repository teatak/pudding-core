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
      targets: ["PuddingComputerUseHelper"]
    ),
    .executable(
      name: "PuddingComputerUseFixture",
      targets: ["PuddingComputerUseFixture"]
    )
  ],
  targets: [
    .executableTarget(
      name: "PuddingComputerUseHelper",
      exclude: ["Info.plist"]
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
