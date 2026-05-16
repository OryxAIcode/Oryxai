// Phase 8.6 of AUDIT_REMEDIATION_PLAN: minimal vscode mock for Jest.
//
// Only the surface the extension imports is shimmed; expand as new tests
// arrive. Each helper records calls so tests can assert.

export const window = {
  showInformationMessage: jest.fn(),
  showWarningMessage: jest.fn(),
  showErrorMessage: jest.fn(),
  createOutputChannel: jest.fn(() => ({
    appendLine: jest.fn(),
    show: jest.fn(),
    dispose: jest.fn(),
  })),
  createStatusBarItem: jest.fn(() => ({
    text: '',
    tooltip: '',
    command: '',
    show: jest.fn(),
    dispose: jest.fn(),
  })),
  createWebviewPanel: jest.fn(),
};

export const workspace = {
  getConfiguration: jest.fn((_section?: string) => ({
    get: jest.fn((key: string, fallback?: unknown) => fallback),
    update: jest.fn(),
  })),
  onDidSaveTextDocument: jest.fn(),
  onDidChangeTextDocument: jest.fn(),
};

export const commands = {
  registerCommand: jest.fn(),
  executeCommand: jest.fn(),
};

export const languages = {
  createDiagnosticCollection: jest.fn(() => ({
    set: jest.fn(),
    delete: jest.fn(),
    clear: jest.fn(),
    dispose: jest.fn(),
  })),
};

export class Range {
  constructor(
    public startLine: number,
    public startChar: number,
    public endLine: number,
    public endChar: number,
  ) {}
}

export class Diagnostic {
  constructor(
    public range: Range,
    public message: string,
    public severity: number,
  ) {}
}

export enum DiagnosticSeverity {
  Error = 0,
  Warning = 1,
  Information = 2,
  Hint = 3,
}

export const StatusBarAlignment = { Left: 1, Right: 2 };
export const ViewColumn = { One: 1, Two: 2 };
export const ConfigurationTarget = { Global: 1, Workspace: 2, WorkspaceFolder: 3 };
