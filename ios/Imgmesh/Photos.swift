import Foundation
import ImageIO
import Photos
import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

struct PickedPhoto {
    var data: Data
    var name: String
    var mime: String
}

enum PhotoIO {
    static let maxPhotoBytes = 50 * 1024 * 1024
    static let maxThumbBytes = 2 * 1024 * 1024

    static func load(_ provider: NSItemProvider) async throws -> PickedPhoto {
        let preferred: [(String, String)] = [
            (UTType.heic.identifier, "image/heic"),
            (UTType.heif.identifier, "image/heif"),
            (UTType.jpeg.identifier, "image/jpeg"),
            (UTType.png.identifier, "image/png"),
            (UTType.webP.identifier, "image/webp"),
        ]
        for (type, mime) in preferred where provider.hasItemConformingToTypeIdentifier(type) {
            let url = try await copyFile(provider, type: type)
            defer { try? FileManager.default.removeItem(at: url) }
            let data = try Data(contentsOf: url)
            guard data.count <= maxPhotoBytes else { throw APIError.transport("有照片超过 50MB。") }
            return PickedPhoto(data: data, name: url.lastPathComponent, mime: mime)
        }
        throw APIError.transport("只能分享 JPEG、HEIC、PNG 或 WebP。")
    }

    static func thumbnail(from data: Data) -> Data? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 480,
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else { return nil }
        let out = NSMutableData()
        guard let dest = CGImageDestinationCreateWithData(out, UTType.jpeg.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(dest, image, [kCGImageDestinationLossyCompressionQuality: 0.7] as CFDictionary)
        guard CGImageDestinationFinalize(dest) else { return nil }
        let jpeg = out as Data
        return jpeg.count <= maxThumbBytes ? jpeg : nil
    }

    static func saveToLibrary(_ images: [Data]) async throws {
        let current = PHPhotoLibrary.authorizationStatus(for: .addOnly)
        let status = current == .notDetermined ? await PHPhotoLibrary.requestAuthorization(for: .addOnly) : current
        guard status == .authorized || status == .limited else {
            throw APIError.transport("没有相册写入权限。")
        }
        for data in images {
            try await PHPhotoLibrary.shared().performChanges {
                let request = PHAssetCreationRequest.forAsset()
                request.addResource(with: .photo, data: data, options: nil)
            }
        }
    }

    private static func copyFile(_ provider: NSItemProvider, type: String) async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            provider.loadFileRepresentation(forTypeIdentifier: type) { url, error in
                if let error {
                    continuation.resume(throwing: error)
                    return
                }
                guard let url else {
                    continuation.resume(throwing: APIError.transport("没有读到照片。"))
                    return
                }
                let dest = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + "-" + url.lastPathComponent)
                do {
                    try FileManager.default.copyItem(at: url, to: dest)
                    continuation.resume(returning: dest)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}

struct PhotoPicker: UIViewControllerRepresentable {
    var onPick: ([NSItemProvider]) -> Void

    func makeUIViewController(context: Context) -> PHPickerViewController {
        var config = PHPickerConfiguration()
        config.filter = .images
        config.selectionLimit = 200
        config.preferredAssetRepresentationMode = .current
        let picker = PHPickerViewController(configuration: config)
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ uiViewController: PHPickerViewController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(onPick: onPick) }

    final class Coordinator: NSObject, PHPickerViewControllerDelegate {
        var onPick: ([NSItemProvider]) -> Void
        init(onPick: @escaping ([NSItemProvider]) -> Void) { self.onPick = onPick }
        func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
            picker.dismiss(animated: true)
            onPick(results.map(\.itemProvider))
        }
    }
}
