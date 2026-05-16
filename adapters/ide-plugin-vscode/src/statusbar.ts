import * as vscode from "vscode";

export class StatusBarManager implements vscode.Disposable {
  private item: vscode.StatusBarItem;
  private enabled: boolean = true;

  constructor() {
    this.item = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Right,
      100
    );
    this.item.command = "codesec.showDashboard";
    this.setIdle();
    this.item.show();
  }

  setConnected(connected: boolean): void {
    if (connected) {
      this.setIdle();
    } else {
      this.item.text = "$(debug-disconnect) CodeSec";
      this.item.tooltip = "CodeSec: Backend not connected";
      this.item.backgroundColor = new vscode.ThemeColor(
        "statusBarItem.warningBackground"
      );
    }
  }

  setEnabled(enabled: boolean): void {
    this.enabled = enabled;
    if (enabled) {
      this.setIdle();
    } else {
      this.item.text = "$(circle-slash) CodeSec OFF";
      this.item.tooltip = "CodeSec: Disabled";
      this.item.backgroundColor = undefined;
    }
  }

  setScanning(): void {
    this.item.text = "$(sync~spin) CodeSec";
    this.item.tooltip = "CodeSec: Scanning...";
    this.item.backgroundColor = undefined;
  }

  setClean(): void {
    this.item.text = "$(check) CodeSec";
    this.item.tooltip = "CodeSec: No issues found";
    this.item.backgroundColor = undefined;
  }

  setWarning(count: number): void {
    this.item.text = `$(warning) CodeSec (${count})`;
    this.item.tooltip = `CodeSec: ${count} warning(s) found`;
    this.item.backgroundColor = new vscode.ThemeColor(
      "statusBarItem.warningBackground"
    );
  }

  setBlocked(count: number): void {
    this.item.text = `$(error) CodeSec (${count})`;
    this.item.tooltip = `CodeSec: ${count} critical issue(s) — BLOCKED`;
    this.item.backgroundColor = new vscode.ThemeColor(
      "statusBarItem.errorBackground"
    );
  }

  setError(): void {
    this.item.text = "$(error) CodeSec";
    this.item.tooltip = "CodeSec: Scan failed";
    this.item.backgroundColor = new vscode.ThemeColor(
      "statusBarItem.errorBackground"
    );
  }

  private setIdle(): void {
    this.item.text = "$(shield) CodeSec";
    this.item.tooltip = "CodeSec: Ready";
    this.item.backgroundColor = undefined;
  }

  dispose(): void {
    this.item.dispose();
  }
}
