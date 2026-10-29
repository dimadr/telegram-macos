// swift-tools-version:5.5
import PackageDescription
import Foundation

let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../..").standardized.path
let package = Package(
    name: "MieruTransport",
    platforms: [.macOS(.v10_13)],
    products: [.library(name: "MieruTransport", targets: ["MieruTransport"])],
    dependencies: [.package(name: "MtProtoKit", path: "../../submodules/telegram-ios/submodules/MtProtoKit")],
    targets: [
        .systemLibrary(name: "MieruABI"),
        .target(name: "MieruTransport", dependencies: ["MieruABI", .product(name: "MtProtoKit", package: "MtProtoKit")], linkerSettings: [
            .unsafeFlags(["-L\(root)/native/mieru/build", "-lmieru"]),
            .linkedFramework("Security"), .linkedFramework("CoreFoundation"), .linkedLibrary("resolv")
        ]),
        .testTarget(name: "MieruTransportTests", dependencies: ["MieruTransport", "MieruABI"])
    ]
)
