package com.imgmesh.app

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKeys
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject

data class Session(
    val token: String,
    val displayName: String,
    val bio: String,
    val city: String,
    val contact: String,
    val appleLinked: Boolean,
    val deviceId: String,
)

data class LocalShare(
    val id: String,
    val masterKey: String,
    val title: String,
    val role: String,
    val photoCount: Int,
    val createdAt: Long,
)

class Vault(context: Context) {
    private val prefs = EncryptedSharedPreferences.create(
        "imgmesh",
        MasterKeys.getOrCreate(MasterKeys.AES256_GCM_SPEC),
        context,
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    fun deviceId(): String {
        val current = prefs.getString("device", "").orEmpty()
        if (current.matches(Regex("^[A-Za-z0-9_-]{16,64}$"))) return current
        val created = UUID.randomUUID().toString()
        prefs.edit().putString("device", created).apply()
        return created
    }

    fun loadSession(): Session? {
        val raw = prefs.getString("session", null) ?: return null
        return try {
            val item = JSONObject(raw)
            Session(
                token = item.getString("token"),
                displayName = item.optString("displayName"),
                bio = item.optString("bio"),
                city = item.optString("city"),
                contact = item.optString("contact"),
                appleLinked = item.optBoolean("appleLinked"),
                deviceId = item.optString("deviceId"),
            ).takeIf { it.token.isNotEmpty() && it.displayName.isNotEmpty() }
        } catch (_: Exception) {
            null
        }
    }

    fun saveSession(session: Session?) {
        val editor = prefs.edit()
        if (session == null) {
            editor.remove("session")
        } else {
            editor.putString(
                "session",
                JSONObject()
                    .put("token", session.token)
                    .put("displayName", session.displayName)
                    .put("bio", session.bio)
                    .put("city", session.city)
                    .put("contact", session.contact)
                    .put("appleLinked", session.appleLinked)
                    .put("deviceId", session.deviceId)
                    .toString(),
            )
        }
        editor.apply()
    }

    fun loadShares(): List<LocalShare> {
        val raw = prefs.getString("shares", null) ?: return emptyList()
        return try {
            val array = JSONArray(raw)
            (0 until array.length()).map { index ->
                val item = array.getJSONObject(index)
                LocalShare(
                    id = item.getString("id"),
                    masterKey = item.getString("masterKey"),
                    title = item.optString("title", "照片"),
                    role = item.optString("role", "received"),
                    photoCount = item.optInt("photoCount"),
                    createdAt = item.optLong("createdAt"),
                )
            }
        } catch (_: Exception) {
            emptyList()
        }
    }

    fun saveShares(shares: List<LocalShare>) {
        val array = JSONArray()
        shares.forEach { share ->
            array.put(
                JSONObject()
                    .put("id", share.id)
                    .put("masterKey", share.masterKey)
                    .put("title", share.title)
                    .put("role", share.role)
                    .put("photoCount", share.photoCount)
                    .put("createdAt", share.createdAt),
            )
        }
        prefs.edit().putString("shares", array.toString()).apply()
    }
}
