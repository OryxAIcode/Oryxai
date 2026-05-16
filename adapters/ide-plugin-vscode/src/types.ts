export interface Finding {
  rule_id: string;
  rule_name: string;
  category: string;
  severity: string;
  confidence: number;
  description: string;
  matched_text: string;
}

export interface ScanResult {
  decision: string;
  risk_score: number;
  safe_content?: string;
  triggered_rules: Finding[];
  explanation: string;
  trace_id: string;
  processing_time_ms: number;
  diff_mode?: boolean;
  scanned_lines?: number;
}

export interface HealthResponse {
  status: string;
  version?: string;
}

/** Response from POST /api/v1/scan/file */
export interface ScanFileResponse {
  filename: string;
  findings: unknown[];
  count: number;
}

/** Response from POST /api/v1/scan/repo */
export interface ScanRepoResponse {
  findings: Array<{
    filename: string;
    finding: Record<string, unknown>;
  }>;
  count: number;
}

/** Response from POST /api/v1/scan/diff */
export interface ScanDiffResponse {
  filename: string;
  new_findings: unknown[];
  count: number;
  diff_risk?: {
    score: number;
    severity: string;
    regressions: unknown[];
    features: Record<string, unknown>;
  };
}

/** Response from POST /api/v1/tool/authorize */
export interface ToolAuthResult {
  decision: string;
  triggered_rules: Finding[];
  explanation: string;
  trace_id: string;
  capability_token?: string;
}
