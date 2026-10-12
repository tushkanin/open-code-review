// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package com.alibaba.opencodereview.idea.services

import com.alibaba.opencodereview.idea.model.ConfigEntry
import java.io.File
import java.nio.file.Files
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assume.assumeTrue
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CliServiceAuthTest {
    @Test
    fun `draft roots use the effective original home`() = withDraft { env, check ->
        env.remove("CLAUDE_CONFIG_DIR")
        env.remove("CODEX_HOME")
        val record = check(true)
        assertEquals(File(env.getValue("HOME"), ".claude").path, record["claude"]!!.jsonPrimitive.content)
        assertEquals(File(env.getValue("HOME"), ".codex").path, record["codex"]!!.jsonPrimitive.content)
    }

    @Test
    fun `draft roots preserve explicit overrides`() = withDraft { env, check ->
        env["CLAUDE_CONFIG_DIR"] = "/explicit/claude"
        env["CODEX_HOME"] = "/explicit/codex"
        val record = check(true)
        assertEquals(env["CLAUDE_CONFIG_DIR"], record["claude"]!!.jsonPrimitive.content)
        assertEquals(env["CODEX_HOME"], record["codex"]!!.jsonPrimitive.content)
    }

    @Test
    fun `failed draft checks keep API configuration and clean the temporary home`() = withDraft { env, check ->
        env["OCR_AUTH_FAIL"] = "1"
        check(false)
    }

    @Test
    fun `real CLI authentication works through the changed draft launch path`() {
        assumeTrue(System.getenv("OCR_IDE_AUTH_SMOKE") == "1")
        val original = ShellEnv.env().toMap()
        val savedPath = File(original["HOME"] ?: System.getProperty("user.home"), ".opencodereview/config.json")
        val saved = savedPath.takeIf(File::exists)?.readText()
        withDraft { env, check ->
            val audit = env.getValue("OCR_AUTH_AUDIT")
            env.clear()
            env.putAll(original)
            env["OCR_AUTH_AUDIT"] = audit
            env["OCR_AUTH_LIVE"] = "1"
            env["OCR_AUTH_CLAUDE"] = System.getenv("OCR_AUTH_CLAUDE") ?: "claude"
            val record = check(true)
            assertEquals(original["CLAUDE_CONFIG_DIR"] ?: File(original.getValue("HOME"), ".claude").path,
                record["claude"]!!.jsonPrimitive.content)
            assertEquals(original["CODEX_HOME"] ?: File(original.getValue("HOME"), ".codex").path,
                record["codex"]!!.jsonPrimitive.content)
        }
        assertEquals(saved, savedPath.takeIf(File::exists)?.readText())
    }

    private fun withDraft(block: (MutableMap<String, String>, (Boolean) -> kotlinx.serialization.json.JsonObject) -> Unit) {
        assumeTrue(!System.getProperty("os.name").startsWith("Windows"))
        val dir = Files.createTempDirectory("ocr-auth-test-").toFile()
        val originalHome = System.getProperty("user.home")
        val cache = ShellEnv::class.java.getDeclaredField("cachedEnv").apply { isAccessible = true }
        val originalCache = cache.get(ShellEnv)
        try {
            val savedHome = File(dir, "saved-home").apply { mkdirs() }
            val savedPath = File(savedHome, ".opencodereview/config.json")
            savedPath.parentFile.mkdirs()
            val saved = """{"provider":"openai","providers":{"openai":{"api_key":"saved-key","model":"saved-model"}}}"""
            savedPath.writeText(saved)
            System.setProperty("user.home", savedHome.path)
            val audit = File(dir, "audit.json")
            val env = System.getenv().toMutableMap().apply {
                put("HOME", savedHome.path)
                put("OCR_AUTH_AUDIT", audit.path)
                remove("OCR_AUTH_LIVE")
                remove("OCR_AUTH_FAIL")
            }
            cache.set(ShellEnv, env)
            val binary = File(dir, "ocr")
            binary.writeText(fixture)
            assertTrue(binary.setExecutable(true))
            val config = ConfigService(CliService(binary.path))
            block(env) { ok ->
                val result = config.testWithEntries(listOf(ConfigEntry("providers.openai.model", "draft-model")))
                assertEquals(ok, result.first, result.second)
                val record = Json.parseToJsonElement(audit.readText()).jsonObject
                val home = File(record["home"]!!.jsonPrimitive.content)
                assertTrue(home != savedHome)
                assertEquals(listOf(".opencodereview"), record["files"]!!.jsonArray.map { it.jsonPrimitive.content })
                assertEquals(listOf("config.json"), record["configFiles"]!!.jsonArray.map { it.jsonPrimitive.content })
                val provider = record["draft"]!!.jsonObject["providers"]!!.jsonObject["openai"]!!.jsonObject
                assertEquals("draft-model", provider["model"]!!.jsonPrimitive.content)
                assertEquals("saved-key", provider["api_key"]!!.jsonPrimitive.content)
                assertEquals(home.path, record["userprofile"]!!.jsonPrimitive.content)
                assertFalse(home.exists())
                assertEquals(saved, savedPath.readText())
                record
            }
        } finally {
            cache.set(ShellEnv, originalCache)
            System.setProperty("user.home", originalHome)
            dir.deleteRecursively()
        }
    }

    private val fixture = """
        #!/usr/bin/env node
        const fs = require('fs');
        const path = require('path');
        const cp = require('child_process');
        if (process.argv.slice(2).join(' ') !== 'llm test') process.exit(2);
        const home = process.env.HOME;
        const draft = JSON.parse(fs.readFileSync(path.join(home, '.opencodereview/config.json'), 'utf8'));
        if (process.env.OCR_AUTH_LIVE === '1') {
          const result = cp.spawnSync(process.env.OCR_AUTH_CLAUDE, [
            '-p', '--model', 'sonnet', '--output-format', 'json', '--tools', '',
            '--strict-mcp-config', '--safe-mode', '--permission-mode', 'dontAsk', '--no-session-persistence',
            'Reply with exactly OCR_IDE_AUTH_OK.',
          ], { encoding: 'utf8', timeout: 120000 });
          if (result.status !== 0) { process.stderr.write(result.stderr || String(result.error)); process.exit(3); }
          const response = JSON.parse(result.stdout);
          if (response.is_error || !response.result.includes('OCR_IDE_AUTH_OK')) process.exit(4);
        }
        fs.writeFileSync(process.env.OCR_AUTH_AUDIT, JSON.stringify({
          home, claude: process.env.CLAUDE_CONFIG_DIR, codex: process.env.CODEX_HOME,
          userprofile: process.env.USERPROFILE, draft,
          files: fs.readdirSync(home), configFiles: fs.readdirSync(path.join(home, '.opencodereview')),
        }));
        if (draft.providers.openai.model !== 'draft-model') process.exit(5);
        if (process.env.OCR_AUTH_FAIL === '1') { process.stderr.write('Error: draft failure'); process.exit(6); }
    """.trimIndent() + "\n"
}
