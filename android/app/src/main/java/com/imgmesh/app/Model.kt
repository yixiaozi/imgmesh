package com.imgmesh.app

import android.app.Application
import android.net.Uri
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.imgmesh.seal.Seal
import com.imgmesh.seal.makeShareUrl
import com.imgmesh.seal.parseShareUrl
import java.nio.charset.StandardCharsets
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

data class GalleryPhoto(
    val id: String,
    val name: String,
    val mime: String,
    val byteCount: Int,
    val nonce: String,
    val sha256: String?,
    val thumbId: String?,
    val thumbNonce: String?,
    val thumbBytes: Int,
    val preview: ByteArray?,
)

data class UiState(
    val ready: Boolean = false,
    val session: Session? = null,
    val draftName: String = "",
    val bio: String = "",
    val city: String = "",
    val contact: String = "",
    val peopleQuery: String = "",
    val people: List<Api.PublicUser> = emptyList(),
    val status: String = "",
    val error: String = "",
    val busy: Boolean = false,
    val link: String = "",
    val shares: List<LocalShare> = emptyList(),
    val openShare: LocalShare? = null,
    val gallery: List<GalleryPhoto> = emptyList(),
    val cards: Map<String, Api.PhotoCard> = emptyMap(),
    val activity: Api.Activity? = null,
    val selected: Int = -1,
)

class MeshModel(app: Application) : AndroidViewModel(app) {
    private val api = Api()
    private val vault = Vault(app)
    private val stateFlow = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = stateFlow

    init {
        restore()
    }

    fun updateDraft(name: String) = stateFlow.update { it.copy(draftName = name) }
    fun updateBio(value: String) = stateFlow.update { it.copy(bio = value) }
    fun updateCity(value: String) = stateFlow.update { it.copy(city = value) }
    fun updateContact(value: String) = stateFlow.update { it.copy(contact = value) }
    fun updateQuery(value: String) = stateFlow.update { it.copy(peopleQuery = value) }

    fun restore() {
        viewModelScope.launch {
            val session = withContext(Dispatchers.IO) {
                vault.loadSession() ?: runCatching { api.resume(vault.deviceId()) }.getOrNull()?.let { storeAccount(it) }
            }
            stateFlow.update {
                it.copy(
                    ready = true,
                    session = session,
                    bio = session?.bio.orEmpty(),
                    city = session?.city.orEmpty(),
                    contact = session?.contact.orEmpty(),
                    shares = vault.loadShares(),
                )
            }
        }
    }

    fun register() {
        val name = state.value.draftName.trim()
        if (name.isEmpty()) {
            stateFlow.update { it.copy(error = "先写一个名称。") }
            return
        }
        launchIo {
            val account = api.register(vault.deviceId(), name)
            val session = storeAccount(account)
            val note = if (account.alreadyRegistered) {
                "这台设备已经注册为 ${session.displayName}。名称保持第一次填写的。"
            } else {
                "已注册为 ${session.displayName}。"
            }
            stateFlow.update { it.copy(session = session, status = note, error = "", bio = session.bio, city = session.city, contact = session.contact) }
        }
    }

    fun saveProfile() {
        val session = state.value.session ?: return
        val name = state.value.draftName.trim().ifEmpty { session.displayName }
        launchIo {
            val user = api.saveProfile(session.token, name, state.value.bio, state.value.city, state.value.contact)
            val next = session.copy(
                displayName = user.displayName,
                bio = user.bio,
                city = user.city,
                contact = user.contact,
                appleLinked = user.appleLinked,
                deviceId = user.deviceId.ifEmpty { session.deviceId },
            )
            vault.saveSession(next)
            stateFlow.update { it.copy(session = next, status = "资料已保存。别人可以用名称、城市或联系方式找到你。", error = "") }
        }
    }

    fun findPeople() {
        val session = state.value.session ?: return
        val query = state.value.peopleQuery.trim()
        if (query.isEmpty()) {
            stateFlow.update { it.copy(people = emptyList()) }
            return
        }
        launchIo {
            val people = api.searchUsers(session.token, query)
            stateFlow.update {
                it.copy(people = people, status = if (people.isEmpty()) "没有找到这个人。" else "", error = "")
            }
        }
    }

    fun createShare(uris: List<Uri>, title: String) {
        val session = state.value.session
        if (session == null) {
            stateFlow.update { it.copy(error = "先登记这台设备的名称。") }
            return
        }
        if (!session.appleLinked) {
            stateFlow.update { it.copy(error = "创建分享还要绑定 Apple。安卓现在可以打开链接、看身份证，并把照片存进相册。") }
            return
        }
        if (uris.isEmpty()) return
        if (uris.size > 200) {
            stateFlow.update { it.copy(error = "一次最多 200 张。") }
            return
        }
        launchIo {
            stateFlow.update { it.copy(status = "正在读取照片", link = "") }
            val context = getApplication<Application>()
            val shareId = Seal.random(16)
            val master = Seal.random(32)
            val shareText = Seal.encode(shareId)
            val masterText = Seal.encode(master)
            val tag = Seal.encode(Seal.accessTag(master, shareId))
            api.createShare(session.token, shareText, tag)
            val fileKey = Seal.derive(master, shareId, Seal.INFO_FILE)
            val items = JSONArray()
            val blobs = linkedMapOf<String, ByteArray>()
            var total = 0
            uris.forEachIndexed { index, uri ->
                stateFlow.update { it.copy(status = "正在加密 ${index + 1}/${uris.size}") }
                val photo = loadPhoto(context, uri, index)
                total += photo.data.size
                if (total > 2L * 1024 * 1024 * 1024) throw IllegalStateException("这一组照片超过 2GB。")
                val objectId = Seal.random(16)
                val nonce = Seal.random(12)
                val objectText = Seal.encode(objectId)
                blobs[objectText] = Seal.encrypt(fileKey, nonce, Seal.aad(shareId, objectId), photo.data)
                val item = JSONObject()
                    .put("id", objectText)
                    .put("name", photo.name)
                    .put("mime", photo.mime)
                    .put("bytes", photo.data.size)
                    .put("nonce", Seal.encode(nonce))
                    .put("sha256", Seal.fingerprint(photo.data))
                thumbnail(photo.data)?.let { jpeg ->
                    val thumbId = Seal.random(16)
                    val thumbNonce = Seal.random(12)
                    val thumbText = Seal.encode(thumbId)
                    blobs[thumbText] = Seal.encrypt(fileKey, thumbNonce, Seal.aad(shareId, thumbId), jpeg)
                    item.put(
                        "thumb",
                        JSONObject().put("id", thumbText).put("bytes", jpeg.size).put("nonce", Seal.encode(thumbNonce)),
                    )
                }
                items.put(item)
            }
            val manifestPlain = JSONObject()
                .put("v", 1)
                .put("title", title.ifBlank { "旅行照片" })
                .put("items", items)
                .toString()
                .toByteArray(StandardCharsets.UTF_8)
            val manifestId = Seal.random(16)
            val manifestNonce = Seal.random(12)
            val manifestText = Seal.encode(manifestId)
            val manifestKey = Seal.derive(master, shareId, Seal.INFO_MANIFEST)
            blobs[manifestText] = Seal.encrypt(manifestKey, manifestNonce, Seal.aad(shareId, manifestId), manifestPlain)
            val objects = JSONArray()
            for (i in 0 until items.length()) {
                val item = items.getJSONObject(i)
                objects.put(objectPayload(item.getString("id"), "original", item.getInt("bytes"), blobs.getValue(item.getString("id")).size))
                if (item.has("thumb")) {
                    val thumb = item.getJSONObject("thumb")
                    objects.put(objectPayload(thumb.getString("id"), "thumb", thumb.getInt("bytes"), blobs.getValue(thumb.getString("id")).size))
                }
            }
            objects.put(objectPayload(manifestText, "manifest", manifestPlain.size, blobs.getValue(manifestText).size))
            stateFlow.update { it.copy(status = "正在上传") }
            val plans = api.reserve(session.token, shareText, objects)
            val completed = JSONArray()
            plans.forEach { plan ->
                val payload = blobs[plan.id] ?: throw IllegalStateException("上传计划对不上。")
                val parts = JSONArray()
                if (!plan.ready) {
                    var offset = 0
                    val chunk = if (plan.partSize > 0) plan.partSize else payload.size
                    plan.parts.forEach { part ->
                        val end = if (part.partNumber == 0) payload.size else minOf(offset + maxOf(chunk, 1), payload.size)
                        val slice = payload.copyOfRange(offset, end)
                        val etag = api.put(part.url, slice)
                        parts.put(JSONObject().put("partNumber", part.partNumber).put("etag", etag))
                        if (part.partNumber > 0) offset = end
                    }
                }
                completed.put(JSONObject().put("id", plan.id).put("byteSize", payload.size).put("parts", parts))
            }
            api.complete(session.token, shareText, completed, manifestText, Seal.encode(manifestNonce))
            val claims = JSONArray()
            for (i in 0 until items.length()) {
                claims.put(JSONObject().put("sha256", items.getJSONObject(i).getString("sha256")).put("kind", "shared"))
            }
            runCatching { api.claim(session.token, shareText, tag, claims) }
            val local = LocalShare(shareText, masterText, title.ifBlank { "旅行照片" }, "sent", items.length(), System.currentTimeMillis())
            val shares = listOf(local) + vault.loadShares().filter { it.id != local.id }
            vault.saveShares(shares)
            stateFlow.update {
                it.copy(shares = shares, link = makeShareUrl(shareText, masterText), status = "链接已经准备好。发出去时要带上井号后面的密钥。")
            }
        }
    }

    fun accept(text: String) {
        val parsed = parseShareUrl(text)
        if (parsed == null) {
            stateFlow.update { it.copy(error = "这个链接里没有密钥。请粘贴完整链接，井号后面的部分也要保留。") }
            return
        }
        val existing = vault.loadShares()
        val shares = if (existing.any { it.id == parsed.id }) existing else {
            listOf(LocalShare(parsed.id, parsed.masterKey, "收到的照片", "received", 0, System.currentTimeMillis())) + existing
        }
        vault.saveShares(shares)
        val share = shares.first { it.id == parsed.id }
        stateFlow.update { it.copy(shares = shares, error = "") }
        open(share)
    }

    fun open(share: LocalShare) {
        val session = state.value.session
        if (session == null) {
            stateFlow.update { it.copy(error = "先登记这台设备的名称。") }
            return
        }
        launchIo {
            stateFlow.update { it.copy(openShare = share, gallery = emptyList(), cards = emptyMap(), activity = null, selected = -1, status = "正在解密") }
            val shareId = Seal.decode(share.id)
            val master = Seal.decode(share.masterKey)
            val access = Seal.encode(Seal.accessTag(master, shareId))
            val listed = api.manifest(session.token, share.id, access)
            val manifestKey = Seal.derive(master, shareId, Seal.INFO_MANIFEST)
            val plain = Seal.decrypt(
                manifestKey,
                Seal.decode(listed.nonce),
                Seal.aad(shareId, Seal.decode(listed.id)),
                api.download(listed.url),
            )
            val manifest = JSONObject(String(plain, StandardCharsets.UTF_8))
            val items = manifest.getJSONArray("items")
            val fileKey = Seal.derive(master, shareId, Seal.INFO_FILE)
            val photos = mutableListOf<GalleryPhoto>()
            for (index in 0 until items.length()) {
                val item = items.getJSONObject(index)
                val thumb = if (item.has("thumb") && !item.isNull("thumb")) item.getJSONObject("thumb") else null
                var preview: ByteArray? = null
                if (thumb != null) {
                    runCatching {
                        val cipher = api.download(api.objectUrl(session.token, share.id, thumb.getString("id"), access))
                        preview = Seal.decrypt(
                            fileKey,
                            Seal.decode(thumb.getString("nonce")),
                            Seal.aad(shareId, Seal.decode(thumb.getString("id"))),
                            cipher,
                        )
                    }
                }
                photos += GalleryPhoto(
                    id = item.getString("id"),
                    name = item.optString("name", "照片"),
                    mime = item.optString("mime", "image/jpeg"),
                    byteCount = item.optInt("bytes"),
                    nonce = item.getString("nonce"),
                    sha256 = item.optString("sha256").ifBlank { null },
                    thumbId = thumb?.optString("id"),
                    thumbNonce = thumb?.optString("nonce"),
                    thumbBytes = thumb?.optInt("bytes") ?: 0,
                    preview = preview,
                )
            }
            val titled = share.copy(title = manifest.optString("title", share.title), photoCount = photos.size)
            val shares = vault.loadShares().map { if (it.id == titled.id) titled else it }
            vault.saveShares(shares)
            val ids = photos.mapNotNull { it.sha256 }.distinct()
            val cards = runCatching { api.photoCards(session.token, ids).associateBy { it.id } }.getOrDefault(emptyMap())
            runCatching { api.event(session.token, share.id, access, "view") }
            val activity = if (share.role == "sent") runCatching { api.activity(session.token, share.id) }.getOrNull() else null
            stateFlow.update {
                it.copy(
                    shares = shares,
                    openShare = titled,
                    gallery = photos,
                    cards = cards,
                    activity = activity,
                    status = "${photos.size} 张照片",
                    error = "",
                )
            }
        }
    }

    fun select(index: Int) {
        stateFlow.update { it.copy(selected = index) }
        if (index >= 0) showOriginal(index)
    }

    fun step(delta: Int) {
        val count = state.value.gallery.size
        if (count == 0 || state.value.selected < 0) return
        select((state.value.selected + delta + count) % count)
    }

    fun closeGallery() {
        stateFlow.update { it.copy(openShare = null, gallery = emptyList(), selected = -1, cards = emptyMap(), activity = null) }
    }

    fun saveSelected() {
        val index = state.value.selected
        if (index < 0) return
        savePhotos(listOf(index))
    }

    fun saveAll() {
        if (state.value.gallery.isEmpty()) return
        savePhotos(state.value.gallery.indices.toList())
    }

    fun remove(share: LocalShare) {
        val session = state.value.session
        launchIo {
            if (session != null) {
                runCatching {
                    val shareId = Seal.decode(share.id)
                    val access = Seal.encode(Seal.accessTag(Seal.decode(share.masterKey), shareId))
                    api.event(session.token, share.id, access, "remove")
                }
            }
            val shares = vault.loadShares().filter { it.id != share.id }
            vault.saveShares(shares)
            stateFlow.update { it.copy(shares = shares, openShare = null, gallery = emptyList(), status = "已从这台手机的列表移除。相册里保存过的照片还在。") }
        }
    }

    fun revoke(share: LocalShare) {
        val session = state.value.session ?: return
        launchIo {
            api.revoke(session.token, share.id)
            val shares = vault.loadShares().filter { it.id != share.id }
            vault.saveShares(shares)
            stateFlow.update {
                it.copy(shares = shares, openShare = null, gallery = emptyList(), status = "链接已经失效。对方相册里已经保存的照片还在。")
            }
        }
    }

    private fun showOriginal(index: Int) {
        val session = state.value.session ?: return
        val share = state.value.openShare ?: return
        val photo = state.value.gallery.getOrNull(index) ?: return
        if (photo.mime == "image/heic" || photo.mime == "image/heif") return
        viewModelScope.launch(Dispatchers.IO) {
            runCatching {
                val bytes = original(session, share, photo)
                val sha = Seal.fingerprint(bytes)
                val current = state.value.gallery.toMutableList()
                if (index !in current.indices) return@runCatching
                current[index] = current[index].copy(preview = bytes, sha256 = sha)
                val cards = refreshCards(session.token, current)
                stateFlow.update { it.copy(gallery = current, cards = cards) }
            }
        }
    }

    private fun savePhotos(indexes: List<Int>) {
        val session = state.value.session ?: return
        val share = state.value.openShare ?: return
        launchIo {
            stateFlow.update { it.copy(status = "正在保存到相册") }
            val context = getApplication<Application>()
            val files = mutableListOf<Pair<String, ByteArray>>()
            val claims = JSONArray()
            val gallery = state.value.gallery.toMutableList()
            indexes.forEach { index ->
                val photo = gallery[index]
                val bytes = original(session, share, photo)
                val sha = Seal.fingerprint(bytes)
                gallery[index] = photo.copy(sha256 = sha)
                files += photo.name to bytes
                claims.put(JSONObject().put("sha256", sha).put("kind", "saved"))
            }
            saveToGallery(context, files)
            val shareId = Seal.decode(share.id)
            val access = Seal.encode(Seal.accessTag(Seal.decode(share.masterKey), shareId))
            runCatching { api.event(session.token, share.id, access, "save") }
            runCatching { api.claim(session.token, share.id, access, claims) }
            val cards = refreshCards(session.token, gallery)
            stateFlow.update { it.copy(gallery = gallery, cards = cards, status = "已经存进相册，并写进照片身份证。", error = "") }
        }
    }

    private fun original(session: Session, share: LocalShare, photo: GalleryPhoto): ByteArray {
        val shareId = Seal.decode(share.id)
        val master = Seal.decode(share.masterKey)
        val access = Seal.encode(Seal.accessTag(master, shareId))
        val fileKey = Seal.derive(master, shareId, Seal.INFO_FILE)
        val cipher = api.download(api.objectUrl(session.token, share.id, photo.id, access))
        return Seal.decrypt(fileKey, Seal.decode(photo.nonce), Seal.aad(shareId, Seal.decode(photo.id)), cipher)
    }

    private fun refreshCards(token: String, photos: List<GalleryPhoto>): Map<String, Api.PhotoCard> {
        val ids = photos.mapNotNull { it.sha256 }.distinct()
        if (ids.isEmpty()) return emptyMap()
        return runCatching { api.photoCards(token, ids).associateBy { it.id } }.getOrDefault(state.value.cards)
    }

    private fun storeAccount(account: Api.Account): Session {
        val session = Session(
            token = account.token,
            displayName = account.user.displayName,
            bio = account.user.bio,
            city = account.user.city,
            contact = account.user.contact,
            appleLinked = account.user.appleLinked,
            deviceId = account.user.deviceId.ifEmpty { vault.deviceId() },
        )
        vault.saveSession(session)
        return session
    }

    private fun objectPayload(id: String, kind: String, plain: Int, cipher: Int): JSONObject {
        return JSONObject()
            .put("id", id)
            .put("kind", kind)
            .put("plaintextBytes", plain)
            .put("ciphertextBytes", cipher)
    }

    private fun launchIo(block: suspend () -> Unit) {
        viewModelScope.launch {
            stateFlow.update { it.copy(busy = true, error = "") }
            try {
                withContext(Dispatchers.IO) { block() }
            } catch (error: Exception) {
                val text = when (error) {
                    is ApiException -> apiMessage(error)
                    else -> error.message?.takeIf { message -> message.any { it.code > 127 } } ?: apiMessage(error)
                }
                stateFlow.update { it.copy(error = text) }
            } finally {
                stateFlow.update { it.copy(busy = false) }
            }
        }
    }
}
