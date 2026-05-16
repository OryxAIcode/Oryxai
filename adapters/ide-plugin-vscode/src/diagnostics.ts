import * as vscode from "vscode";
import { Finding } from "./types";

export class DiagnosticsManager implements vscode.Disposable {
  private collection: vscode.DiagnosticCollection;
  private findingsMap: Map<string, Finding[]> = new Map();

  constructor() {
    this.collection = vscode.languages.createDiagnosticCollection("codesec");
  }

  update(
    uri: vscode.Uri,
    findings: Finding[],
    doc: vscode.TextDocument
  ): void {
    this.findingsMap.set(uri.toString(), findings);

    if (findings.length === 0) {
      this.collection.delete(uri);
      return;
    }

    const diags: vscode.Diagnostic[] = findings.map((f) => {
      const range = this.findRange(doc, f.matched_text);
      const severity = this.mapSeverity(f.severity);

      const diag = new vscode.Diagnostic(
        range,
        `[CodeSec] ${f.description}`,
        severity
      );
      diag.source = "CodeSec";
      diag.code = f.rule_id;

      return diag;
    });

    this.collection.set(uri, diags);
  }

  getAllFindings(): Map<string, Finding[]> {
    return new Map(this.findingsMap);
  }

  private findRange(
    doc: vscode.TextDocument,
    matchedText: string
  ): vscode.Range {
    if (!matchedText) {
      return new vscode.Range(0, 0, 0, 0);
    }

    const text = doc.getText();
    const idx = text.indexOf(matchedText);

    if (idx >= 0) {
      const start = doc.positionAt(idx);
      const end = doc.positionAt(idx + matchedText.length);
      return new vscode.Range(start, end);
    }

    return new vscode.Range(0, 0, 0, 0);
  }

  private mapSeverity(severity: string): vscode.DiagnosticSeverity {
    switch (severity) {
      case "critical":
      case "high":
        return vscode.DiagnosticSeverity.Error;
      case "medium":
        return vscode.DiagnosticSeverity.Warning;
      case "low":
        return vscode.DiagnosticSeverity.Information;
      default:
        return vscode.DiagnosticSeverity.Hint;
    }
  }

  dispose(): void {
    this.collection.dispose();
    this.findingsMap.clear();
  }
}
