package com.imgmesh.app

import java.util.concurrent.TimeUnit
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject

class ApiException(val code: String) : Exception(code)

class Api(private val baseUrl: String = "https://imgmesh.com") {
    private val http = OkHttpClient.Builder()
        .connectTimeout(20, TimeUnit.SECONDS)
        .readTimeout(2, TimeUnit.MINUTES)
        .writeTimeout(2, TimeUnit.MINUTES)
        .build()
    private val jsonType = "application/json".toMediaType()
    private val bytesType = "application/octet-stream".toMediaType()

    data class Account(val token: String, val alreadyRegistered: Boolean, val user: User)
    data class User(
        val id: String,
        val displayName: String,
        val bio: String,
        val city: String,
        val contact: String,
        val appleLinked: Boolean,
        val deviceId: String,
    )

    data class PublicUser(
        val id: String,
        val displayName: String,
        val bio: String,
        val city: String,
        val contact: String,
        val sharedCount: Int,
        val savedCount: Int,
    )

    data class NamedUser(val id: String, val displayName: String, val city: String)
    data class PhotoHolder(
        val id: String,
        val displayName: String,
        val city: String,
        val shared: Boolean,
        val saved: Boolean,
        val from: NamedUser?,
    )

    data class PhotoCard(
        val id: String,
        val sharedBy: List<NamedUser>,
        val holders: List<PhotoHolder>,
        val savers: List<PhotoHolder>,
    )

    data class UploadPart(val partNumber: Int, val url: String)
    data class UploadPlan(
        val id: String,
        val ready: Boolean,
        val partSize: Int,
        val parts: List<UploadPart>,
    )

    data class ListedManifest(val id: String, val nonce: String, val url: String)
    data class ActivityRow(val displayName: String, val client: String, val count: Int, val savedAt: String)
    data class Activity(val viewed: List<ActivityRow>, val downloaded: List<ActivityRow>, val phones: List<ActivityRow>)

    fun register(deviceId: String, displayName: String): Account {
        val body = JSONObject()
            .put("deviceId", deviceId)
            .put("displayName", displayName)
            .put("platform", "web")
        val response = call("/v1/register", "POST", null, null, body.toString())
        return account(response)
    }

    fun resume(deviceId: String): Account {
        val body = JSONObject().put("deviceId", deviceId)
        return account(call("/v1/devices/session", "POST", null, null, body.toString()))
    }

    fun saveProfile(token: String, displayName: String, bio: String, city: String, contact: String): User {
        val body = JSONObject()
            .put("displayName", displayName)
            .put("bio", bio)
            .put("city", city)
            .put("contact", contact)
        val response = call("/v1/me", "PATCH", token, null, body.toString())
        return user(JSONObject(response).getJSONObject("user"))
    }

    fun searchUsers(token: String, query: String): List<PublicUser> {
        val response = call("/v1/users?q=" + java.net.URLEncoder.encode(query, "UTF-8"), "GET", token, null, null)
        val users = JSONObject(response).optJSONArray("users") ?: JSONArray()
        return (0 until users.length()).map { index ->
            val item = users.getJSONObject(index)
            PublicUser(
                id = item.optString("id"),
                displayName = item.optString("displayName"),
                bio = item.optString("bio"),
                city = item.optString("city"),
                contact = item.optString("contact"),
                sharedCount = item.optInt("sharedCount"),
                savedCount = item.optInt("savedCount"),
            )
        }
    }

    fun photoCards(token: String, ids: List<String>): List<PhotoCard> {
        if (ids.isEmpty()) return emptyList()
        val query = ids.joinToString("&") { "id=$it" }
        val response = call("/v1/photos?$query", "GET", token, null, null)
        val photos = JSONObject(response).optJSONArray("photos") ?: JSONArray()
        return (0 until photos.length()).map { index -> card(photos.getJSONObject(index)) }
    }

    fun createShare(token: String, id: String, accessTag: String) {
        val body = JSONObject().put("id", id).put("accessTag", accessTag)
        call("/v1/shares", "POST", token, null, body.toString())
    }

    fun reserve(token: String, shareId: String, objects: JSONArray): List<UploadPlan> {
        val body = JSONObject().put("objects", objects)
        val response = call("/v1/shares/$shareId/uploads", "POST", token, null, body.toString())
        val uploads = JSONObject(response).optJSONArray("uploads") ?: JSONArray()
        return (0 until uploads.length()).map { index ->
            val item = uploads.getJSONObject(index)
            val parts = item.optJSONArray("parts") ?: JSONArray()
            UploadPlan(
                id = item.getString("id"),
                ready = item.optBoolean("ready"),
                partSize = item.optInt("partSize"),
                parts = (0 until parts.length()).map { partIndex ->
                    val part = parts.getJSONObject(partIndex)
                    UploadPart(part.getInt("partNumber"), part.getString("url"))
                },
            )
        }
    }

    fun put(url: String, data: ByteArray): String {
        val request = Request.Builder().url(url).put(data.toRequestBody(bytesType)).build()
        http.newCall(request).execute().use { response ->
            if (!response.isSuccessful) throw ApiException(errorCode(response.body?.string()) ?: "upload_failed")
            return response.header("ETag").orEmpty()
        }
    }

    fun complete(token: String, shareId: String, objects: JSONArray, manifestId: String, nonce: String) {
        val body = JSONObject()
            .put("objects", objects)
            .put("manifestObjectId", manifestId)
            .put("manifestNonce", nonce)
        call("/v1/shares/$shareId/uploads/complete", "POST", token, null, body.toString())
    }

    fun claim(token: String, shareId: String, access: String, items: JSONArray) {
        if (items.length() == 0) return
        call("/v1/shares/$shareId/photos", "POST", token, access, JSONObject().put("items", items).toString())
    }

    fun revoke(token: String, shareId: String) {
        call("/v1/shares/$shareId", "DELETE", token, null, null)
    }

    fun manifest(token: String, shareId: String, access: String): ListedManifest {
        val item = JSONObject(call("/v1/shares/$shareId/manifest", "GET", token, access, null))
        return ListedManifest(item.getString("id"), item.getString("nonce"), item.getString("url"))
    }

    fun objectUrl(token: String, shareId: String, objectId: String, access: String): String {
        return JSONObject(call("/v1/shares/$shareId/objects/$objectId", "GET", token, access, null)).getString("url")
    }

    fun download(url: String): ByteArray {
        val request = Request.Builder().url(url).get().build()
        http.newCall(request).execute().use { response ->
            if (!response.isSuccessful) throw ApiException("download_failed")
            return response.body?.bytes() ?: ByteArray(0)
        }
    }

    fun event(token: String, shareId: String, access: String, kind: String) {
        val body = JSONObject().put("kind", kind).put("client", "web")
        call("/v1/shares/$shareId/events", "POST", token, access, body.toString())
    }

    fun activity(token: String, shareId: String): Activity? {
        return try {
            val item = JSONObject(call("/v1/shares/$shareId/events", "GET", token, null, null))
            Activity(
                viewed = rows(item.optJSONArray("viewed")),
                downloaded = rows(item.optJSONArray("downloaded")),
                phones = rows(item.optJSONArray("phones")),
            )
        } catch (error: ApiException) {
            if (error.code == "not_found") null else throw error
        }
    }

    private fun rows(array: JSONArray?): List<ActivityRow> {
        if (array == null) return emptyList()
        return (0 until array.length()).map { index ->
            val item = array.getJSONObject(index)
            ActivityRow(
                displayName = item.optString("displayName"),
                client = item.optString("client"),
                count = item.optInt("count"),
                savedAt = item.optString("savedAt"),
            )
        }
    }

    private fun card(item: JSONObject): PhotoCard {
        return PhotoCard(
            id = item.optString("id"),
            sharedBy = names(item.optJSONArray("sharedBy")),
            holders = holders(item.optJSONArray("holders")),
            savers = holders(item.optJSONArray("savers")),
        )
    }

    private fun names(array: JSONArray?): List<NamedUser> {
        if (array == null) return emptyList()
        return (0 until array.length()).map { index -> named(array.getJSONObject(index)) }
    }

    private fun holders(array: JSONArray?): List<PhotoHolder> {
        if (array == null) return emptyList()
        return (0 until array.length()).map { index ->
            val item = array.getJSONObject(index)
            PhotoHolder(
                id = item.optString("id"),
                displayName = item.optString("displayName"),
                city = item.optString("city"),
                shared = item.optBoolean("shared"),
                saved = item.optBoolean("saved"),
                from = if (item.has("from") && !item.isNull("from")) named(item.getJSONObject("from")) else null,
            )
        }
    }

    private fun named(item: JSONObject) = NamedUser(item.optString("id"), item.optString("displayName"), item.optString("city"))

    private fun account(raw: String): Account {
        val item = JSONObject(raw)
        return Account(item.getString("token"), item.optBoolean("alreadyRegistered"), user(item.getJSONObject("user")))
    }

    private fun user(item: JSONObject) = User(
        id = item.optString("id"),
        displayName = item.optString("displayName"),
        bio = item.optString("bio"),
        city = item.optString("city"),
        contact = item.optString("contact"),
        appleLinked = item.optBoolean("appleLinked"),
        deviceId = item.optString("deviceId"),
    )

    private fun call(path: String, method: String, token: String?, access: String?, body: String?): String {
        val builder = Request.Builder().url(baseUrl.trimEnd('/') + path)
        if (token != null) builder.header("Authorization", "Bearer $token")
        if (access != null) builder.header("X-Imgmesh-Access", access)
        val requestBody = body?.toRequestBody(jsonType)
        builder.method(method, if (method == "GET" || method == "DELETE") null else requestBody ?: "{}".toRequestBody(jsonType))
        http.newCall(builder.build()).execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw ApiException(errorCode(text) ?: "request_failed")
            return text
        }
    }

    private fun errorCode(text: String?): String? {
        if (text.isNullOrBlank()) return null
        return try {
            JSONObject(text).optString("error").ifBlank { null }
        } catch (_: Exception) {
            null
        }
    }
}

fun apiMessage(error: Throwable): String {
    val code = (error as? ApiException)?.code ?: return "没有完成，可以再试一次。"
    return when (code) {
        "quota" -> "空间已经用完。"
        "too_large" -> "照片太多，或单张超过 50MB。"
        "unauthorized" -> "登录已失效，请重新登记。"
        "access_denied" -> "链接不完整，或密钥对不上。"
        "revoked" -> "这份分享已经撤销。"
        "expired" -> "这份分享已经过期。"
        "not_found" -> "没有找到。"
        "name_taken" -> "这个名称已经有人用了，换一个。"
        "name_required" -> "先登记一个名称。"
        "apple_required" -> "创建分享还需要绑定 Apple。安卓目前可以查看和保存。"
        "bad_request" -> "填写的内容不符合要求。"
        else -> "请求没有完成。"
    }
}
