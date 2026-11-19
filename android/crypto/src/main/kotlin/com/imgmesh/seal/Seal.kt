package com.imgmesh.seal

import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

class SealException(message: String) : Exception(message)

object Seal {
    const val INFO_FILE = "imgmesh-file-v1"
    const val INFO_MANIFEST = "imgmesh-manifest-v1"

    private const val ACCESS_PREFIX = "imgmesh-access-v1"

    fun random(count: Int): ByteArray {
        val out = ByteArray(count)
        SecureRandom().nextBytes(out)
        return out
    }

    fun encode(data: ByteArray): String =
        Base64.getUrlEncoder().withoutPadding().encodeToString(data)

    fun decode(value: String): ByteArray {
        val padded = value + "=".repeat((4 - value.length % 4) % 4)
        val data = Base64.getUrlDecoder().decode(padded)
        if (encode(data) != value) throw SealException("编码不是规范的 base64url")
        return data
    }

    fun derive(master: ByteArray, shareId: ByteArray, info: String): ByteArray {
        if (master.size != 32 || shareId.size != 16) throw SealException("密钥长度不对")
        return hkdf(master, shareId, info.toByteArray(Charsets.UTF_8), 32)
    }

    fun accessTag(master: ByteArray, shareId: ByteArray): ByteArray {
        if (master.size != 32 || shareId.size != 16) throw SealException("密钥长度不对")
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(master, "HmacSHA256"))
        mac.update(ACCESS_PREFIX.toByteArray(Charsets.UTF_8))
        mac.update(shareId)
        return mac.doFinal()
    }

    fun aad(shareId: ByteArray, objectId: ByteArray): ByteArray {
        if (shareId.size != 16 || objectId.size != 16) throw SealException("标识长度不对")
        return shareId + objectId
    }

    fun encrypt(key: ByteArray, nonce: ByteArray, aad: ByteArray, plain: ByteArray): ByteArray {
        if (key.size != 32 || nonce.size != 12) throw SealException("加密参数长度不对")
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(aad)
        return cipher.doFinal(plain)
    }

    fun decrypt(key: ByteArray, nonce: ByteArray, aad: ByteArray, ciphertext: ByteArray): ByteArray {
        if (key.size != 32 || nonce.size != 12 || ciphertext.size < 16) throw SealException("解密参数长度不对")
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(aad)
        return cipher.doFinal(ciphertext)
    }

    fun fingerprint(data: ByteArray): String {
        val digest = java.security.MessageDigest.getInstance("SHA-256").digest(data)
        return encode(digest)
    }

    private fun hkdf(ikm: ByteArray, salt: ByteArray, info: ByteArray, length: Int): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(salt, "HmacSHA256"))
        val prk = mac.doFinal(ikm)
        mac.init(SecretKeySpec(prk, "HmacSHA256"))
        mac.update(info)
        mac.update(1)
        return mac.doFinal().copyOf(length)
    }
}

data class ShareLink(val id: String, val masterKey: String)

fun parseShareUrl(text: String): ShareLink? {
    val trimmed = text.trim()
    val hash = trimmed.indexOf('#')
    if (hash < 0) return null
    val before = trimmed.substring(0, hash)
    val fragment = trimmed.substring(hash + 1)
    val key = fragment.split('&').firstNotNullOfOrNull { pair ->
        val bits = pair.split('=', limit = 2)
        if (bits.size == 2 && bits[0] == "k" && bits[1].isNotEmpty()) bits[1] else null
    } ?: return null
    val hostAndPath = before.substringAfter("://", "")
    if (hostAndPath.isEmpty()) return null
    val host = hostAndPath.substringBefore('/').substringBefore(':').lowercase()
    if (host != "imgmesh.com" && host != "www.imgmesh.com") return null
    val path = hostAndPath.substringAfter('/', "")
    val parts = path.split('/').filter { it.isNotEmpty() }
    if (parts.size != 2 || parts[0] != "s") return null
    return ShareLink(parts[1], key)
}

fun makeShareUrl(id: String, masterKey: String): String = "https://imgmesh.com/s/$id#k=$masterKey"
