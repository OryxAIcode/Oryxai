import * as fs from "fs/promises";
import * as os from "os";
import * as path from "path";
import {
  enable,
  disable,
  status,
  settingsPath,
  ClaudeCodePaths,
  CodeSecClaudeConfig,
} from "./claudeCode";

/**
 * Each test gets its own scratch homedir under os.tmpdir() so writes
 * stay sandboxed. The settings.json lives under "$tmp/.claude/" exactly
 * like production.
 */
async function scratchPaths(): Promise<ClaudeCodePaths> {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "codesec-cc-"));
  return { homedir: dir };
}

const baseCfg: CodeSecClaudeConfig = {
  apiKey: "csk_live_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  proxyURL: "http://localhost:8091",
  mcpURL: "http://localhost:8090/sse",
};

describe("claudeCode.enable", () => {
  it("creates a fresh settings.json with our keys", async () => {
    const paths = await scratchPaths();
    const res = await enable(baseCfg, paths);

    expect(res.firstEnable).toBe(true);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.env.ANTHROPIC_BASE_URL).toBe("http://localhost:8091");
    expect(written.env.ANTHROPIC_AUTH_TOKEN).toBe(baseCfg.apiKey);
    expect(written.mcpServers.codesec).toMatchObject({
      type: "sse",
      url: "http://localhost:8090/sse",
      headers: { Authorization: `Bearer ${baseCfg.apiKey}` },
    });
    expect(written.codesecManaged).toBeDefined();
  });

  it("preserves other mcpServers entries", async () => {
    const paths = await scratchPaths();
    const existing = {
      mcpServers: {
        other: { command: "/usr/local/bin/other-mcp", args: ["--flag"] },
      },
      env: { SOMETHING_ELSE: "1" },
    };
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    await fs.writeFile(settingsPath(paths), JSON.stringify(existing), "utf8");

    await enable(baseCfg, paths);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.mcpServers.other).toEqual(existing.mcpServers.other);
    expect(written.env.SOMETHING_ELSE).toBe("1");
  });

  it("refuses to overwrite a non-vanilla ANTHROPIC_BASE_URL", async () => {
    const paths = await scratchPaths();
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    await fs.writeFile(
      settingsPath(paths),
      JSON.stringify({ env: { ANTHROPIC_BASE_URL: "https://my-corp-proxy.example/api" } }),
      "utf8",
    );

    await expect(enable(baseCfg, paths)).rejects.toThrow(
      /Refusing to overwrite a custom upstream/,
    );
  });

  it("does overwrite the default api.anthropic.com (treated as vanilla)", async () => {
    const paths = await scratchPaths();
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    await fs.writeFile(
      settingsPath(paths),
      JSON.stringify({ env: { ANTHROPIC_BASE_URL: "https://api.anthropic.com" } }),
      "utf8",
    );
    await enable(baseCfg, paths);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.env.ANTHROPIC_BASE_URL).toBe("http://localhost:8091");
  });

  it("is idempotent — second call refreshes values, doesn't re-touch backup", async () => {
    // Seed with a pre-existing settings.json so the first enable triggers
    // backup; subsequent enables must NOT touch that backup even if the
    // current settings drift further away from it.
    const paths = await scratchPaths();
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    const originalSettings = JSON.stringify({ env: { ANTHROPIC_AUTH_TOKEN: "user-original" } });
    await fs.writeFile(settingsPath(paths), originalSettings, "utf8");

    const first = await enable(baseCfg, paths);
    expect(first.firstEnable).toBe(true);
    const backupPath = path.join(paths.homedir, ".claude", "settings.json.codesec-backup");
    const backupBefore = await fs.readFile(backupPath, "utf8");
    expect(backupBefore).toEqual(originalSettings);

    const second = await enable(
      { ...baseCfg, apiKey: "csk_live_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
      paths,
    );
    expect(second.firstEnable).toBe(false);
    const backupAfter = await fs.readFile(backupPath, "utf8");
    expect(backupAfter).toEqual(backupBefore); // backup preserved verbatim

    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.env.ANTHROPIC_AUTH_TOKEN).toContain("bbbbbbbb");
  });

  it("first enable on a brand-new home creates no backup (nothing to back up)", async () => {
    const paths = await scratchPaths();
    const res = await enable(baseCfg, paths);
    expect(res.firstEnable).toBe(true);
    const backup = path.join(paths.homedir, ".claude", "settings.json.codesec-backup");
    await expect(fs.access(backup)).rejects.toThrow();
  });

  it("validates the apiKey shape", async () => {
    const paths = await scratchPaths();
    await expect(
      enable({ ...baseCfg, apiKey: "definitely-not-a-csk" }, paths),
    ).rejects.toThrow(/CodeSec key/);
  });

  it("validates the mcp URL must include /sse", async () => {
    const paths = await scratchPaths();
    await expect(
      enable({ ...baseCfg, mcpURL: "http://localhost:8090" }, paths),
    ).rejects.toThrow(/SSE endpoint/);
  });

  it("strips trailing slashes from proxyURL", async () => {
    const paths = await scratchPaths();
    await enable({ ...baseCfg, proxyURL: "http://localhost:8091//" }, paths);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.env.ANTHROPIC_BASE_URL).toBe("http://localhost:8091");
  });
});

describe("claudeCode.disable", () => {
  it("restores original env values when a backup exists", async () => {
    const paths = await scratchPaths();
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    await fs.writeFile(
      settingsPath(paths),
      JSON.stringify({
        env: { ANTHROPIC_AUTH_TOKEN: "user-personal-token", OTHER: "x" },
      }),
      "utf8",
    );

    await enable(baseCfg, paths);
    await disable(paths);

    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.env.ANTHROPIC_AUTH_TOKEN).toBe("user-personal-token");
    expect(written.env.ANTHROPIC_BASE_URL).toBeUndefined();
    expect(written.env.OTHER).toBe("x");
    expect(written.codesecManaged).toBeUndefined();
  });

  it("removes the mcpServers.codesec key on disable", async () => {
    const paths = await scratchPaths();
    await enable(baseCfg, paths);
    await disable(paths);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written.mcpServers).toBeUndefined();
  });

  it("is a no-op when settings.json is unmanaged", async () => {
    const paths = await scratchPaths();
    await fs.mkdir(path.join(paths.homedir, ".claude"), { recursive: true });
    const original = { env: { FOO: "bar" } };
    await fs.writeFile(
      settingsPath(paths),
      JSON.stringify(original),
      "utf8",
    );
    const res = await disable(paths);
    expect(res.hint).toMatch(/isn't managed/);
    const written = JSON.parse(
      await fs.readFile(settingsPath(paths), "utf8"),
    );
    expect(written).toEqual(original);
  });

  it("is a no-op when settings.json is missing entirely", async () => {
    const paths = await scratchPaths();
    const res = await disable(paths);
    expect(res.hint).toMatch(/Nothing to disable/);
  });
});

describe("claudeCode.status", () => {
  it("reports unmanaged when never enabled", async () => {
    const paths = await scratchPaths();
    const st = await status(paths);
    expect(st.exists).toBe(false);
    expect(st.managed).toBe(false);
    expect(st.mcpHasCodesec).toBe(false);
  });

  it("reports managed after enable", async () => {
    const paths = await scratchPaths();
    await enable(baseCfg, paths);
    const st = await status(paths);
    expect(st.exists).toBe(true);
    expect(st.managed).toBe(true);
    expect(st.anthropicBaseURL).toBe("http://localhost:8091");
    expect(st.mcpHasCodesec).toBe(true);
  });
});
