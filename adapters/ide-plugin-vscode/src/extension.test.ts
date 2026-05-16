// Phase 8.6 of AUDIT_REMEDIATION_PLAN: a sanity test for the test harness
// itself. Real extension behaviour requires VS Code's TestRunner integration
// (vscode-test-electron) which is out-of-band; Jest with the mocked vscode
// module is enough to give us per-PR confidence that the TypeScript still
// type-checks and that pure utility code behaves.

import { window, workspace } from 'vscode';

describe('extension test harness', () => {
  test('mocked vscode.window is callable', () => {
    window.showInformationMessage('hello');
    expect(window.showInformationMessage).toHaveBeenCalledWith('hello');
  });

  test('mocked workspace.getConfiguration falls back to default', () => {
    const cfg = workspace.getConfiguration('codesec');
    const val = cfg.get('backendAddress', 'http://127.0.0.1:8080');
    expect(val).toBe('http://127.0.0.1:8080');
  });
});
