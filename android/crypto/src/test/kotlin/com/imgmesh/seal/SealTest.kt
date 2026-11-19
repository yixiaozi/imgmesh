package com.imgmesh.seal

import java.io.File
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class SealTest {
    @Test
    fun matchesFrozenVector() {
        val root = generateSequence(File("").absoluteFile) { it.parentFile }
            .first { File(it, "testdata/vector.json").isFile }
        val text = File(root, "testdata/vector.json").readText()
        fun field(name: String): String {
            val match = Regex(""""$name"\s*:\s*"([^"]+)"""").find(text)
            return match?.groupValues?.get(1) ?: error("missing $name")
        }
        val master = Seal.decode(field("masterKey"))
        val share = Seal.decode(field("shareId"))
        val objectId = Seal.decode(field("objectId"))
        val nonce = Seal.decode(field("nonce"))
        val plain = java.util.Base64.getDecoder().decode(field("plaintext"))
        assertArrayEquals(Seal.decode(field("fileKey")), Seal.derive(master, share, Seal.INFO_FILE))
        assertArrayEquals(Seal.decode(field("manifestKey")), Seal.derive(master, share, Seal.INFO_MANIFEST))
        assertArrayEquals(Seal.decode(field("accessTag")), Seal.accessTag(master, share))
        val aad = Seal.aad(share, objectId)
        assertEquals(field("aad"), Seal.encode(aad))
        val cipher = Seal.encrypt(Seal.derive(master, share, Seal.INFO_FILE), nonce, aad, plain)
        assertEquals(field("ciphertext"), Seal.encode(cipher))
        assertArrayEquals(plain, Seal.decrypt(Seal.derive(master, share, Seal.INFO_FILE), nonce, aad, cipher))
    }

    @Test
    fun parseKeepsFragmentKey() {
        val link = parseShareUrl("https://imgmesh.com/s/IiIiIiIiIiIiIiIiIiIiIg#k=ERERERERERERERERERERERERERERERERERERERERERE")
        assertEquals("IiIiIiIiIiIiIiIiIiIiIg", link?.id)
        assertEquals("ERERERERERERERERERERERERERERERERERERERERERE", link?.masterKey)
        assertEquals(null, parseShareUrl("https://imgmesh.com/s/IiIiIiIiIiIiIiIiIiIiIg"))
        assertEquals(null, parseShareUrl("https://example.com/s/IiIiIiIiIiIiIiIiIiIiIg#k=abc"))
    }
}
