package io.codesec.plugin

import com.intellij.openapi.options.Configurable
import javax.swing.*
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets

class CodeSecConfigurable : Configurable {

    private var panel: JPanel? = null
    private val backendField   = JTextField(30)
    private val proxyField     = JTextField(30)
    private val mcpField       = JTextField(30)
    private val controlField   = JTextField(30)
    private val enabledCheckbox     = JCheckBox("CodeSec aktif")
    private val scanOnSaveCheckbox  = JCheckBox("Kaydetme sırasında tara")
    private val scanOnPasteCheckbox = JCheckBox("Yapıştırma sırasında tara")
    private val showInlineCheckbox  = JCheckBox("Inline uyarıları göster")
    private val timeoutSpinner = JSpinner(SpinnerNumberModel(5000, 500, 30000, 500))
    private val policyField    = JTextField(20)
    private val keyStatusLabel = JLabel()
    private val signOutBtn     = JButton("Forget API key")

    override fun getDisplayName(): String = "CodeSec"

    override fun createComponent(): JComponent {
        val p = JPanel(GridBagLayout())
        val c = GridBagConstraints().apply {
            anchor = GridBagConstraints.WEST
            insets = Insets(4, 4, 4, 4)
            fill = GridBagConstraints.HORIZONTAL
        }
        var row = 0

        fun addRow(label: String, comp: JComponent) {
            c.gridx = 0; c.gridy = row; c.gridwidth = 1; p.add(JLabel(label), c)
            c.gridx = 1; c.gridy = row++; p.add(comp, c)
        }

        addRow("Engine URL:",        backendField)
        addRow("Proxy URL:",         proxyField)
        addRow("MCP SSE URL:",       mcpField)
        addRow("Control plane URL:", controlField)

        // Key management — read-only status + sign-out button.
        c.gridx = 0; c.gridy = row; c.gridwidth = 1; p.add(JLabel("API key:"), c)
        val keyPanel = JPanel().apply {
            add(keyStatusLabel)
            add(signOutBtn)
        }
        c.gridx = 1; c.gridy = row++; p.add(keyPanel, c)
        signOutBtn.addActionListener {
            Onboarding.signOut(null)
            refreshKeyStatus()
        }

        c.gridx = 0; c.gridy = row; c.gridwidth = 2; p.add(enabledCheckbox, c); row++
        p.add(scanOnSaveCheckbox,  c.apply { gridy = row++ })
        p.add(scanOnPasteCheckbox, c.apply { gridy = row++ })
        p.add(showInlineCheckbox,  c.apply { gridy = row++ })
        c.gridwidth = 1

        addRow("Timeout (ms):",   timeoutSpinner)
        addRow("Policy profile:", policyField)

        panel = p
        reset()
        return p
    }

    override fun isModified(): Boolean {
        val s = CodeSecSettings.getInstance().state
        return backendField.text != s.backendAddress ||
                proxyField.text != s.proxyURL ||
                mcpField.text != s.mcpURL ||
                controlField.text != s.controlURL ||
                enabledCheckbox.isSelected != s.enabled ||
                scanOnSaveCheckbox.isSelected != s.scanOnSave ||
                scanOnPasteCheckbox.isSelected != s.scanOnPaste ||
                showInlineCheckbox.isSelected != s.showInlineWarnings ||
                (timeoutSpinner.value as Int) != s.timeoutMs ||
                policyField.text != s.policyProfile
    }

    override fun apply() {
        val s = CodeSecSettings.getInstance().state
        s.backendAddress     = backendField.text.trim()
        s.proxyURL           = proxyField.text.trim().trimEnd('/')
        s.mcpURL             = mcpField.text.trim()
        s.controlURL         = controlField.text.trim().trimEnd('/')
        s.enabled            = enabledCheckbox.isSelected
        s.scanOnSave         = scanOnSaveCheckbox.isSelected
        s.scanOnPaste        = scanOnPasteCheckbox.isSelected
        s.showInlineWarnings = showInlineCheckbox.isSelected
        s.timeoutMs          = timeoutSpinner.value as Int
        s.policyProfile      = policyField.text
    }

    override fun reset() {
        val s = CodeSecSettings.getInstance().state
        backendField.text   = s.backendAddress
        proxyField.text     = s.proxyURL
        mcpField.text       = s.mcpURL
        controlField.text   = s.controlURL
        enabledCheckbox.isSelected     = s.enabled
        scanOnSaveCheckbox.isSelected  = s.scanOnSave
        scanOnPasteCheckbox.isSelected = s.scanOnPaste
        showInlineCheckbox.isSelected  = s.showInlineWarnings
        timeoutSpinner.value           = s.timeoutMs
        policyField.text               = s.policyProfile
        refreshKeyStatus()
    }

    private fun refreshKeyStatus() {
        val has = CodeSecCredentialStore.hasApiKey()
        keyStatusLabel.text = if (has)
            "stored in OS keychain · run Tools → CodeSec → Sign Out to forget"
        else
            "NOT stored — run Tools → CodeSec → Start Onboarding"
        signOutBtn.isEnabled = has
    }
}
