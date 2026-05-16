package io.codesec.plugin

import com.intellij.codeInspection.*
import com.intellij.openapi.diagnostic.Logger
import com.intellij.psi.PsiFile

class CodeSecInspection : LocalInspectionTool() {

    private val logger = Logger.getInstance(CodeSecInspection::class.java)
    private val client = CodeSecClient()

    override fun checkFile(file: PsiFile, manager: InspectionManager, isOnTheFly: Boolean): Array<ProblemDescriptor>? {
        val settings = CodeSecSettings.getInstance().state
        if (!settings.enabled || !settings.showInlineWarnings) return null

        val content = file.text ?: return null
        val lang = detectLanguage(file)
        val filePath = file.virtualFile?.path ?: file.name

        val result: ScanResult
        try {
            result = client.scanFullFile(content, lang, filePath)
        } catch (e: Exception) {
            logger.warn("CodeSec scan failed for $filePath: ${e.message}")
            return null
        }

        if (result.findings.isEmpty()) return null

        val problems = mutableListOf<ProblemDescriptor>()
        val doc = file.viewProvider.document ?: return null

        for (finding in result.findings) {
            val lineNo = findMatchingLine(content, finding.matched_value)
            if (lineNo < 0 || lineNo >= doc.lineCount) continue

            val startOffset = doc.getLineStartOffset(lineNo)
            val endOffset = doc.getLineEndOffset(lineNo)
            val textRange = com.intellij.openapi.util.TextRange(startOffset, endOffset)
            val element = file.findElementAt(startOffset) ?: continue

            val severity = when (finding.severity) {
                "critical", "high" -> ProblemHighlightType.GENERIC_ERROR
                "medium" -> ProblemHighlightType.WARNING
                else -> ProblemHighlightType.WEAK_WARNING
            }

            val desc = manager.createProblemDescriptor(
                element,
                textRange.shiftLeft(element.textRange.startOffset),
                "[CodeSec] ${finding.rule_id}: ${finding.message}",
                severity,
                isOnTheFly
            )
            problems.add(desc)
        }

        return problems.toTypedArray()
    }

    private fun findMatchingLine(content: String, matchedValue: String): Int {
        if (matchedValue.isBlank()) return 0
        val lines = content.lines()
        val searchLower = matchedValue.trim().lowercase()
        for ((i, line) in lines.withIndex()) {
            if (line.lowercase().contains(searchLower)) return i
        }
        return 0
    }

    private fun detectLanguage(file: PsiFile): String {
        val ext = file.virtualFile?.extension?.lowercase() ?: ""
        return when (ext) {
            "py" -> "python"
            "go" -> "go"
            "js" -> "javascript"
            "ts" -> "typescript"
            "jsx" -> "javascript"
            "tsx" -> "typescript"
            "java" -> "java"
            "kt", "kts" -> "kotlin"
            "rs" -> "rust"
            "rb" -> "ruby"
            "php" -> "php"
            "c", "h" -> "c"
            "cpp", "cc", "cxx", "hpp" -> "cpp"
            "cs" -> "csharp"
            "swift" -> "swift"
            "sh", "bash" -> "shell"
            "yaml", "yml" -> "yaml"
            "json" -> "json"
            "xml" -> "xml"
            "sql" -> "sql"
            "tf", "hcl" -> "terraform"
            else -> ext
        }
    }
}
