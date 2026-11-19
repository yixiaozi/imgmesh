package com.imgmesh.app

import android.graphics.BitmapFactory
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts.PickMultipleVisualMedia
import androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlin.math.max

private val Ocean = Color(0xFF0E6B6E)
private val Ink = Color(0xFF1A2332)
private val Paper = Color(0xFFF3F6F5)

@Composable
fun ImgmeshApp(model: MeshModel = viewModel()) {
    val state by model.state.collectAsState()
    MaterialTheme(colorScheme = lightColorScheme(primary = Ocean, background = Paper, surface = Color.White)) {
        Box(Modifier.fillMaxSize().background(Paper).padding(16.dp)) {
            when {
                !state.ready -> Text("正在打开", color = Ink)
                state.session == null -> RegisterScreen(state, model)
                state.openShare != null -> GalleryScreen(state, model)
                else -> HomeScreen(state, model)
            }
        }
    }
}

@Composable
private fun RegisterScreen(state: UiState, model: MeshModel) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("imgmesh", style = MaterialTheme.typography.headlineMedium, color = Ink)
        Text("每台设备只能注册一次，名称不能和别人重复。", color = Ink)
        OutlinedTextField(state.draftName, model::updateDraft, label = { Text("名称，例如小林") }, modifier = Modifier.fillMaxWidth())
        Button(onClick = model::register, enabled = !state.busy, colors = ButtonDefaults.buttonColors(containerColor = Ocean)) { Text("注册") }
        if (state.error.isNotEmpty()) Text(state.error, color = Color(0xFF8A3D32))
    }
}

@Composable
private fun HomeScreen(state: UiState, model: MeshModel) {
    val session = state.session ?: return
    val clipboard = LocalClipboardManager.current
    var pasted by remember { mutableStateOf("") }
    var title by remember { mutableStateOf("") }
    val pick = rememberLauncherForActivityResult(PickMultipleVisualMedia(200)) { uris ->
        model.createShare(uris, title)
    }
    Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("imgmesh", style = MaterialTheme.typography.headlineMedium, color = Ink)
        Text(session.displayName, fontWeight = FontWeight.SemiBold, color = Ink)
        Text("这台安卓设备用现有接口登记。创建分享仍要 Apple 绑定，所以这里可以查看、找人、看身份证，并把照片存进相册。", color = Ink)
        OutlinedTextField(state.draftName, model::updateDraft, label = { Text("名称") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(state.bio, model::updateBio, label = { Text("介绍") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(state.city, model::updateCity, label = { Text("城市") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(state.contact, model::updateContact, label = { Text("联系方式") }, modifier = Modifier.fillMaxWidth())
        Button(onClick = model::saveProfile, enabled = !state.busy, colors = ButtonDefaults.buttonColors(containerColor = Ocean)) { Text("保存资料") }
        Text("找人", fontWeight = FontWeight.SemiBold, color = Ink)
        OutlinedTextField(state.peopleQuery, model::updateQuery, label = { Text("名称、城市或联系方式") }, modifier = Modifier.fillMaxWidth())
        Button(onClick = model::findPeople, enabled = !state.busy) { Text("查找") }
        state.people.forEach { person ->
            Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(Color.White).padding(10.dp)) {
                Text(person.displayName, fontWeight = FontWeight.SemiBold)
                Text("${person.city.ifBlank { "未填城市" }} · ${person.contact.ifBlank { "未填联系方式" }}")
                Text(person.bio.ifBlank { "还没有介绍" })
                Text("分享了 ${person.sharedCount} 张，保存了 ${person.savedCount} 张")
            }
        }
        Text("打开一份分享", fontWeight = FontWeight.SemiBold, color = Ink)
        OutlinedTextField(pasted, { pasted = it }, label = { Text("粘贴完整链接") }, modifier = Modifier.fillMaxWidth())
        Button(onClick = { model.accept(pasted) }, enabled = pasted.isNotBlank()) { Text("打开") }
        Text("分享", fontWeight = FontWeight.SemiBold, color = Ink)
        OutlinedTextField(title, { title = it }, label = { Text("标题，例如三亚") }, modifier = Modifier.fillMaxWidth())
        Button(
            onClick = { pick.launch(PickVisualMediaRequest(PickVisualMedia.ImageOnly)) },
            enabled = !state.busy && session.appleLinked,
            colors = ButtonDefaults.buttonColors(containerColor = Ocean),
        ) { Text("选择照片") }
        if (!session.appleLinked) Text("这台设备还没有绑定 Apple，不能创建新的分享。", color = Ink)
        if (state.link.isNotEmpty()) {
            SelectionContainer { Text(state.link, color = Ink) }
            TextButton(onClick = { clipboard.setText(AnnotatedString(state.link)) }) { Text("复制链接") }
        }
        Text("我的链接", fontWeight = FontWeight.SemiBold, color = Ink)
        if (state.shares.isEmpty()) Text("还没有分享。")
        state.shares.forEach { share ->
            Row(Modifier.fillMaxWidth().clickable { model.open(share) }.padding(vertical = 6.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(share.title, color = Ink)
                Text(if (share.role == "sent") "发出" else "收到", color = Ocean)
            }
        }
        if (state.status.isNotEmpty()) Text(state.status, color = Ink)
        if (state.error.isNotEmpty()) Text(state.error, color = Color(0xFF8A3D32))
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun GalleryScreen(state: UiState, model: MeshModel) {
    val share = state.openShare ?: return
    Column(Modifier.fillMaxSize()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = model::closeGallery) { Text("返回") }
            Text(share.title, style = MaterialTheme.typography.titleLarge, color = Ink, modifier = Modifier.weight(1f))
        }
        Text(state.status, color = Ink)
        if (state.error.isNotEmpty()) Text(state.error, color = Color(0xFF8A3D32))
        if (state.selected >= 0) {
            IdentityCard(state, model, Modifier.weight(1f).verticalScroll(rememberScrollState()))
            return
        }
        LazyVerticalGrid(GridCells.Adaptive(108.dp), modifier = Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            itemsIndexed(state.gallery, key = { _, photo -> photo.id }) { index, photo ->
                Box(Modifier.height(108.dp).clip(RoundedCornerShape(8.dp)).clickable { model.select(index) }) {
                    PhotoImage(photo.preview, Modifier.fillMaxSize())
                    Text(
                        tileLabel(photo, state.cards),
                        color = Color.White,
                        modifier = Modifier.align(Alignment.BottomStart).padding(4.dp).background(Ocean, RoundedCornerShape(99.dp)).padding(horizontal = 6.dp, vertical = 2.dp),
                    )
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = model::saveAll, enabled = state.gallery.isNotEmpty() && !state.busy, colors = ButtonDefaults.buttonColors(containerColor = Ocean)) { Text("保存到相册") }
            TextButton(onClick = { model.remove(share) }) { Text("从列表移除") }
            if (share.role == "sent" && state.session?.appleLinked == true) {
                TextButton(onClick = { model.revoke(share) }) { Text("撤销链接") }
            }
        }
        state.activity?.let { activity ->
            Text("谁查看过：" + names(activity.viewed), color = Ink)
            Text("谁下载过：" + names(activity.downloaded), color = Ink)
            Text("在这些手机里：" + activity.phones.joinToString("、") { it.displayName.ifBlank { "未命名" } }.ifBlank { "还没有" }, color = Ink)
        }
    }
}

@Composable
private fun IdentityCard(state: UiState, model: MeshModel, modifier: Modifier = Modifier) {
    val photo = state.gallery.getOrNull(state.selected) ?: return
    val card = photo.sha256?.let { state.cards[it] }
    Column(modifier.fillMaxWidth().padding(top = 8.dp).background(Color.White, RoundedCornerShape(12.dp)).padding(12.dp)) {
        PhotoImage(photo.preview, Modifier.fillMaxWidth().height(180.dp))
        Row(Modifier.horizontalScroll(rememberScrollState()).padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            state.gallery.forEachIndexed { index, item ->
                Box(
                    Modifier.size(48.dp).clip(RoundedCornerShape(6.dp)).border(2.dp, if (index == state.selected) Ocean else Color.Transparent, RoundedCornerShape(6.dp)).clickable { model.select(index) },
                ) { PhotoImage(item.preview, Modifier.fillMaxSize()) }
            }
        }
        Text("身份证", style = MaterialTheme.typography.titleMedium, color = Ink)
        Text(photo.sha256?.let(::shortId) ?: "打开原图后生成", color = Ink)
        SelectionContainer { Text(photo.sha256.orEmpty(), color = Color(0xFF3D4A5C)) }
        if (card == null) {
            Text("这张照片还没有身份证记录。", color = Ink)
        } else {
            Text(if (card.sharedBy.isEmpty()) "还没分享" else "已分享 · " + card.sharedBy.joinToString("、") { it.displayName }, color = Ink)
            Text("谁有这张", fontWeight = FontWeight.SemiBold)
            card.holders.forEach { holder -> Text(holderLine(holder), color = Ink) }
            Text("谁保存了", fontWeight = FontWeight.SemiBold)
            if (card.savers.isEmpty()) Text("还没有人保存。")
            card.savers.forEach { saver -> Text(saverLine(saver), color = Ink) }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = { model.step(-1) }) { Text("上一张") }
            TextButton(onClick = { model.step(1) }) { Text("下一张") }
            Button(onClick = model::saveSelected, enabled = !state.busy, colors = ButtonDefaults.buttonColors(containerColor = Ocean)) { Text("保存这张") }
            TextButton(onClick = { model.select(-1) }) { Text("关闭") }
        }
    }
}

@Composable
private fun PhotoImage(bytes: ByteArray?, modifier: Modifier) {
    if (bytes == null) {
        Box(modifier.background(Color(0xFFE6EEED)))
        return
    }
    val bitmap = remember(bytes) { decodePreview(bytes) }
    if (bitmap == null) Box(modifier.background(Color(0xFFE6EEED)))
    else Image(bitmap, contentDescription = null, modifier = modifier, contentScale = ContentScale.Crop)
}

private fun decodePreview(bytes: ByteArray) = try {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
    var sample = 1
    while (bounds.outWidth > 0 && max(bounds.outWidth, bounds.outHeight) / sample > 1600) sample *= 2
    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })?.asImageBitmap()
} catch (_: Exception) {
    null
}

private fun tileLabel(photo: GalleryPhoto, cards: Map<String, Api.PhotoCard>): String {
    val card = photo.sha256?.let { cards[it] } ?: return "照片"
    val count = card.holders.size
    return when {
        card.sharedBy.isNotEmpty() && count > 0 -> "已分享 · $count 人有"
        card.sharedBy.isNotEmpty() -> "已分享"
        count > 0 -> "$count 人有"
        else -> "未分享"
    }
}

private fun holderLine(holder: Api.PhotoHolder): String {
    val action = if (holder.saved) "保存了" else "分享了"
    val from = holder.from
    return if (holder.saved && from != null && from.id != holder.id) {
        "${holder.displayName} $action，保存自 ${from.displayName}"
    } else {
        "${holder.displayName} $action"
    }
}

private fun saverLine(saver: Api.PhotoHolder): String {
    val from = saver.from
    return if (from != null && from.id != saver.id) "${saver.displayName}，保存自 ${from.displayName}" else "${saver.displayName}，自己留下的"
}

private fun names(rows: List<Api.ActivityRow>): String =
    rows.joinToString("、") { it.displayName.ifBlank { "未命名" } }.ifBlank { "还没有" }

private fun shortId(id: String): String = "IM-${id.take(4)} ${id.drop(4).take(4)}"
