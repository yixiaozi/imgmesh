package com.imgmesh.app

import android.content.ContentValues
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Environment
import android.provider.MediaStore
import android.provider.OpenableColumns
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.min

data class PickedPhoto(val data: ByteArray, val name: String, val mime: String)

private const val MAX_PHOTO_BYTES = 50 * 1024 * 1024
private const val MAX_THUMB_BYTES = 2 * 1024 * 1024

fun loadPhoto(context: Context, uri: Uri, index: Int): PickedPhoto {
    val name = displayName(context, uri) ?: "photo-${index + 1}.jpg"
    val declared = context.contentResolver.getType(uri)
    context.contentResolver.openInputStream(uri).use { input ->
        if (input == null) throw ApiException("download_failed")
        val data = input.readBytes()
        if (data.size > MAX_PHOTO_BYTES) throw ApiException("too_large")
        val mime = normalizeMime(declared, data) ?: throw IllegalStateException("只能分享 JPEG、HEIC、PNG 或 WebP。")
        return PickedPhoto(data, name, mime)
    }
}

fun thumbnail(data: ByteArray): ByteArray? {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeByteArray(data, 0, data.size, bounds)
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
    var sample = 1
    while (max(bounds.outWidth, bounds.outHeight) / sample > 960) sample *= 2
    val decoded = BitmapFactory.decodeByteArray(data, 0, data.size, BitmapFactory.Options().apply { inSampleSize = sample })
        ?: return null
    val longEdge = max(decoded.width, decoded.height).toFloat()
    val scale = min(1f, 480f / longEdge)
    val width = (decoded.width * scale).toInt().coerceAtLeast(1)
    val height = (decoded.height * scale).toInt().coerceAtLeast(1)
    val scaled = if (width == decoded.width && height == decoded.height) decoded else Bitmap.createScaledBitmap(decoded, width, height, true)
    val out = ByteArrayOutputStream()
    val ok = scaled.compress(Bitmap.CompressFormat.JPEG, 70, out)
    if (scaled !== decoded) scaled.recycle()
    decoded.recycle()
    if (!ok) return null
    val jpeg = out.toByteArray()
    return jpeg.takeIf { it.size in 1..MAX_THUMB_BYTES }
}

fun saveToGallery(context: Context, photos: List<Pair<String, ByteArray>>) {
    photos.forEach { (name, bytes) ->
        val values = ContentValues().apply {
            put(MediaStore.Images.Media.DISPLAY_NAME, uniqueGalleryName(name))
            put(MediaStore.Images.Media.MIME_TYPE, mimeFromName(name, bytes))
            put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + "/imgmesh")
            put(MediaStore.Images.Media.IS_PENDING, 1)
        }
        val uri = context.contentResolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
            ?: throw IllegalStateException("没有写进相册。")
        context.contentResolver.openOutputStream(uri).use { output ->
            if (output == null) throw IllegalStateException("没有写进相册。")
            output.write(bytes)
        }
        values.clear()
        values.put(MediaStore.Images.Media.IS_PENDING, 0)
        context.contentResolver.update(uri, values, null, null)
    }
}

private fun displayName(context: Context, uri: Uri): String? {
    context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null).use { cursor ->
        if (cursor != null && cursor.moveToFirst()) {
            val index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (index >= 0) return cursor.getString(index)
        }
    }
    return null
}

private fun normalizeMime(declared: String?, data: ByteArray): String? {
    val named = when (declared?.lowercase()) {
        "image/jpg", "image/jpeg" -> "image/jpeg"
        "image/png" -> "image/png"
        "image/webp" -> "image/webp"
        "image/heic" -> "image/heic"
        "image/heif" -> "image/heif"
        else -> null
    }
    if (named != null) return named
    return sniff(data)
}

private fun sniff(data: ByteArray): String? {
    if (data.size >= 3 && data[0] == 0xFF.toByte() && data[1] == 0xD8.toByte()) return "image/jpeg"
    if (data.size >= 8 && data[0] == 0x89.toByte() && data[1] == 0x50.toByte()) return "image/png"
    if (data.size >= 12 && data.copyOfRange(0, 4).decodeToString() == "RIFF" && data.copyOfRange(8, 12).decodeToString() == "WEBP") {
        return "image/webp"
    }
    if (data.size >= 12 && data.copyOfRange(4, 8).decodeToString() == "ftyp") {
        val brand = data.copyOfRange(8, 12).decodeToString()
        if (brand.startsWith("hei") || brand == "mif1" || brand == "msf1") return "image/heic"
    }
    return null
}

private fun mimeFromName(name: String, data: ByteArray): String {
    val lower = name.lowercase()
    return when {
        lower.endsWith(".png") -> "image/png"
        lower.endsWith(".webp") -> "image/webp"
        lower.endsWith(".heic") || lower.endsWith(".heif") -> "image/heic"
        else -> sniff(data) ?: "image/jpeg"
    }
}

private fun uniqueGalleryName(name: String): String {
    val clean = name.substringAfterLast('/').ifBlank { "imgmesh.jpg" }
    return clean.substringBeforeLast('.') + "-" + System.currentTimeMillis() + "." + clean.substringAfterLast('.', "jpg")
}
