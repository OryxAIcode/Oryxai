import * as http from "http";
import * as https from "https";
import {
  ScanResult,
  HealthResponse,
  ScanFileResponse,
  ScanRepoResponse,
  ScanDiffResponse,
  ToolAuthResult,
} from "./types";

/** İstemci 2xx dışı yanıtlarda (404 fallback için) bu hatayı fırlatır. */
export class CodeSecHttpError extends Error {
  readonly statusCode: number;
  readonly responseBody: string;

  constructor(statusCode: number, responseBody: string) {
    super(`HTTP ${statusCode}`);
    this.name = "CodeSecHttpError";
    this.statusCode = statusCode;
    this.responseBody = responseBody;
  }
}

export class CodeSecClient {
  private baseURL: string;
  private isUnixSocket: boolean;
  private socketPath: string;
  private timeoutMs: number;
  private apiKey: string;

  constructor(address: string, timeoutMs = 5000, apiKey = "") {
    this.timeoutMs = timeoutMs;
    this.apiKey = apiKey;

    if (address.startsWith("/") || address.startsWith("unix://")) {
      this.isUnixSocket = true;
      this.socketPath = address.replace("unix://", "");
      this.baseURL = "http://localhost";
    } else {
      this.isUnixSocket = false;
      this.socketPath = "";
      this.baseURL = address.replace(/\/$/, "");
    }
  }

  setApiKey(apiKey: string): void {
    this.apiKey = apiKey;
  }

  async healthCheck(): Promise<boolean> {
    try {
      const resp = await this.getFirstOk<HealthResponse>([
        "/api/v1/health",
        "/health",
      ]);
      return resp.status === "ok" || resp.status === "healthy";
    } catch {
      return false;
    }
  }

  async scanCompletion(
    content: string,
    language?: string,
    diffAgainst?: string,
    filePath?: string,
    metadata?: Record<string, string>
  ): Promise<ScanResult> {
    const body: Record<string, unknown> = {
      content,
      content_type: "code",
    };
    if (language) body.language = language;
    if (diffAgainst) body.old_content = diffAgainst;
    if (filePath) body.file_path = filePath;
    if (metadata && Object.keys(metadata).length > 0) {
      body.metadata = metadata;
    }

    return this.postFirstOk<ScanResult>(
      ["/api/v1/egress", "/api/v1/egress/check"],
      body
    );
  }

  /**
   * Tool firewall authorization (workspace boundary checks use metadata.workspace_roots).
   */
  async authorizeTool(
    toolName: string,
    parameters: Record<string, unknown>,
    metadata?: Record<string, string>
  ): Promise<ToolAuthResult> {
    const body: Record<string, unknown> = {
      tool_name: toolName,
      parameters,
    };
    if (metadata && Object.keys(metadata).length > 0) {
      body.metadata = metadata;
    }
    return this.post<ToolAuthResult>("/api/v1/tool/authorize", body);
  }

  async scanPrompt(content: string): Promise<ScanResult> {
    return this.postFirstOk<ScanResult>(
      ["/api/v1/ingress", "/api/v1/ingress/check"],
      {
        content,
        content_type: "text",
      }
    );
  }

  /**
   * Static scan of a single file (parity with CLI/API vibe-coding mode).
   * Uses canonical POST /api/v1/scan/file.
   */
  async scanFile(
    filename: string,
    content: string,
    mode: "standard" | "vibe-coding" = "vibe-coding"
  ): Promise<ScanFileResponse> {
    return this.post<ScanFileResponse>("/api/v1/scan/file", {
      filename,
      content,
      mode,
    });
  }

  /**
   * Diff scan: findings introduced in new_content vs old_content.
   * Uses POST /api/v1/scan/diff.
   */
  async scanDiff(
    filename: string,
    oldContent: string,
    newContent: string,
    mode: "standard" | "vibe-coding" = "vibe-coding"
  ): Promise<ScanDiffResponse> {
    return this.post<ScanDiffResponse>("/api/v1/scan/diff", {
      filename,
      old_content: oldContent,
      new_content: newContent,
      mode,
    });
  }

  /**
   * Repo snapshot: per-file static scan + repo-batch (e.g. doc↔code consistency).
   * Body uses virtual paths under `files`; server must support POST /api/v1/scan/repo.
   */
  async scanRepo(
    files: Array<{ path: string; content: string }>,
    mode: "standard" | "vibe-coding" = "vibe-coding"
  ): Promise<ScanRepoResponse> {
    return this.post<ScanRepoResponse>("/api/v1/scan/repo", {
      files,
      mode,
    });
  }

  private get<T>(path: string): Promise<T> {
    return this.request<T>("GET", path);
  }

  private async getFirstOk<T>(paths: string[]): Promise<T> {
    let lastErr: unknown;
    for (let i = 0; i < paths.length; i++) {
      try {
        return await this.get<T>(paths[i]!);
      } catch (e) {
        lastErr = e;
        if (
          e instanceof CodeSecHttpError &&
          e.statusCode === 404 &&
          i < paths.length - 1
        ) {
          continue;
        }
        throw e;
      }
    }
    throw lastErr;
  }

  private post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>("POST", path, body);
  }

  private async postFirstOk<T>(paths: string[], body: unknown): Promise<T> {
    let lastErr: unknown;
    for (let i = 0; i < paths.length; i++) {
      try {
        return await this.post<T>(paths[i]!, body);
      } catch (e) {
        lastErr = e;
        if (
          e instanceof CodeSecHttpError &&
          e.statusCode === 404 &&
          i < paths.length - 1
        ) {
          continue;
        }
        throw e;
      }
    }
    throw lastErr;
  }

  private request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    return new Promise((resolve, reject) => {
      const payload = body ? JSON.stringify(body) : undefined;
      const url = new URL(path, this.baseURL);

      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Accept: "application/json",
        "User-Agent": "codesec-vscode/0.1.0",
      };
      if (this.apiKey) {
        headers["Authorization"] = `Bearer ${this.apiKey}`;
      }

      const options: http.RequestOptions = {
        method,
        hostname: this.isUnixSocket ? undefined : url.hostname,
        port: this.isUnixSocket ? undefined : url.port,
        path: url.pathname,
        timeout: this.timeoutMs,
        headers,
      };

      if (this.isUnixSocket) {
        (options as Record<string, unknown>).socketPath = this.socketPath;
      }

      if (payload && options.headers && !Array.isArray(options.headers)) {
        (options.headers as Record<string, string>)["Content-Length"] =
          Buffer.byteLength(payload).toString();
      }

      const transport = url.protocol === "https:" ? https : http;

      const req = transport.request(options, (res) => {
        let data = "";
        res.on("data", (chunk: Buffer) => {
          data += chunk.toString();
        });
        res.on("end", () => {
          if (res.statusCode && res.statusCode >= 200 && res.statusCode < 300) {
            try {
              resolve(JSON.parse(data) as T);
            } catch (e) {
              reject(new Error(`Invalid JSON: ${data.slice(0, 200)}`));
            }
          } else {
            const code = res.statusCode ?? 0;
            reject(
              new CodeSecHttpError(code, data.slice(0, 2000))
            );
          }
        });
      });

      req.on("error", reject);
      req.on("timeout", () => {
        req.destroy();
        reject(new Error("Request timeout"));
      });

      if (payload) {
        req.write(payload);
      }
      req.end();
    });
  }
}
